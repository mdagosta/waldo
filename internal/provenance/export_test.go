package provenance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openwaldo/waldo-new/internal/corpus"
)

func TestWriteCorpusExport(t *testing.T) {
	destination := t.TempDir()
	document := NewCorpusExport(
		corpus.BOM{Kind: "openwaldo-bom", Schema: 1, Subject: "corpus"},
		"native",
		[]corpus.ExportedFile{{Path: "data/books/item.parquet", SHA256: "abc"}},
		time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
	)
	if err := WriteCorpusExport(destination, document); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "EXPORT.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded CorpusExport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != "waldo-corpus-export" || decoded.Format != "native" || decoded.Generated != "2026-08-04T12:00:00Z" || len(decoded.Files) != 1 {
		t.Fatalf("decoded export = %+v", decoded)
	}
}
