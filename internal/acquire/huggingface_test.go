package acquire

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(status int, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}
}

func TestFetchHuggingFaceFileStreamsVerifiesAndResumes(t *testing.T) {
	data := []byte("PAR1 immutable parquet fixture PAR1")
	digestArray := sha256.Sum256(data)
	digest := hex.EncodeToString(digestArray[:])
	metadataRequests := 0
	downloadRequests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/datasets/org/set/revision/main":
			metadataRequests++
			body := []byte(fmt.Sprintf(`{"id":"org/set","sha":"commit123","cardData":{"license":"cc-by-4.0"},"siblings":[{"rfilename":"data/train.parquet","size":%d,"lfs":{"sha256":%q,"size":%d}}]}`, len(data), digest, len(data)))
			return response(http.StatusOK, body), nil
		case "/datasets/org/set/resolve/commit123/data/train.parquet":
			downloadRequests++
			return response(http.StatusOK, data), nil
		default:
			return response(http.StatusNotFound, nil), nil
		}
	})}
	directory := filepath.Join(t.TempDir(), "deposit")
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	request := HuggingFaceRequest{Dataset: "org/set", File: "data/train.parquet", Output: directory, BaseURL: "https://hf.test", Client: client, Now: func() time.Time { return now }}
	record, err := FetchHuggingFaceFile(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if record.Source.Version != "commit123" || record.Artifacts[0].SHA256 != digest || record.Artifacts[0].Format != "parquet" || string(record.Source.License) != `"cc-by-4.0"` {
		t.Fatalf("record = %+v", record)
	}
	loaded, root, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(loaded, root); err != nil {
		t.Fatal(err)
	}
	if _, err := FetchHuggingFaceFile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if metadataRequests != 2 || downloadRequests != 1 {
		t.Fatalf("requests = %d metadata, %d downloads", metadataRequests, downloadRequests)
	}
}

func TestFetchHuggingFaceFileRejectsUnverifiableAndUnsafeInputs(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return response(http.StatusOK, []byte(`{"id":"org/set","sha":"commit123","siblings":[{"rfilename":"README.md","size":4}]}`)), nil
	})}
	request := HuggingFaceRequest{Dataset: "org/set", File: "README.md", Output: t.TempDir(), BaseURL: "https://hf.test", Client: client}
	if _, err := FetchHuggingFaceFile(context.Background(), request); err == nil || !strings.Contains(err.Error(), "no immutable LFS SHA-256") {
		t.Fatalf("unverifiable error = %v", err)
	}
	request.File = "../escape.parquet"
	if _, err := FetchHuggingFaceFile(context.Background(), request); err == nil || !strings.Contains(err.Error(), "safe file path") {
		t.Fatalf("unsafe error = %v", err)
	}
}

func TestVerifyRejectsCorruptArtifact(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "data.txt")
	if err := os.WriteFile(path, []byte("wrong"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := Record{Kind: "waldo-acquisition", Schema: 1, Adapter: Identity{Name: "test", Revision: "1"}, Started: "2026-08-04T00:00:00Z", Completed: "2026-08-04T00:00:01Z", Source: Source{Name: "test", Origin: "test", Version: "1", URL: "https://example.test", Category: "public-dataset", CollectedTo: "2026-08"}, Artifacts: []Artifact{{Path: "data.txt", URL: "https://example.test/data", SHA256: strings.Repeat("a", 64), Bytes: 5, Format: "file"}}}
	if err := Verify(record, directory); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifyRejectsUndeclaredFiles(t *testing.T) {
	directory := t.TempDir()
	data := []byte("valid")
	digest := sha256.Sum256(data)
	if err := os.WriteFile(filepath.Join(directory, "data.txt"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte("not declared"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := Record{Kind: "waldo-acquisition", Schema: 1, Adapter: Identity{Name: "test", Revision: "1"}, Started: "2026-08-04T00:00:00Z", Completed: "2026-08-04T00:00:01Z", Source: Source{Name: "test", Origin: "test", Version: "1", URL: "https://example.test", Category: "public-dataset", CollectedTo: "2026-08"}, Artifacts: []Artifact{{Path: "data.txt", URL: "https://example.test/data", SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data)), Format: "file"}}}
	if err := Verify(record, directory); err == nil || !strings.Contains(err.Error(), "undeclared file notes.txt") {
		t.Fatalf("Verify() error = %v", err)
	}
}
