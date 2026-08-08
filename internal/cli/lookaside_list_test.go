// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openwaldo/waldo/internal/config"
	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/lookaside"
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
	for _, want := range []string{"OBJECT", "STORED (UTC)", referenced[:16], "tiny", "--"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{unreferenced[:16], "inventory ", "configured lookaside ", "selected index ", " shown (", "bucket objects match", missing} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("output unexpectedly contains %q:\n%s", unwanted, output)
		}
	}
	if strings.Contains(output, "LAST ACCESS") {
		t.Fatalf("output unexpectedly contains LAST ACCESS:\n%s", output)
	}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Fatalf("output line has trailing padding: %q", line)
		}
	}
	if lines := strings.Split(strings.TrimSpace(output), "\n"); len(lines) != 2 {
		t.Fatalf("wanted header and one matching row, got %d lines:\n%s", len(lines), output)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"lookaside", "list", indexRoot, "--all"}, &stdout, &stderr); code != 0 {
		t.Fatalf("--all code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	allOutput := stdout.String()
	for _, want := range []string{referenced[:16], unreferenced[:16], "tiny", "--"} {
		if !strings.Contains(allOutput, want) {
			t.Fatalf("--all output missing %q:\n%s", want, allOutput)
		}
	}
	if lines := strings.Split(strings.TrimSpace(allOutput), "\n"); len(lines) != 3 {
		t.Fatalf("wanted header and two rows, got %d lines:\n%s", len(lines), allOutput)
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
	if strings.Contains(stderr.String(), "no index path specified") {
		t.Fatalf("lookaside inventory emitted an index warning: %q", stderr.String())
	}
	var result struct {
		Objects []struct {
			Name     string    `json:"name"`
			StoredAt time.Time `json:"stored_at"`
		} `json:"objects"`
		Totals lookasideListTotals `json:"totals"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Objects) != 1 || result.Objects[0].Name != digest || result.Totals.Objects != 1 || result.Totals.Canonical != 1 || result.Totals.WithinLookaside != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Objects[0].StoredAt.IsZero() {
		t.Fatalf("timestamps = %+v", result.Objects[0])
	}
}

func writeLookasideListIndex(t *testing.T, baseURL, referenced, missing string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "index")
	if _, err := waldoindex.Initialize(root); err != nil {
		t.Fatal(err)
	}
	directory := waldoindex.Directory{
		Kind: "index", Schema: 1, Path: "", Entries: []waldoindex.Entry{{Name: "tiny.yaml", Type: "manifest"}},
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
	writeListYAML(t, filepath.Join(root, "index.yaml"), directory)
	writeListYAML(t, filepath.Join(root, "tiny.yaml"), manifest)
	return root
}

func writeListYAML(t *testing.T, path string, value any) {
	t.Helper()
	data, err := waldoindex.MarshalYAML(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
