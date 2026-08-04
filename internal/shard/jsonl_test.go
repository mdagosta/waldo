package shard

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/record"
	"github.com/parquet-go/parquet-go"
)

func TestWriteJSONL(t *testing.T) {
	text := "portable text"
	rows := []Row{{
		SHA256: record.TextHash(text), Kind: record.KindPretrain, Text: text,
		Source: "fixture", License: "CC-BY-4.0", Meta: `{"z":2}`,
	}}
	var native bytes.Buffer
	writer := parquet.NewGenericWriter[Row](&native)
	if _, err := writer.Write(rows); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var interchange bytes.Buffer
	written, err := WriteJSONL(&interchange, bytes.NewReader(native.Bytes()), int64(native.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if written != int64(interchange.Len()) || !strings.Contains(interchange.String(), `"text":"portable text"`) || !strings.HasSuffix(interchange.String(), "\n") {
		t.Fatalf("JSONL = %q, bytes = %d", interchange.String(), written)
	}
}

func TestWriteJSONLRejectsInvalidRecord(t *testing.T) {
	rows := []Row{{SHA256: record.TextHash("different"), Kind: record.KindPretrain, Text: "text", Source: "fixture", License: "CC0-1.0"}}
	var native bytes.Buffer
	writer := parquet.NewGenericWriter[Row](&native)
	if _, err := writer.Write(rows); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteJSONL(&bytes.Buffer{}, bytes.NewReader(native.Bytes()), int64(native.Len())); err == nil {
		t.Fatal("expected invalid record error")
	}
}
