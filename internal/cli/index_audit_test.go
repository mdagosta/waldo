package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/config"
	"github.com/openwaldo/waldo-new/internal/shard"
	"github.com/openwaldo/waldo-new/internal/tokenizer"
)

func TestIndexAuditComparesStreamedShardTotalsWithManifest(t *testing.T) {
	root := t.TempDir()
	text := "index audit fixture"
	counter, err := tokenizer.Get(tokenizer.Default)
	if err != nil {
		t.Fatal(err)
	}
	tokens := int64(counter.Count(text))
	digest := sha256.Sum256([]byte(text))
	var encoded bytes.Buffer
	writer := shard.NewTextParquetWriter(&encoded)
	if _, err := writer.Write([]shard.TextRow{{ContentSHA256: digest, Text: text, Source: "fixture", License: "CC0-1.0", TokenCount: &tokens}}); err != nil {
		t.Fatal(err)
	}
	writer.SetKeyValueMetadata("waldo.records", "1")
	writer.SetKeyValueMetadata("waldo.tokens", fmt.Sprint(tokens))
	writer.SetKeyValueMetadata("waldo.content_bytes", fmt.Sprint(len(text)))
	writer.SetKeyValueMetadata("waldo.licenses", `["CC0-1.0"]`)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	objectHash := sha256.Sum256(encoded.Bytes())
	objectDigest := hex.EncodeToString(objectHash[:])
	objectPath := filepath.Join(root, "object.parquet")
	if err := os.WriteFile(objectPath, encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(root, "index.json"), `{"kind":"index","schema":2,"path":"","entries":[{"name":"tiny","type":"dir"}]}`)
	writeCLIFile(t, filepath.Join(root, "tiny", "index.json"), `{"kind":"index","schema":2,"path":"tiny","entries":[{"name":"tiny.json","type":"manifest"}]}`)
	writeAuditManifest(t, root, objectPath, objectDigest, int64(encoded.Len()), tokens)
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{Lookaside: config.Lookaside{Scratch: t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"index", "audit", root}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "VERIFIED") {
		t.Fatalf("valid audit code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	writeAuditManifest(t, root, objectPath, objectDigest, int64(encoded.Len()), tokens+1)
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"index", "audit", root}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "audited totals differ") {
		t.Fatalf("mismatched audit code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func writeAuditManifest(t *testing.T, root, objectPath, digest string, bytes, tokens int64) {
	t.Helper()
	manifest := fmt.Sprintf(`{
  "kind":"manifest", "schema":1, "name":"tiny", "title":"Tiny", "description":"Audit fixture.", "license":"CC0-1.0",
  "sources":[{"name":"fixture","source":"Fixture","url":"https://example.invalid/audit","sha256":"%s"}],
  "converted_by":{"tool":"waldo","version":"test","profile":"text","recipe":"%s","tokenizer":"%s"},
  "shards":[{"url":%q,"sha256":%q,"sources":["fixture"],"docs":1,"tokens":%d,"bytes":%d}]
}`, strings.Repeat("a", 64), shard.TextWriterRecipe, tokenizer.Default, objectPath, digest, tokens, bytes)
	writeCLIFile(t, filepath.Join(root, "tiny", "tiny.json"), manifest)
}
