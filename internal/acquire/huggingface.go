package acquire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const HuggingFaceRevision = "builtin-huggingface-file-schema-1"

type Progress struct {
	Phase   string `json:"phase"`
	Status  string `json:"status"`
	Path    string `json:"path,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`
	Total   int64  `json:"total_bytes,omitempty"`
	Message string `json:"message,omitempty"`
}

type HuggingFaceRequest struct {
	Dataset  string
	File     string
	Revision string
	Output   string
	Token    string
	Client   *http.Client
	BaseURL  string
	Now      func() time.Time
	Progress func(Progress)
}

type hfDataset struct {
	ID       string          `json:"id"`
	SHA      string          `json:"sha"`
	CardData json.RawMessage `json:"cardData"`
	Siblings []struct {
		Name string `json:"rfilename"`
		Size int64  `json:"size"`
		LFS  *struct {
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		} `json:"lfs"`
	} `json:"siblings"`
}

func FetchHuggingFaceFile(ctx context.Context, request HuggingFaceRequest) (Record, error) {
	if !validDataset(request.Dataset) || !safeRelative(request.File) || request.Output == "" {
		return Record{}, fmt.Errorf("Hugging Face fetch requires owner/dataset, a safe file path, and an output directory")
	}
	if request.Revision == "" {
		request.Revision = "main"
	}
	if request.Client == nil {
		request.Client = http.DefaultClient
	}
	if request.BaseURL == "" {
		request.BaseURL = "https://huggingface.co"
	}
	if request.Now == nil {
		request.Now = time.Now
	}
	started := request.Now().UTC()
	report(request, Progress{Phase: "metadata", Status: "resolving", Message: request.Dataset + "@" + request.Revision})
	datasetPath := strings.Join([]string{url.PathEscape(strings.Split(request.Dataset, "/")[0]), url.PathEscape(strings.Split(request.Dataset, "/")[1])}, "/")
	endpoint := strings.TrimRight(request.BaseURL, "/") + "/api/datasets/" + datasetPath + "/revision/" + url.PathEscape(request.Revision)
	var metadata hfDataset
	if err := getJSON(ctx, request, endpoint, &metadata); err != nil {
		return Record{}, err
	}
	if metadata.SHA == "" {
		return Record{}, fmt.Errorf("Hugging Face did not return a resolved commit for %s@%s", request.Dataset, request.Revision)
	}
	var selected *struct {
		Name string `json:"rfilename"`
		Size int64  `json:"size"`
		LFS  *struct {
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		} `json:"lfs"`
	}
	for i := range metadata.Siblings {
		if metadata.Siblings[i].Name == request.File {
			selected = &metadata.Siblings[i]
			break
		}
	}
	if selected == nil {
		return Record{}, fmt.Errorf("Hugging Face dataset %s@%s has no file %q", request.Dataset, metadata.SHA, request.File)
	}
	if selected.LFS == nil || !validSHA256(selected.LFS.SHA256) || selected.LFS.Size <= 0 {
		return Record{}, fmt.Errorf("Hugging Face file %s has no immutable LFS SHA-256 and size; this adapter will not notarize an unverifiable download", request.File)
	}
	absolute, err := filepath.Abs(request.Output)
	if err != nil {
		return Record{}, err
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return Record{}, err
	}
	artifactPath := filepath.ToSlash(filepath.Join("data", filepath.FromSlash(request.File)))
	destination := filepath.Join(absolute, filepath.FromSlash(artifactPath))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return Record{}, err
	}
	downloadURL := strings.TrimRight(request.BaseURL, "/") + "/datasets/" + datasetPath + "/resolve/" + url.PathEscape(metadata.SHA) + "/" + escapePath(request.File)
	recordPath := filepath.Join(absolute, RecordName)
	if _, err := os.Stat(recordPath); err == nil {
		existing, directory, loadErr := Load(recordPath)
		if loadErr != nil {
			return Record{}, loadErr
		}
		if existing.Source.Version != metadata.SHA || len(existing.Artifacts) != 1 || existing.Artifacts[0].Path != artifactPath || existing.Artifacts[0].SHA256 != selected.LFS.SHA256 || existing.Artifacts[0].Bytes != selected.LFS.Size {
			return Record{}, fmt.Errorf("%s already contains a different completed acquisition", absolute)
		}
		if err := Verify(existing, directory); err != nil {
			return Record{}, err
		}
		report(request, Progress{Phase: "record", Status: "resumed", Path: recordPath, Message: metadata.SHA})
		return existing, nil
	} else if !os.IsNotExist(err) {
		return Record{}, err
	}
	if err := verifyFile(destination, selected.LFS.SHA256, selected.LFS.Size); err == nil {
		report(request, Progress{Phase: "artifact", Status: "resumed", Path: artifactPath, Bytes: selected.LFS.Size, Total: selected.LFS.Size})
	} else {
		partial := destination + ".part"
		_ = os.Remove(partial)
		report(request, Progress{Phase: "artifact", Status: "downloading", Path: artifactPath, Total: selected.LFS.Size})
		if err := download(ctx, request, downloadURL, partial, selected.LFS.SHA256, selected.LFS.Size); err != nil {
			_ = os.Remove(partial)
			return Record{}, err
		}
		if err := os.Rename(partial, destination); err != nil {
			return Record{}, err
		}
		report(request, Progress{Phase: "artifact", Status: "verified", Path: artifactPath, Bytes: selected.LFS.Size, Total: selected.LFS.Size})
	}
	completed := request.Now().UTC()
	record := Record{
		Kind: "waldo-acquisition", Schema: Schema,
		Adapter: Identity{Name: "huggingface-file", Revision: HuggingFaceRevision},
		Started: started.Format(time.RFC3339), Completed: completed.Format(time.RFC3339),
		Source: Source{
			Name: request.Dataset, Origin: "Hugging Face dataset " + request.Dataset,
			Version: metadata.SHA, URL: "https://huggingface.co/datasets/" + request.Dataset,
			Category: "public-dataset", CollectedTo: completed.Format("2006-01"),
			License: extractLicense(metadata.CardData),
		},
		Proposal:  Proposal{Title: request.Dataset, Description: "Dataset acquired from Hugging Face at commit " + metadata.SHA + "."},
		Artifacts: []Artifact{{Path: artifactPath, URL: downloadURL, SHA256: selected.LFS.SHA256, Bytes: selected.LFS.Size, Format: formatFromPath(request.File), MediaType: mediaTypeFromPath(request.File)}},
	}
	if err := Verify(record, absolute); err != nil {
		return Record{}, fmt.Errorf("verify completed acquisition: %w", err)
	}
	if err := Write(absolute, record); err != nil {
		return Record{}, err
	}
	report(request, Progress{Phase: "record", Status: "complete", Path: filepath.Join(absolute, RecordName), Message: metadata.SHA})
	return record, nil
}

func getJSON(ctx context.Context, request HuggingFaceRequest, endpoint string, target any) error {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	setHeaders(httpRequest, request.Token)
	response, err := request.Client.Do(httpRequest)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Hugging Face metadata: HTTP %s", response.Status)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<20))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("Hugging Face metadata: %w", err)
	}
	return nil
}

func download(ctx context.Context, request HuggingFaceRequest, endpoint, destination, expectedHash string, expectedBytes int64) error {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	setHeaders(httpRequest, request.Token)
	response, err := request.Client.Do(httpRequest)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %s", endpoint, response.Status)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.CopyBuffer(io.MultiWriter(file, hash), response.Body, make([]byte, 1<<20))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != expectedBytes {
		return fmt.Errorf("download size is %d, want %d", written, expectedBytes)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expectedHash {
		return fmt.Errorf("download sha256 is %s, want %s", actual, expectedHash)
	}
	return nil
}

func setHeaders(request *http.Request, token string) {
	request.Header.Set("User-Agent", "OpenWALDO/0.1 (+https://openwaldo.org)")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}

func validDataset(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && safeSegment(parts[0]) && safeSegment(parts[1])
}

func safeSegment(value string) bool {
	return value != "." && value != ".." && !strings.ContainsAny(value, "\\?#")
}

func safeRelative(value string) bool {
	clean := filepath.ToSlash(filepath.Clean(value))
	return value != "" && clean == value && !filepath.IsAbs(value) && clean != "." && !strings.HasPrefix(clean, "../")
}

func escapePath(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func formatFromPath(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".parquet"):
		return "parquet"
	case strings.HasSuffix(lower, ".jsonl"), strings.HasSuffix(lower, ".jsonl.gz"):
		return "jsonl"
	case strings.HasSuffix(lower, ".md"), strings.HasSuffix(lower, ".markdown"):
		return "markdown"
	default:
		return "file"
	}
}

func mediaTypeFromPath(path string) string {
	switch formatFromPath(path) {
	case "parquet":
		return "application/vnd.apache.parquet"
	case "jsonl":
		return "application/x-ndjson"
	case "markdown":
		return "text/markdown"
	default:
		return "application/octet-stream"
	}
}

func extractLicense(card json.RawMessage) json.RawMessage {
	if len(card) == 0 || string(card) == "null" {
		return nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(card, &object) != nil {
		return nil
	}
	return append(json.RawMessage(nil), object["license"]...)
}

func report(request HuggingFaceRequest, progress Progress) {
	if request.Progress != nil {
		request.Progress(progress)
	}
}
