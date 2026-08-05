// Package acquire owns bounded upstream adapters and their local evidence.
package acquire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/openwaldo/waldo-new/internal/index"
)

const (
	Schema     = 1
	RecordName = "ACQUISITION.json"
)

type Record struct {
	Kind      string     `json:"kind"`
	Schema    int        `json:"schema"`
	Adapter   Identity   `json:"adapter"`
	Started   string     `json:"started"`
	Completed string     `json:"completed"`
	Source    Source     `json:"source"`
	Proposal  Proposal   `json:"proposal,omitempty"`
	Artifacts []Artifact `json:"artifacts"`
}

type Identity struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

type Source struct {
	Name          string          `json:"name"`
	Origin        string          `json:"origin"`
	Version       string          `json:"version"`
	URL           string          `json:"url"`
	Category      string          `json:"category"`
	CollectedFrom string          `json:"collected_from,omitempty"`
	CollectedTo   string          `json:"collected_to"`
	License       json.RawMessage `json:"license_evidence,omitempty"`
}

type Proposal struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type Artifact struct {
	Path      string `json:"path"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	Format    string `json:"format"`
	MediaType string `json:"media_type,omitempty"`
}

func Load(path string) (Record, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Record{}, "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return Record{}, "", err
	}
	directory := absolute
	filePath := absolute
	if info.IsDir() {
		filePath = filepath.Join(absolute, RecordName)
	} else {
		directory = filepath.Dir(absolute)
		if filepath.Base(absolute) != RecordName {
			return Record{}, "", fmt.Errorf("acquisition record must be named %s", RecordName)
		}
	}
	file, err := os.Open(filePath)
	if err != nil {
		return Record{}, "", err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return Record{}, "", fmt.Errorf("%s: %w", filePath, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values are not allowed")
		}
		return Record{}, "", fmt.Errorf("%s: %w", filePath, err)
	}
	if err := record.Validate(); err != nil {
		return Record{}, "", fmt.Errorf("%s: %w", filePath, err)
	}
	return record, directory, nil
}

func (record Record) Validate() error {
	if record.Kind != "waldo-acquisition" || record.Schema != Schema {
		return fmt.Errorf("unsupported acquisition identity %q schema %d", record.Kind, record.Schema)
	}
	if record.Adapter.Name == "" || record.Adapter.Revision == "" || record.Started == "" || record.Completed == "" {
		return fmt.Errorf("adapter identity and acquisition times are required")
	}
	started, err := time.Parse(time.RFC3339, record.Started)
	if err != nil {
		return fmt.Errorf("invalid acquisition start time")
	}
	completed, err := time.Parse(time.RFC3339, record.Completed)
	if err != nil || completed.Before(started) {
		return fmt.Errorf("invalid acquisition completion time")
	}
	if record.Source.Name == "" || record.Source.Origin == "" || record.Source.Version == "" || record.Source.URL == "" || record.Source.Category == "" || record.Source.CollectedTo == "" {
		return fmt.Errorf("source identity, version, category, and acquisition month are required")
	}
	if category, ok := index.CanonicalSourceCategory(record.Source.Category); !ok || category != record.Source.Category {
		return fmt.Errorf("source category %q is not canonical", record.Source.Category)
	}
	if len(record.Source.License) > 0 && !json.Valid(record.Source.License) {
		return fmt.Errorf("source license evidence is not valid JSON")
	}
	for _, value := range []string{record.Source.CollectedFrom, record.Source.CollectedTo} {
		if value != "" {
			if _, err := time.Parse("2006-01", value); err != nil {
				return fmt.Errorf("collection months must use YYYY-MM")
			}
		}
	}
	if len(record.Artifacts) == 0 {
		return fmt.Errorf("at least one acquired artifact is required")
	}
	paths := make([]string, 0, len(record.Artifacts))
	for _, artifact := range record.Artifacts {
		clean := filepath.ToSlash(filepath.Clean(artifact.Path))
		if artifact.Path == "" || clean != artifact.Path || filepath.IsAbs(artifact.Path) || clean == "." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("unsafe artifact path %q", artifact.Path)
		}
		if artifact.URL == "" || !validSHA256(artifact.SHA256) || artifact.Bytes <= 0 || artifact.Format == "" {
			return fmt.Errorf("artifact %s has incomplete identity", artifact.Path)
		}
		paths = append(paths, artifact.Path)
	}
	if !sort.StringsAreSorted(paths) {
		return fmt.Errorf("artifact paths must be sorted")
	}
	for i := 1; i < len(paths); i++ {
		if paths[i] == paths[i-1] {
			return fmt.Errorf("artifact path %s is duplicated", paths[i])
		}
	}
	return nil
}

func Verify(record Record, directory string) error {
	if err := record.Validate(); err != nil {
		return err
	}
	for _, artifact := range record.Artifacts {
		path := filepath.Join(directory, filepath.FromSlash(artifact.Path))
		if err := verifyFile(path, artifact.SHA256, artifact.Bytes); err != nil {
			return fmt.Errorf("artifact %s: %w", artifact.Path, err)
		}
	}
	declared := map[string]bool{}
	for _, artifact := range record.Artifacts {
		declared[artifact.Path] = true
	}
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == RecordName {
			return nil
		}
		if !declared[relative] {
			return fmt.Errorf("undeclared file %s", relative)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func Write(directory string, record Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, ".waldo-acquisition-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, filepath.Join(directory, RecordName)); err != nil {
		return err
	}
	committed = true
	return nil
}

func verifyFile(path, expectedHash string, expectedBytes int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.CopyBuffer(hash, file, make([]byte, 1<<20))
	if err != nil {
		return err
	}
	if written != expectedBytes {
		return fmt.Errorf("size is %d, want %d", written, expectedBytes)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expectedHash {
		return fmt.Errorf("sha256 is %s, want %s", actual, expectedHash)
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
