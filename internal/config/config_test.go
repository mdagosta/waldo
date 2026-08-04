package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveLoadAndEffectiveCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WALDO_CONFIG", path)
	t.Setenv("WALDO_CACHE", "")
	want := Config{Lookaside: Lookaside{
		Cache:   filepath.Join(t.TempDir(), "cache"),
		Mirrors: []string{"https://one.example/root/", "https://one.example/root", "s3://bucket/root"},
	}}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", info.Mode().Perm())
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want.Schema = 1
	want.Lookaside.Mirrors = []string{"https://one.example/root", "s3://bucket/root"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
	root, err := EffectiveCacheRoot(got)
	if err != nil {
		t.Fatal(err)
	}
	if root != want.Lookaside.Cache {
		t.Fatalf("EffectiveCacheRoot() = %q, want %q", root, want.Lookaside.Cache)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != Schema {
		t.Fatalf("Load() = %+v", got)
	}
}
