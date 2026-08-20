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
	"github.com/openwaldo/waldo/internal/record"
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

func TestRecordMapAcceptsTopLevelJSONArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, []byte(`[{"text":"one"},{"text":"two"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{Text: []string{"text"}}})
	rows := collectMappedRows(t, plan)
	if len(rows) != 2 || rows[0].Text != "one" || rows[1].Text != "two" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestChatMessagesPreservesSeparateSystemPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.json")
	contents := `{"system":"Use careful reasoning.","messages":[{"role":"user","content":"Why?"},{"role":"assistant","content":"Because."}]}`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{Type: ProfileChatMessages, Messages: ChatMessagesMapping{
		Role: "messages[].role", Content: "messages[].content", System: "system",
	}}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	conversation, err := record.DecodeConversation(rows[0].Text)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation.Messages) != 3 || conversation.Messages[0].Role != "system" || conversation.Messages[0].Content != "Use careful reasoning." {
		t.Fatalf("conversation = %+v", conversation)
	}
}

func TestDialoguePairPreservesTools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tools.json")
	contents := `{"query":"Find it","answer":"[{\"name\":\"search\"}]","tools":"[{\"name\":\"search\"}]"}`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{Type: ProfileDialoguePair, Fields: ProfileFields{
		Text: []string{"query"}, Response: "answer", Tools: "tools",
	}}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	conversation, err := record.DecodeConversation(rows[0].Text)
	if err != nil {
		t.Fatal(err)
	}
	if string(conversation.Tools) != `[{"name":"search"}]` {
		t.Fatalf("tools = %s", conversation.Tools)
	}
}

func TestParquetMappingMatchesTerminalRepeatedScalar(t *testing.T) {
	physical := []string{"chat_template_kwargs", "xml_tools", "list", "element"}
	if !parquetPathMatches("chat_template_kwargs.xml_tools[]", physical) {
		t.Fatal("terminal repeated scalar did not match its Parquet LIST wrappers")
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

func TestRecordMapClassifiesMainContentFromConjoinedScalarFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quality.jsonl")
	contents := "{\"text\":\"keep\",\"helpfulness\":4,\"correctness\":4,\"coherence\":4}\n" +
		"{\"text\":\"omit\",\"helpfulness\":4,\"correctness\":3,\"coherence\":4}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{
		Type:        ProfileRecordMap,
		MainContent: map[string]any{"helpfulness": 4, "correctness": 4, "coherence": 4},
		Fields:      ProfileFields{Text: []string{"text"}},
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

func TestRecordMapMainContentChecksEveryDeclaredFieldForSchemaDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	if err := os.WriteFile(path, []byte("{\"text\":\"Auxiliary\",\"a\":0}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{
		Type: ProfileRecordMap, MainContent: map[string]any{"a": 1, "z": 1},
		Fields: ProfileFields{Text: []string{"text"}},
	}
	err := StreamCanonicalTextBatches(context.Background(), mappedFixturePlan(t, path, profile), func(TextBatch) error { return nil })
	if err == nil || !strings.Contains(err.Error(), `main_content classification failed: field "z" is absent`) {
		t.Fatalf("missing later main-content field error = %v", err)
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

func TestMappedParquetReportsProgressWithinFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.parquet")
	if err := parquet.WriteFile(path, []mappedParquetRow{
		{Title: "First", Body: "Body", ID: 1, Metadata: mappedParquetMetadata{License: "CC0-1.0"}},
		{Title: "Second", Body: "Body", ID: 2, Metadata: mappedParquetMetadata{License: "CC0-1.0"}},
	}); err != nil {
		t.Fatal(err)
	}
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{
		Text: []string{"title", "body"}, ID: "id", License: "metadata.license",
	}})
	var progress []int64
	err := streamMappedParquet(t.Context(), plan, plan.Inputs[0], func(_ shard.TextRow, inputBytes int64) error {
		progress = append(progress, inputBytes)
		return nil
	}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) != 2 || progress[0] <= 0 || progress[0] >= plan.Inputs[0].Artifact.Bytes || progress[1] != plan.Inputs[0].Artifact.Bytes {
		t.Fatalf("progress = %v, total = %d", progress, plan.Inputs[0].Artifact.Bytes)
	}
	plan.Writer.AdapterBatchBytes = 1
	var events []ProgressEvent
	ctx := WithProgress(t.Context(), func(event ProgressEvent) { events = append(events, event) })
	if _, err := AssembleTextObjects(ctx, plan, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	foundIntermediate := false
	for _, event := range events {
		if event.Phase == "ingest" && event.Status == "records" && event.Bytes > 0 && event.Bytes < event.TotalBytes {
			foundIntermediate = true
		}
	}
	if !foundIntermediate {
		t.Fatalf("ingest events lack intermediate byte progress: %+v", events)
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
	if len(manifest.Shards[0].LicenseUsage) != 2 {
		t.Fatalf("mixed-license shard usage = %+v", manifest.Shards[0].LicenseUsage)
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
	want := `{"messages":[{"role":"user","content":"Summarize","context":"A long passage"},{"role":"assistant","content":"Short summary"}]}`
	if len(rows) != 1 || rows[0].Text != want || rows[0].Meta == nil || !strings.Contains(*rows[0].Meta, `"turns":2`) {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestDialoguePairPreservesMappedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dialogue.jsonl")
	contents := "{\"prompt\":\"Question\",\"response\":\"Answer\",\"helpfulness\":4,\"correctness\":3}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{
		Type: ProfileDialoguePair,
		Fields: ProfileFields{
			Text: []string{"prompt"}, Response: "response",
			Meta: map[string]string{"helpfulness": "helpfulness", "correctness": "correctness"},
		},
	}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	if len(rows) != 1 || rows[0].Meta == nil || !strings.Contains(*rows[0].Meta, `"helpfulness":"4"`) || !strings.Contains(*rows[0].Meta, `"correctness":"3"`) || !strings.Contains(*rows[0].Meta, `"format":"structured-conversation"`) {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestChatMessagesPreservesRolesAndToolResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.jsonl")
	contents := `{"id":"tool-1","tools":"[{\"name\":\"weather\"}]","messages":[{"role":"human","content":"Weather?"},{"role":"model","content":"<tool_call>weather</tool_call>"},{"role":"tool","content":"Sunny"},{"role":"model","content":"It is sunny."}]}` + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{
		Type:   ProfileChatMessages,
		Fields: ProfileFields{ID: "id"},
		Messages: ChatMessagesMapping{
			Role: "messages[].role", Content: "messages[].content", Tools: "tools",
			RoleAliases: map[string]string{"human": "user", "model": "assistant"},
		},
	}
	plan := mappedFixturePlan(t, path, profile)
	rows := collectMappedRows(t, plan)
	want := `{"messages":[{"role":"user","content":"Weather?"},{"role":"assistant","content":"\u003ctool_call\u003eweather\u003c/tool_call\u003e"},{"role":"tool","content":"Sunny"},{"role":"assistant","content":"It is sunny."}],"tools":[{"name":"weather"}]}`
	if len(rows) != 1 || rows[0].Text != want || rows[0].Meta == nil || !strings.Contains(*rows[0].Meta, `"turns":4`) {
		t.Fatalf("rows = %+v", rows)
	}
	assembly, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest(plan, assembly, "s3://openwaldo/lookaside/v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(assembly.Objects) != 1 || assembly.Objects[0].RecordKind != record.KindConversation || manifest.RecordKind != record.KindConversation || manifest.ConvertedBy.Recipe != shard.ConversationWriterRecipe {
		t.Fatalf("conversation assembly = %+v, manifest = %+v", assembly, manifest)
	}
}

type chatParquetMessage struct {
	Role    string `parquet:"role"`
	Content string `parquet:"content"`
}

type chatParquetRow struct {
	ID       string               `parquet:"id"`
	Messages []chatParquetMessage `parquet:"messages,list"`
}

func TestChatMessagesReadsNestedParquetLists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.parquet")
	if err := parquet.WriteFile(path, []chatParquetRow{{
		ID: "chat-1",
		Messages: []chatParquetMessage{
			{Role: "user", Content: "Question"},
			{Role: "assistant", Content: "Answer"},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{
		Type: ProfileChatMessages, Fields: ProfileFields{ID: "id"},
		Messages: ChatMessagesMapping{Role: "messages[].role", Content: "messages[].content"},
	}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	if len(rows) != 1 || rows[0].Text != `{"messages":[{"role":"user","content":"Question"},{"role":"assistant","content":"Answer"}]}` {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestChatMessagesReplacesNULByDefaultAndSupportsStrictMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.jsonl")
	contents := `{"messages":[{"role":"user","content":"Question"},{"role":"assistant","content":"before\u0000after"}]}` + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{
		Type:     ProfileChatMessages,
		Messages: ChatMessagesMapping{Role: "messages[].role", Content: "messages[].content"},
	}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	conversation, err := record.DecodeConversation(rows[0].Text)
	if err != nil {
		t.Fatal(err)
	}
	if got := conversation.Messages[1].Content; got != "before after" {
		t.Fatalf("default normalized content = %q", got)
	}
	profile.NUL = "error"
	err = StreamCanonicalTextBatches(context.Background(), mappedFixturePlan(t, path, profile), func(TextBatch) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "set nul = space in the fetcher [input] profile") {
		t.Fatalf("NUL policy error = %v", err)
	}
}

func TestChatMessagesPrivacyCheckPreservesMessageBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.jsonl")
	contents := `{"messages":[{"role":"user","content":"Received: this is ordinary message content"},{"role":"assistant","content":"Subject: response\n\nNothing private here."}]}` + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := InputProfile{
		Type:     ProfileChatMessages,
		Messages: ChatMessagesMapping{Role: "messages[].role", Content: "messages[].content"},
	}
	result, err := AssembleTextObjects(t.Context(), mappedFixturePlan(t, path, profile), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result.InputDocs != 1 || result.RetainedDocs != 1 {
		t.Fatalf("assembly = %+v", result)
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
	want := `{"messages":[{"role":"user","content":"Question"},{"role":"assistant","content":"Better"},{"role":"user","content":"Follow up"},{"role":"assistant","content":"Final answer"}]}`
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
	want := `{"messages":[{"role":"user","content":"Question"},{"role":"assistant","content":"Ranked answer"},{"role":"user","content":"First follow up"}]}`
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
