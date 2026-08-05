package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveLoadAndEffectiveScratch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WALDO_CONFIG", path)
	want := Config{Lookaside: Lookaside{
		Scratch: filepath.Join(t.TempDir(), "scratch"),
		Mirrors: []string{"https://one.example/root/", "https://one.example/root", "s3://bucket/root"},
		Publish: &Publish{URL: "s3://bucket/write/", Region: "us-west-2", Workers: 3},
	}, Ingest: Ingest{Staging: filepath.Join(t.TempDir(), "ingest")}, Model: Model{Root: filepath.Join(t.TempDir(), "models"), Backend: "fake"}}
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
	want.Lookaside.Publish.URL = "s3://bucket/write"
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
	root, err := EffectiveScratchRoot(got)
	if err != nil {
		t.Fatal(err)
	}
	if root != want.Lookaside.Scratch {
		t.Fatalf("EffectiveScratchRoot() = %q, want %q", root, want.Lookaside.Scratch)
	}
}

func TestSaveRejectsInvalidPublishConfiguration(t *testing.T) {
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := Save(Config{Lookaside: Lookaside{Publish: &Publish{URL: "https://example.test", Workers: 4}}}); err == nil {
		t.Fatal("expected non-S3 publisher rejection")
	}
	if err := Save(Config{Lookaside: Lookaside{Publish: &Publish{URL: "s3://bucket", Workers: 33}}}); err == nil {
		t.Fatal("expected worker limit rejection")
	}
}

func TestSaveAcceptsLocalPublishConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WALDO_CONFIG", path)
	root := filepath.Join(t.TempDir(), "published")
	configuration := Config{Lookaside: Lookaside{Publish: &Publish{URL: "file://" + root, Workers: 2}}}
	if err := Save(configuration); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Lookaside.Publish == nil || loaded.Lookaside.Publish.URL != "file://"+root {
		t.Fatalf("local publisher = %+v", loaded.Lookaside.Publish)
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

func TestModelBackendDefaultsToRealAutoAndRejectsUnknown(t *testing.T) {
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if got := EffectiveModelBackend(Default()); got != "auto" {
		t.Fatalf("default model backend = %q", got)
	}
	if err := Save(Config{Model: Model{Backend: "made-up"}}); err == nil {
		t.Fatal("unknown model backend accepted")
	}
	for _, backend := range []string{"auto", "mlx", "torchtitan", "pytorch", "fake"} {
		if err := Save(Config{Model: Model{Backend: backend}}); err != nil {
			t.Fatalf("backend %q rejected: %v", backend, err)
		}
	}
}

func TestSigningConfigurationValidation(t *testing.T) {
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	for _, signing := range []Signing{{Method: "sigstore-keyless"}, {Method: "sigstore-key", Key: "/tmp/test.key"}} {
		if err := Save(Config{Signing: signing}); err != nil {
			t.Fatalf("signing %+v rejected: %v", signing, err)
		}
	}
	for _, signing := range []Signing{{Method: "unknown"}, {Method: "sigstore-keyless", Key: "/tmp/test.key"}} {
		if err := Save(Config{Signing: signing}); err == nil {
			t.Fatalf("invalid signing %+v accepted", signing)
		}
	}
}

func TestEffectiveStagingRootIsPlanSpecific(t *testing.T) {
	base := t.TempDir()
	configuration := Config{Ingest: Ingest{Staging: base}}
	got, err := EffectiveStagingRoot(configuration, "plan-identity")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(base, "plan-identity") {
		t.Fatalf("EffectiveStagingRoot() = %q", got)
	}
}

func TestDefaultLocationsSeparateDurableAndDisposableState(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	configuration := Default()
	models, err := EffectiveModelRoot(configuration)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := EffectiveCacheRoot(configuration)
	if err != nil {
		t.Fatal(err)
	}
	scratch, err := EffectiveScratchRoot(configuration)
	if err != nil {
		t.Fatal(err)
	}
	staging, err := EffectiveStagingBase(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if models != filepath.Join(home, ".waldo", "models") {
		t.Fatalf("model root = %q", models)
	}
	if cache != filepath.Join(home, ".waldo", "cache") {
		t.Fatalf("cache root = %q", cache)
	}
	if !within(temporaryRoot(), scratch) || !within(temporaryRoot(), staging) {
		t.Fatalf("temporary defaults are scratch=%q staging=%q, want beneath %q", scratch, staging, temporaryRoot())
	}
	if scratch == staging {
		t.Fatal("scratch and ingestion staging defaults must differ")
	}
}

func within(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
