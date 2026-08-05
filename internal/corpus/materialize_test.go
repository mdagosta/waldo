package corpus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/lookaside"
)

func TestMaterializeUsesBOMWithoutIndex(t *testing.T) {
	content := []byte("shard bytes")
	digest := sha256.Sum256(content)
	source := filepath.Join(t.TempDir(), "source.parquet")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}
	cache, err := lookaside.NewCache(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	shard := ShardPin{
		Manifest: "books/books.json",
		URL:      source,
		SHA256:   hex.EncodeToString(digest[:]),
		Format:   "parquet",
		License:  "CC0-1.0",
		Docs:     1,
		Tokens:   2,
		Bytes:    int64(len(content)),
	}
	bom := BOM{
		Kind: "openwaldo-bom", Schema: 1, Subject: "corpus",
		Shards: []ShardPin{shard, shard},
		Totals: index.Measures{Shards: 2, Docs: 2, Tokens: 4, Bytes: int64(len(content) * 2)},
	}
	progressCalls := 0
	materialized, err := Materialize(context.Background(), bom, cache, func(MaterializeProgress) { progressCalls++ })
	if err != nil {
		t.Fatal(err)
	}
	if len(materialized.Objects) != 2 || materialized.Objects[0].Path != materialized.Objects[1].Path {
		t.Fatalf("materialized objects = %+v", materialized.Objects)
	}
	if progressCalls != 2 {
		t.Fatalf("progress calls = %d, want 2", progressCalls)
	}
}
