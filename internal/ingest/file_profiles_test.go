package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGutenbergProfileStripsOutsideStandardMarkers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.txt")
	contents := "header and license\n*** START OF THE PROJECT GUTENBERG EBOOK TEST ***\nBook text.\n*** END OF THE PROJECT GUTENBERG EBOOK TEST ***\nfooter"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileGutenbergText})
	rows := collectMappedRows(t, plan)
	if len(rows) != 1 || rows[0].Text != "Book text.\n" || strings.Contains(rows[0].Text, "license") {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestGutenbergProfileRequiresBothMarkers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.txt")
	if err := os.WriteFile(path, []byte("*** START OF THE PROJECT GUTENBERG EBOOK TEST ***\nBook text."), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileGutenbergText})
	err := StreamCanonicalTextBatches(context.Background(), plan, func(TextBatch) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "START and END markers are required") {
		t.Fatalf("error = %v", err)
	}
}

const jatsTestArticle = `<?xml version="1.0"?>
<article article-type="research-article">
  <front>
    <journal-meta><journal-title>PLOS ONE</journal-title></journal-meta>
    <article-meta>
      <article-id pub-id-type="doi">10.1371/journal.pone.0123456</article-id>
      <title-group><article-title>Article title</article-title></title-group>
      <pub-date pub-type="epub"><day>7</day><month>3</month><year>2019</year></pub-date>
      <abstract><p>Abstract prose.</p></abstract>
      <permissions><license><license-p>Distributed under <ext-link xlink:href="https://creativecommons.org/licenses/by/4.0/">CC BY</ext-link>.</license-p></license></permissions>
    </article-meta>
  </front>
  <body><sec><title>Introduction</title><p>Body prose.</p><fig><caption><p>Excluded figure.</p></caption></fig></sec></body>
  <back><ref-list><ref><mixed-citation>Excluded citation.</mixed-citation></ref></ref-list></back>
</article>`

func TestJATSProfileExtractsArticleFactsAndPerArticleLicense(t *testing.T) {
	path := filepath.Join(t.TempDir(), "article.xml")
	if err := os.WriteFile(path, []byte(jatsTestArticle), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileJATSXML})
	rows := collectMappedRows(t, plan)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	row := rows[0]
	for _, wanted := range []string{"Article title", "Abstract prose.", "Introduction", "Body prose."} {
		if !strings.Contains(row.Text, wanted) {
			t.Fatalf("text misses %q: %q", wanted, row.Text)
		}
	}
	if strings.Contains(row.Text, "Excluded") || row.Source != "https://doi.org/10.1371/journal.pone.0123456" || row.Date == nil || *row.Date != "2019-03-07" || row.License != "https://creativecommons.org/licenses/by/4.0/" || row.LicenseRaw == nil || row.Meta == nil || !strings.Contains(*row.Meta, "PLOS ONE") {
		t.Fatalf("row = %+v", row)
	}
}
