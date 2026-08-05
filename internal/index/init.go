package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Initialize creates the smallest valid schema-1 index at an empty directory.
// Git initialization remains an explicit user action.
func Initialize(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(absolute); statErr == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("index path %s is not a directory", absolute)
		}
		entries, err := os.ReadDir(absolute)
		if err != nil {
			return "", err
		}
		if len(entries) != 0 {
			return "", fmt.Errorf("index directory %s is not empty", absolute)
		}
	} else if os.IsNotExist(statErr) {
		if err := os.MkdirAll(absolute, 0o755); err != nil {
			return "", err
		}
	} else {
		return "", statErr
	}

	directory := Directory{Kind: "index", Schema: DirectorySchema, Path: "", Entries: []Entry{}}
	data, err := json.MarshalIndent(directory, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(absolute, ".waldo-index-*")
	if err != nil {
		return "", err
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
		return "", err
	}
	if err := temporary.Chmod(0o644); err != nil {
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, filepath.Join(absolute, indexFile)); err != nil {
		return "", err
	}
	committed = true
	directoryHandle, err := os.Open(absolute)
	if err != nil {
		return "", err
	}
	if err := directoryHandle.Sync(); err != nil {
		_ = directoryHandle.Close()
		return "", err
	}
	if err := directoryHandle.Close(); err != nil {
		return "", err
	}
	return absolute, nil
}
