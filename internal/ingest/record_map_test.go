// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/shard"
	"github.com/parquet-go/parquet-go"
)

func TestRecordMapReadsOneJSONObjectAndExpandsArrays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "case.json")
	contents := `{
  "id":"https://cite.case/1","decision_date":"1825-12","language":"en",
  "metadata":{"license":"https://creativecommons.org/licenses/by/4.0/"},
  "casebody":{"head_matter":"Case heading","opinions":[{"text":"Majority"},{"text":"Dissent"}]}
}`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{
		Text: []string{"casebody.head_matter", "casebody.opinions[].text"}, ID: "id",
		Date: "decision_date", Language: "language", License: "metadata.license",
	}}
	plan := mappedFixturePlan(t, path, profile)
	rows := collectMappedRows(t, plan)
	if len(rows) != 1 || rows[0].Text != "Case heading\n\nMajority\n\nDissent" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Source != "https://cite.case/1" || rows[0].License != "CC-BY-4.0" || rows[0].LicenseRaw == nil || *rows[0].LicenseRaw != "https://creativecommons.org/licenses/by/4.0/" || *rows[0].Language != "en" || *rows[0].Date != "1825-12" {
		t.Fatalf("mapped metadata = %+v", rows[0])
	}
}

func TestRecordMapPreservesAndConservativelyCombinesLicenseArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	contents := `{"text":"licensed text","metadata":{"license":["MIT License","Creative Commons - Attribution Share-Alike - https://creativecommons.org/licenses/by-sa/4.0/"]}}` + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{
		Type:   ProfileRecordMap,
		Fields: ProfileFields{Text: []string{"text"}, License: "metadata.license[]"},
		LicensePolicy: corpus.LicensePolicy{Include: []string{
			"CC-BY-SA-4.0 AND MIT",
		}},
	}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	if len(rows) != 1 || rows[0].License != "CC-BY-SA-4.0 AND MIT" || rows[0].LicenseRaw == nil {
		t.Fatalf("rows = %+v", rows)
	}
	wantRaw := `["MIT License","Creative Commons - Attribution Share-Alike - https://creativecommons.org/licenses/by-sa/4.0/"]`
	if *rows[0].LicenseRaw != wantRaw {
		t.Fatalf("raw license = %q, want %q", *rows[0].LicenseRaw, wantRaw)
	}
}

func TestRecordMapPreservesSelectedProvenanceMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	contents := `{"text":"training record","id":"record-7","metadata":{"dataset_id":"dataset-a","url":"https://source.example/7","tags":["one","two"]}}` + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{
		Text: []string{"text"}, Source: "metadata.url",
		Meta: map[string]string{"dataset_id": "metadata.dataset_id", "record_id": "id", "tags": "metadata.tags[]"},
	}}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	if len(rows) != 1 || rows[0].Source != "https://source.example/7" || rows[0].Meta == nil {
		t.Fatalf("rows = %+v", rows)
	}
	want := `{"dataset_id":"dataset-a","record_id":"record-7","tags":["one","two"]}`
	if *rows[0].Meta != want {
		t.Fatalf("meta = %q, want %q", *rows[0].Meta, want)
	}
}

func TestRecordMapUsesFallbackTextOnlyWhenPrimaryTextIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	contents := "{\"text\":\"page text\",\"metadata\":{\"url\":\"https://source.example/1\"}}\n" +
		"{\"text\":\"\",\"metadata\":{\"url\":\"https://source.example/2\"}}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{
		Text: []string{"text"}, TextFallback: []string{"metadata.url"},
		Meta: map[string]string{"url": "metadata.url"},
	}}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Text != "page text" || rows[1].Text != "https://source.example/2" {
		t.Fatalf("fallback rows = %+v", rows)
	}
	if rows[0].Meta == nil || *rows[0].Meta != `{"url":"https://source.example/1"}` ||
		rows[1].Meta == nil || *rows[1].Meta != `{"url":"https://source.example/2"}` {
		t.Fatalf("fallback metadata = %+v", rows)
	}
}

func TestRecordMapRejectsTopLevelJSONArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, []byte(`[{"text":"one"},{"text":"two"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{Text: []string{"text"}}})
	err := StreamCanonicalTextBatches(context.Background(), plan, func(TextBatch) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "top-level JSON arrays are not supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestRecordMapUsesExistingCompressedJSONLReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl.zst")
	writeCompressedJSONL(t, path, "zstd", "{\"payload\":{\"body\":\"first\"}}\n{\"payload\":{\"body\":\"second\"}}\n")
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{Text: []string{"payload.body"}}})
	rows := collectMappedRows(t, plan)
	if len(rows) != 2 || rows[0].Text != "first" || rows[1].Text != "second" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestRecordMapClassifiesMainContentFromOneScalarField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wikimedia.jsonl")
	contents := "{\"text\":\"Article\",\"metadata\":{\"namespace\":0}}\n" +
		"{\"text\":\"Discussion\",\"metadata\":{\"namespace\":1}}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{
		Type: ProfileRecordMap, MainContent: map[string]any{"metadata.namespace": 0},
		Fields: ProfileFields{Text: []string{"text"}},
	}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	if len(rows) != 2 || !rows[0].MainContent || rows[1].MainContent {
		t.Fatalf("main-content rows = %+v", rows)
	}
}

func TestRecordMapMainContentDefaultsTrueAndMissingDeclaredFieldFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	if err := os.WriteFile(path, []byte("{\"text\":\"Article\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{Text: []string{"text"}}}
	if rows := collectMappedRows(t, mappedFixturePlan(t, path, plain)); len(rows) != 1 || !rows[0].MainContent {
		t.Fatalf("default main-content rows = %+v", rows)
	}
	declared := plain
	declared.MainContent = map[string]any{"metadata.namespace": 0}
	err := StreamCanonicalTextBatches(context.Background(), mappedFixturePlan(t, path, declared), func(TextBatch) error { return nil })
	if err == nil || !strings.Contains(err.Error(), `main_content classification failed: field "metadata.namespace" is absent`) {
		t.Fatalf("missing main-content field error = %v", err)
	}
}

func TestRecordMapExplicitlyReplacesNULWithSpace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	if err := os.WriteFile(path, []byte("{\"text\":\"before\\u0000after\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileRecordMap, NUL: "space", Fields: ProfileFields{Text: []string{"text"}}})
	rows := collectMappedRows(t, plan)
	if len(rows) != 1 || rows[0].Text != "before after" {
		t.Fatalf("rows = %+v", rows)
	}
}

type mappedParquetMetadata struct {
	License string `parquet:"license"`
}

type mappedParquetRow struct {
	Title    string                `parquet:"title"`
	Body     string                `parquet:"body"`
	ID       int64                 `parquet:"id"`
	Metadata mappedParquetMetadata `parquet:"metadata"`
}

func TestRecordMapReadsMappedParquetFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.parquet")
	if err := parquet.WriteFile(path, []mappedParquetRow{{Title: "Heading", Body: "Body", ID: 7, Metadata: mappedParquetMetadata{License: "CC0-1.0"}}}); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{
		Text: []string{"title", "body"}, ID: "id", License: "metadata.license",
	}})
	rows := collectMappedRows(t, plan)
	if len(rows) != 1 || rows[0].Text != "Heading\n\nBody" || rows[0].Source != "7" || rows[0].License != "CC0-1.0" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestPerRecordLicensesShareObjectsAndRemainInManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "licenses.jsonl")
	contents := "{\"text\":\"same\",\"license\":\"CC0-1.0\"}\n{\"text\":\"same\",\"license\":\"CC-BY-4.0\"}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{Text: []string{"text"}, License: "license"}})
	assembly, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if assembly.RetainedDocs != 2 || assembly.DuplicateDocs != 0 || len(assembly.Objects) != 1 {
		t.Fatalf("assembly = %+v", assembly)
	}
	if got := strings.Join(assembly.Objects[0].Licenses, ","); got != "CC-BY-4.0,CC0-1.0" {
		t.Fatalf("object licenses = %s", got)
	}
	manifest, err := BuildManifest(plan, assembly, "s3://openwaldo/lookaside/v1")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(manifest.Licenses, ","); got != "CC-BY-4.0,CC0-1.0" || len(manifest.Shards) != 1 || strings.Join(manifest.Shards[0].Licenses, ",") != got {
		t.Fatalf("manifest shards = %+v", manifest.Shards)
	}
}

func TestMappedLicensePolicyExcludesAndCountsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "licenses.jsonl")
	contents := "{\"text\":\"keep\",\"license\":\"CC0-1.0\"}\n{\"text\":\"omit\",\"license\":\"LicenseRef-Proprietary\"}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{
		Type:          ProfileRecordMap,
		Fields:        ProfileFields{Text: []string{"text"}, License: "license"},
		LicensePolicy: corpus.LicensePolicy{Include: []string{"CC*"}},
	}
	assembly, err := AssembleTextObjects(context.Background(), mappedFixturePlan(t, path, profile), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if assembly.RetainedDocs != 1 || assembly.RejectedDocs != 1 || assembly.Rejections[RejectionLicense] != 1 {
		t.Fatalf("assembly = %+v", assembly)
	}
}

func TestDialoguePairRendersPromptContextAndResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dialogue.jsonl")
	contents := "{\"instruction\":\"Summarize\",\"context\":\"A long passage\",\"response\":\"Short summary\"}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileDialoguePair, Fields: ProfileFields{
		Text: []string{"instruction"}, Context: "context", Response: "response",
	}})
	rows := collectMappedRows(t, plan)
	want := "User: Summarize\n\nA long passage\n\nAssistant: Short summary\n"
	if len(rows) != 1 || rows[0].Text != want || rows[0].Meta == nil || !strings.Contains(*rows[0].Meta, `"turns":2`) {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestDialoguePairExplicitlySkipsAndCountsEmptyRequiredFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dialogue.jsonl")
	contents := "{\"prompt\":\"Question\",\"reply\":\"Answer\"}\n" +
		"{\"prompt\":\"\",\"reply\":\"Orphan reply\"}\n" +
		"{\"prompt\":\"Orphan prompt\",\"reply\":\"  \"}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{
		Type: ProfileDialoguePair, OnEmpty: "skip",
		Fields: ProfileFields{Text: []string{"prompt"}, Response: "reply"},
	})
	result, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result.InputDocs != 3 || result.RetainedDocs != 1 || result.DuplicateDocs != 0 || result.RejectedDocs != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRankedConversationTreeChoosesLowestRankAtEveryLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation-tree.json")
	contents := `{
  "message_tree_id":"tree-1",
  "prompt":{"text":"Question","role":"prompter","children":{"replies":[
    {"text":"Worse","role":"assistant","rank":2,"children":{"replies":[]}},
    {"text":"Better","role":"assistant","rank":0,"children":{"replies":[
      {"text":"Follow up","role":"prompter","rank":1,"children":{"replies":[
        {"text":"Final answer","role":"assistant","rank":0,"children":{"replies":[]}}
      ]}}
    ]}}
  ]}}
}`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{
		Type:   ProfileRankedConversationTree,
		Fields: ProfileFields{ID: "message_tree_id"},
		Tree:   ConversationTree{Root: "prompt", Replies: "children.replies", Text: "text", Rank: "rank", Role: "role", AssistantRole: "assistant"},
	})
	rows := collectMappedRows(t, plan)
	want := "User: Question\n\nAssistant: Better\n\nUser: Follow up\n\nAssistant: Final answer\n"
	if len(rows) != 1 || rows[0].Text != want || rows[0].Source != "tree-1" || rows[0].Meta == nil || !strings.Contains(*rows[0].Meta, `"turns":4`) {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestRankedConversationTreeUsesDeclaredSourceOrderForUnrankedLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation-tree.json")
	contents := `{
  "prompt":{"text":"Question","role":"prompter","replies":[
    {"text":"Unranked answer","role":"assistant","replies":[]},
    {"text":"Ranked answer","role":"assistant","rank":2,"replies":[
      {"text":"First follow up","role":"prompter","replies":[]},
      {"text":"Second follow up","role":"prompter","replies":[]}
    ]}
  ]}
}`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{
		Type: ProfileRankedConversationTree,
		Tree: ConversationTree{Root: "prompt", Replies: "replies", Text: "text", Rank: "rank", MissingRank: "source-order", Role: "role", AssistantRole: "assistant"},
	})
	rows := collectMappedRows(t, plan)
	want := "User: Question\n\nAssistant: Ranked answer\n\nUser: First follow up\n"
	if len(rows) != 1 || rows[0].Text != want {
		t.Fatalf("rows = %+v", rows)
	}
}

func mappedFixturePlan(t *testing.T, path string, profile InputProfile) Plan {
	t.Helper()
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/mapped", Title: "Mapped", License: "LicenseRef-Default",
		Source:  PlanSource{Name: "mapped", URL: "https://example.test", Category: "public-dataset"},
		Profile: profile,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func collectMappedRows(t *testing.T, plan Plan) []shard.TextRow {
	t.Helper()
	var rows []shard.TextRow
	if err := StreamCanonicalTextBatches(context.Background(), plan, func(batch TextBatch) error {
		rows = append(rows, batch.Rows...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}
