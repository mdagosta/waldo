package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeCreatesMinimalValidIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-index")
	root, err := Initialize(path)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := LoadDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if directory.Kind != "index" || directory.Schema != DirectorySchema || directory.Path != "" || len(directory.Entries) != 0 {
		t.Fatalf("directory = %+v", directory)
	}
	target, err := Resolve("", root)
	if err != nil {
		t.Fatal(err)
	}
	verification, err := Verify(target)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Directories != 1 || verification.Corpora != 0 || verification.Shards != 0 {
		t.Fatalf("verification = %+v", verification)
	}
	if _, err := os.Stat(filepath.Join(root, "index.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "index.json")); !os.IsNotExist(err) {
		t.Fatalf("Initialize wrote legacy JSON navigation: %v", err)
	}
}

func TestInitializeRefusesNonemptyDirectory(t *testing.T) {
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "keep.txt"), []byte("user data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(path); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("Initialize() error = %v", err)
	}
}
