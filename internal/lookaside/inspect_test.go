package lookaside

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatsAndScrub(t *testing.T) {
	cache, err := NewCache(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	goodContent := "good"
	goodDigest := digestOf(goodContent)
	goodPath, _ := cache.Path(goodDigest)
	if err := os.MkdirAll(filepath.Dir(goodPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goodPath, []byte(goodContent), 0o644); err != nil {
		t.Fatal(err)
	}
	badDigest := digestOf("expected")
	badPath, _ := cache.Path(badDigest)
	if err := os.MkdirAll(filepath.Dir(badPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPath, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache.Root(), "note"), []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats, err := cache.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Objects != 2 || stats.Other != 1 {
		t.Fatalf("Stats() = %+v", stats)
	}
	scrub, err := cache.Scrub()
	if err != nil {
		t.Fatal(err)
	}
	if scrub.Verified != 1 || len(scrub.Corrupt) != 1 {
		t.Fatalf("Scrub() = %+v", scrub)
	}
}
