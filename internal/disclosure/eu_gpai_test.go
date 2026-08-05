package disclosure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openwaldo/waldo-new/internal/corpus"
	"github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/model"
	"github.com/openwaldo/waldo-new/internal/training"
)

func TestBuildEUGPAIReportDeduplicatesCorporaAndReportsFacts(t *testing.T) {
	bom := corpus.BOM{
		Kind: "openwaldo-bom", Schema: 1, Subject: "corpus", Paths: []string{"books"},
		Totals: index.Measures{Shards: 1, Docs: 10, Tokens: 100, Bytes: 200},
		Manifests: []corpus.ManifestPin{{
			Path: "books/books.json", SHA256: strings.Repeat("a", 64), Name: "books", Title: "Books",
			Sources: []index.Source{{Name: "source", Source: "Example", URL: "https://example.test", SHA256: strings.Repeat("b", 64)}},
		}},
	}
	inspection := model.Inspection{
		Model: model.ModelRecord{ID: strings.Repeat("1", 64), Name: "smoke", PlanSHA256: strings.Repeat("2", 64), ArchitectureSHA256: strings.Repeat("3", 64)},
		BOM:   model.ModelBOM{Kind: "openwaldo-bom", Schema: 1, Subject: "model", ModelID: strings.Repeat("1", 64)},
		Runs: []model.RunRecord{
			{State: model.RunComplete, Observation: &training.Observation{Simulated: true, ConsumedTokens: 100}},
			{State: model.RunComplete, Observation: &training.Observation{Simulated: true, ConsumedTokens: 50}},
		},
		RunBOMs: []model.RunBOM{
			{Ordinal: 1, Stage: "pretrain", StageType: "pre-training", Objective: "causal-language-modeling", CorpusBOMSHA256: strings.Repeat("c", 64), CorpusBOM: bom, Parameters: training.ResolvedParameters{Steps: 1, BatchSize: 1, SequenceLength: 100}},
			{Ordinal: 2, Stage: "review", StageType: "alignment", Objective: "causal-language-modeling", CorpusBOMSHA256: strings.Repeat("c", 64), CorpusBOM: bom, Parameters: training.ResolvedParameters{Steps: 1, BatchSize: 1, SequenceLength: 50}},
		},
	}
	report, err := BuildEUGPAIReport(inspection, nil, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "incomplete-draft" || len(report.Training.UniqueCorpora) != 1 || report.Training.UniqueCorpora[0].Uses != 2 || len(report.Training.Stages) != 2 {
		t.Fatalf("report summary = %+v", report)
	}
	for _, field := range []string{"provider.profile", "training.observed-consumption", "content.modalities", "source.category", "processing.steps"} {
		if !hasGap(report.Gaps, field) {
			t.Fatalf("missing expected gap %q: %+v", field, report.Gaps)
		}
	}
}

func TestLoadProviderIsStrict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider.json")
	data := `{"kind":"waldo-eu-gpai-provider-profile","schema":1,"provider":{"name":"Example","address":"Here","contact":"mail"},"model":{"public_name":"Model","version":"1","market_placement_date":"2026-01-01","origin":"new"},"code_of_practice_status":"signatory","copyright_policy_url":"https://example.test/policy","extra":true}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProvider(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("LoadProvider() error = %v", err)
	}
}

func hasGap(gaps []Gap, field string) bool {
	for _, gap := range gaps {
		if gap.Field == field {
			return true
		}
	}
	return false
}
