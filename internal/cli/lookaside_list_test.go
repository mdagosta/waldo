package cli

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/config"
	waldoindex "github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/lookaside"
)

func TestLookasideListRelatesObjectsToSelectedIndex(t *testing.T) {
	lookasideRoot := t.TempDir()
	baseURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(lookasideRoot)}).String()
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{Lookaside: config.Lookaside{
		Publish: &config.Publish{URL: baseURL, Workers: 2}, Scratch: t.TempDir(),
	}}); err != nil {
		t.Fatal(err)
	}
	publisher, err := lookaside.NewFilePublisher(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	referenced := publishRemovalFixture(t, publisher, []byte("referenced object"))
	unreferenced := publishRemovalFixture(t, publisher, []byte("not in selected index"))
	missing := strings.Repeat("f", 64)
	indexRoot := writeLookasideListIndex(t, baseURL, referenced, missing)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"lookaside", "list", indexRoot}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{referenced, unreferenced, "tiny.json", "(not in selected index)", "MISSING " + missing, "1 referenced, 1 not in selected index"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestLookasideListJSONWithoutIndex(t *testing.T) {
	lookasideRoot := t.TempDir()
	baseURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(lookasideRoot)}).String()
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{Lookaside: config.Lookaside{Publish: &config.Publish{URL: baseURL, Workers: 2}}}); err != nil {
		t.Fatal(err)
	}
	publisher, err := lookaside.NewFilePublisher(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	digest := publishRemovalFixture(t, publisher, []byte("listed as json"))

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"lookaside", "list", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	var result struct {
		Objects []struct {
			Name string `json:"name"`
		} `json:"objects"`
		Totals lookasideListTotals `json:"totals"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Objects) != 1 || result.Objects[0].Name != digest || result.Totals.Objects != 1 || result.Totals.Canonical != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func writeLookasideListIndex(t *testing.T, baseURL, referenced, missing string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "index")
	if _, err := waldoindex.Initialize(root); err != nil {
		t.Fatal(err)
	}
	directory := waldoindex.Directory{
		Kind: "index", Schema: 2, Path: "", Entries: []waldoindex.Entry{{Name: "tiny.json", Type: "manifest"}},
	}
	manifest := waldoindex.Manifest{
		Kind: "manifest", Schema: 1, Name: "tiny", Title: "Tiny", Description: "List relationship fixture", License: "CC0-1.0",
		Format: "parquet", RecordSchema: 1,
		Sources:     []waldoindex.Source{{Name: "fixture", Source: "fixture", URL: "https://example.invalid", SHA256: strings.Repeat("a", 64)}},
		ConvertedBy: waldoindex.Conversion{Tool: "test", Version: "1", Profile: "text", Recipe: "test/v1"},
		Shards: []waldoindex.Shard{
			{URL: baseURL + "/" + referenced[:2] + "/" + referenced[2:4] + "/" + referenced, SHA256: referenced, Docs: 1, Bytes: 17, Sources: []string{"fixture"}},
			{URL: baseURL + "/" + missing[:2] + "/" + missing[2:4] + "/" + missing, SHA256: missing, Docs: 1, Bytes: 1, Sources: []string{"fixture"}},
		},
	}
	writeListJSON(t, filepath.Join(root, "index.json"), directory)
	writeListJSON(t, filepath.Join(root, "tiny.json"), manifest)
	return root
}

func writeListJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
