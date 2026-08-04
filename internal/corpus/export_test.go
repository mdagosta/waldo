package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/lookaside"
)

func TestExportNativeCopiesAndResumesVerifiedFiles(t *testing.T) {
	content := []byte("parquet-shaped fixture")
	digestArray := sha256.Sum256(content)
	digest := hex.EncodeToString(digestArray[:])
	cacheObject := filepath.Join(t.TempDir(), "cached")
	if err := os.WriteFile(cacheObject, content, 0o644); err != nil {
		t.Fatal(err)
	}
	shard := ShardPin{
		Manifest: "books/books.json", SHA256: digest, Format: "parquet",
		License: "CC0-1.0", Docs: 1, Tokens: 2, Bytes: int64(len(content)),
	}
	materialized := Materialized{Objects: []MaterializedObject{{Shard: shard, Path: cacheObject}}}
	destination := t.TempDir()
	files, err := ExportNative(materialized, destination, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Existing {
		t.Fatalf("ExportNative() = %+v", files)
	}
	exported := filepath.Join(destination, filepath.FromSlash(files[0].Path))
	if err := lookaside.VerifyFile(exported, digest, int64(len(content))); err != nil {
		t.Fatal(err)
	}

	files, err = ExportNative(materialized, destination, false)
	if err != nil {
		t.Fatal(err)
	}
	if !files[0].Existing {
		t.Fatalf("resumed export did not identify existing file: %+v", files[0])
	}

	if err := os.WriteFile(exported, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportNative(materialized, destination, false); err == nil || !strings.Contains(err.Error(), "use --force") {
		t.Fatalf("corrupt destination error = %v", err)
	}
	if _, err := ExportNative(materialized, destination, true); err != nil {
		t.Fatal(err)
	}
	if err := lookaside.VerifyFile(exported, digest, int64(len(content))); err != nil {
		t.Fatal(err)
	}
}
