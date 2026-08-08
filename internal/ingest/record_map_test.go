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

func TestPerRecordLicensesPartitionObjectsAndManifest(t *testing.T) {
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
	if assembly.RetainedDocs != 2 || assembly.DuplicateDocs != 0 || len(assembly.Objects) != 2 {
		t.Fatalf("assembly = %+v", assembly)
	}
	licenses := map[string]bool{}
	for _, object := range assembly.Objects {
		licenses[object.License] = true
	}
	if !licenses["CC0-1.0"] || !licenses["CC-BY-4.0"] {
		t.Fatalf("object licenses = %v", licenses)
	}
	manifest, err := BuildManifest(plan, assembly, "s3://openwaldo/lookaside/v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range manifest.Shards {
		licenses[manifest.EffectiveLicense(object)] = true
	}
	if !licenses["CC0-1.0"] || !licenses["CC-BY-4.0"] {
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
