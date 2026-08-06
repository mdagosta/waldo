package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundedTextExtractsBetweenConfiguredPatterns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.txt")
	contents := "unwanted header\n=== START: variable title ===\nDocument text.\n=== END: variable title ===\nunwanted footer"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{Type: ProfileBoundedText, Bounds: TextBounds{
		StartPattern: `(?m)^=== START: .+ ===$`, EndPattern: `(?m)^=== END: .+ ===$`,
	}}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	if len(rows) != 1 || rows[0].Text != "Document text.\n" || strings.Contains(rows[0].Text, "unwanted") {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestBoundedTextRequiresBothConfiguredBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.txt")
	if err := os.WriteFile(path, []byte("BEGIN\nDocument text."), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{Type: ProfileBoundedText, Bounds: TextBounds{StartPattern: `(?m)^BEGIN$`, EndPattern: `(?m)^END$`}}
	err := StreamCanonicalTextBatches(context.Background(), mappedFixturePlan(t, path, profile), func(TextBatch) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "end boundary did not match") {
		t.Fatalf("error = %v", err)
	}
}

const xmlRecordFixture = `<?xml version="1.0"?>
<doc xmlns:x="https://example.test/ns/metadata">
  <header>
    <identifier>record-7</identifier>
    <date>2019-03-07</date>
    <license x:href="CC-BY-4.0"/>
    <journal>Example Journal</journal>
  </header>
  <title>Article <em>title</em>.</title>
  <abstract><p>Abstract prose.</p></abstract>
  <body><section><p>Body prose.</p><figure><caption>Excluded figure.</caption></figure></section></body>
</doc>`

func TestXMLRecordMapsXPathSubsetAndExclusions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.xml")
	if err := os.WriteFile(path, []byte(xmlRecordFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{
		Type: ProfileXMLRecord,
		Fields: ProfileFields{
			Text: []string{"/doc/title", "/doc/abstract", "/doc/body"}, Source: "/doc/header/identifier",
			Date: "/doc/header/date", License: "/doc/header/license/@{https://example.test/ns/metadata}href",
			Meta: map[string]string{"publication": "/doc/header/journal"},
		},
		XML: XMLMapping{Exclude: []string{"//figure"}, SourcePrefix: "urn:record:"},
	}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	row := rows[0]
	for _, wanted := range []string{"Article title.", "Abstract prose.", "Body prose."} {
		if !strings.Contains(row.Text, wanted) {
			t.Fatalf("text misses %q: %q", wanted, row.Text)
		}
	}
	if strings.Contains(row.Text, "Excluded") || row.Source != "urn:record:record-7" || row.Date == nil || *row.Date != "2019-03-07" || row.License != "CC-BY-4.0" || row.LicenseRaw == nil || row.Meta == nil || !strings.Contains(*row.Meta, "Example Journal") {
		t.Fatalf("row = %+v", row)
	}
}

func TestXMLRecordConcatenatesRepeatedNodesInDocumentOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.xml")
	if err := os.WriteFile(path, []byte(`<doc><body><p>First.</p><p>Second.</p></body></doc>`), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{Type: ProfileXMLRecord, Fields: ProfileFields{Text: []string{"/doc/body/p"}}}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	if len(rows) != 1 || rows[0].Text != "First.\n\nSecond.\n" {
		t.Fatalf("rows = %+v", rows)
	}
}
