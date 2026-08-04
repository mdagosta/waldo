package corpus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/lookaside"
)

func TestCheckAvailabilityProbesEveryCanonicalShard(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "first.parquet")
	second := filepath.Join(directory, "second.parquet")
	if err := os.WriteFile(first, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	bom := availabilityBOM([]ShardPin{
		{Manifest: "example/example.json", URL: first, SHA256: strings.Repeat("b", 64), Format: "parquet", RecordSchema: 1, License: "CC0-1.0", ConvertedBy: conversionFixture(), Docs: 1, Tokens: 2, Bytes: 5},
		{Manifest: "example/example.json", URL: second, SHA256: strings.Repeat("c", 64), Format: "parquet", RecordSchema: 1, License: "CC0-1.0", ConvertedBy: conversionFixture(), Docs: 1, Tokens: 3, Bytes: 6},
	})
	cache, err := lookaside.NewCache(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	progress := 0
	available, err := CheckAvailability(context.Background(), bom, cache, 2, func(AvailabilityProgress) { progress++ })
	if err != nil {
		t.Fatal(err)
	}
	if available.Objects != 2 || available.Bytes != 11 || progress != 2 {
		t.Fatalf("availability = %+v, progress = %d", available, progress)
	}
}

func availabilityBOM(shards []ShardPin) BOM {
	measures := index.Measures{}
	for _, shard := range shards {
		measures.Shards++
		measures.Docs += shard.Docs
		measures.Tokens += shard.Tokens
		measures.Bytes += shard.Bytes
	}
	manifest := ManifestPin{
		Path: "example/example.json", SHA256: strings.Repeat("a", 64), Name: "example", Title: "Example",
		Description: "Availability fixture.", License: "CC0-1.0", Format: "parquet", RecordSchema: 1,
		ConvertedBy: conversionFixture(),
		Sources:     []index.Source{{Name: "fixture", Source: "Fixture", URL: "https://example.test", SHA256: strings.Repeat("d", 64)}},
		Totals:      measures, Licenses: map[string]index.Measures{"CC0-1.0": measures},
	}
	return BOM{
		Kind: "openwaldo-bom", Schema: 1, Subject: "corpus", Paths: []string{"example"},
		Manifests: []ManifestPin{manifest}, Shards: shards, Totals: measures,
		Licenses: map[string]index.Measures{"CC0-1.0": measures},
	}
}

func conversionFixture() index.Conversion {
	return index.Conversion{Tool: "test", Version: "1", Profile: "text", Recipe: "test/v1", Tokenizer: "byte"}
}
