// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/shard"
	"github.com/openwaldo/waldo/internal/tokenizer"
)

func TestBuildManifestMatchesCurrentIndexContract(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, filepath.Join(directory, "a.txt"), "same")
	writeFixture(t, filepath.Join(directory, "b.txt"), "same")
	probe, err := ProbePaths(context.Background(), []string{directory})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/fixture", Title: "Fixture", Description: "Fixture corpus.", License: "CC0-1.0",
		Source: PlanSource{Name: "fixture-source", URL: "https://example.test/data", Category: "public-dataset", Content: &index.Content{Languages: []string{"en"}, ProgrammingLanguages: []string{"Go"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest(plan, assembly, "s3://openwaldo/lookaside/v1")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != index.ManifestSchema || manifest.RecordSchema != shard.TextRecordSchema || len(manifest.Shards) != 1 || manifest.Shards[0].Docs != 1 || manifest.Shards[0].Tokens <= 0 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.Content == nil || len(manifest.Content.Languages) != 1 || manifest.Content.Languages[0] != "en" || len(manifest.Content.ProgrammingLanguages) != 1 || manifest.Content.ProgrammingLanguages[0] != "Go" {
		t.Fatalf("manifest language declaration = %+v", manifest.Content)
	}
	if manifest.Format != "" || manifest.Processing != nil || manifest.ComposedBy != nil || len(manifest.Sources[0].Files) != 0 || len(manifest.Sources[0].Usage) != 0 || manifest.Sources[0].Content == nil || len(manifest.Shards[0].Modalities) != 0 {
		t.Fatalf("generated manifest contains expanded metadata: %+v", manifest)
	}
	if manifest.ConvertedBy.Tokenizer != tokenizer.Default {
		t.Fatalf("tokenizer = %q", manifest.ConvertedBy.Tokenizer)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"files"`, `"usage"`, `"processing"`, `"composed_by"`, `"modalities"`, `"format"`} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("manifest contains expanded field %s: %s", forbidden, data)
		}
	}
	var roundTrip index.Manifest
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if len(roundTrip.Shards) != 1 || roundTrip.Shards[0].SHA256 != manifest.Shards[0].SHA256 {
		t.Fatalf("round-trip manifest = %+v", roundTrip)
	}
	if roundTrip.Redaction == nil || roundTrip.Shards[0].Redaction == nil || *roundTrip.Redaction != *manifest.Redaction {
		t.Fatalf("round-trip redaction = %+v / %+v", roundTrip.Redaction, roundTrip.Shards[0].Redaction)
	}
}

func TestIngestionFlagsContentAssessmentsAndSummarizesManifest(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, filepath.Join(directory, "email.txt"), "Contact maintainer@example.org for details.\n")
	writeFixture(t, filepath.Join(directory, "plain.txt"), "No contact address in this record.\n")
	writeFixture(t, filepath.Join(directory, "boilerplate.txt"), strings.Repeat("Repeated navigation and footer line.\n", 8))
	writeFixture(t, filepath.Join(directory, "repetitive.txt"), strings.Repeat("alpha beta gamma delta epsilon zeta eta theta ", 12))
	probe, err := ProbePaths(context.Background(), []string{directory})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/email-fixture", Title: "Email fixture", Description: "Email assessment fixture.", License: "CC0-1.0",
		Source: PlanSource{Name: "fixture", URL: "https://example.test/data", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest(plan, assembly, "https://objects.example")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Assessment == nil || manifest.Assessment.EmailAddresses == nil || manifest.Assessment.EmailAddresses.Detector != shard.EmailDetector || manifest.Assessment.EmailAddresses.Records != 0 {
		t.Fatalf("manifest assessment = %+v", manifest.Assessment)
	}
	if manifest.Redaction == nil || manifest.Redaction.Policy != shard.PrivacyRedactionPolicy || !manifest.Redaction.NamesRetained || manifest.Redaction.EmailAddresses != 1 {
		t.Fatalf("manifest redaction = %+v", manifest.Redaction)
	}
	if manifest.Assessment.RepetitiveContent == nil || manifest.Assessment.RepetitiveContent.Detector != shard.RepetitionDetector || manifest.Assessment.RepetitiveContent.Records != 1 {
		t.Fatalf("repetitive assessment = %+v", manifest.Assessment)
	}
	if manifest.Assessment.BoilerplateContent == nil || manifest.Assessment.BoilerplateContent.Detector != shard.BoilerplateDetector || manifest.Assessment.BoilerplateContent.Records != 1 {
		t.Fatalf("boilerplate assessment = %+v", manifest.Assessment)
	}
	var emailFlagged, repetitiveFlagged, boilerplateFlagged, assessed int
	if err := shard.WalkRecords(assembly.Objects[0].Path, func(_ int64, view shard.RecordView) error {
		if view.EmailAddresses == nil || view.RepetitiveContent == nil || view.BoilerplateContent == nil {
			t.Fatal("schema-2 row has incomplete content assessment")
		}
		assessed++
		if *view.EmailAddresses {
			emailFlagged++
		}
		if strings.Contains(view.Text, "maintainer@example.org") {
			t.Fatal("canonical shard retained an email address")
		}
		if *view.RepetitiveContent {
			repetitiveFlagged++
		}
		if *view.BoilerplateContent {
			boilerplateFlagged++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if assessed != 4 || emailFlagged != 0 || repetitiveFlagged != 1 || boilerplateFlagged != 1 {
		t.Fatalf("assessed/email/repetitive/boilerplate rows = %d/%d/%d/%d", assessed, emailFlagged, repetitiveFlagged, boilerplateFlagged)
	}
}

func TestDeclarativeProfileIsPartOfConversionAndSourceIdentity(t *testing.T) {
	base := Plan{Inputs: []PlanInput{{
		Artifact: Artifact{SHA256: fmt.Sprintf("%064x", 1), Bytes: 10, Format: "json"}, Adapter: "json",
		Profile: InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{Text: []string{"body"}}},
	}}}
	changed := base
	changed.Inputs = append([]PlanInput(nil), base.Inputs...)
	changed.Inputs[0].Profile.Fields.Text = []string{"content"}
	baseSource, err := sourceAcquisitionIdentity(base, "")
	if err != nil {
		t.Fatal(err)
	}
	changedSource, err := sourceAcquisitionIdentity(changed, "")
	if err != nil {
		t.Fatal(err)
	}
	if baseSource == changedSource || conversionProfile(base) == conversionProfile(changed) {
		t.Fatal("profile mapping did not change persisted identities")
	}
}

func TestMainContentClassificationChangesPlanAndSourceIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	writeFixture(t, path, `{"metadata":{"namespace":0},"text":"main"}`)
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	base, err := NewPlan(probe, PlanRequest{
		Destination: "core/main-content", Title: "Main content", License: "CC0-1.0",
		Source:  PlanSource{Name: "fixture", URL: "https://example.test/data", Category: "public-dataset"},
		Profile: InputProfile{Type: ProfileRecordMap, MainContent: map[string]any{"metadata.namespace": 0}, Fields: ProfileFields{Text: []string{"text"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.Inputs = append([]PlanInput(nil), base.Inputs...)
	changed.Inputs[0].Profile.MainContent = map[string]any{"metadata.namespace": 1}
	basePlan, err := base.Identity()
	if err != nil {
		t.Fatal(err)
	}
	changedPlan, err := changed.Identity()
	if err != nil {
		t.Fatal(err)
	}
	baseSource, err := sourceAcquisitionIdentity(base, "")
	if err != nil {
		t.Fatal(err)
	}
	changedSource, err := sourceAcquisitionIdentity(changed, "")
	if err != nil {
		t.Fatal(err)
	}
	if basePlan == changedPlan || baseSource == changedSource {
		t.Fatal("main-content classification did not change plan and source identities")
	}
}

func TestSourceEvidenceChangesPlanAndAcquisitionIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.txt")
	writeFixture(t, path, "source evidence identity")
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	base, err := NewPlan(probe, PlanRequest{
		Destination: "core/evidence", Title: "Evidence", License: "Apache-2.0",
		Source: PlanSource{
			Name: "evidence", URL: "https://example.test/data", Category: "public-dataset",
			Content: &index.Content{Types: []string{"text"}, Languages: []string{"en"}, ProgrammingLanguages: []string{"Python"}, Selection: "All records."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.Source = base.Source
	changed.Source.Content = &index.Content{Types: []string{"text"}, Selection: "Only the train split."}
	basePlan, err := base.Identity()
	if err != nil {
		t.Fatal(err)
	}
	changedPlan, err := changed.Identity()
	if err != nil {
		t.Fatal(err)
	}
	baseSource, err := sourceAcquisitionIdentity(base, "")
	if err != nil {
		t.Fatal(err)
	}
	changedSource, err := sourceAcquisitionIdentity(changed, "")
	if err != nil {
		t.Fatal(err)
	}
	if basePlan == changedPlan || baseSource == changedSource {
		t.Fatal("source selection did not change plan and acquisition identities")
	}
}

func TestBuildManifestSizeDoesNotScaleWithInputArtifactCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed.txt")
	writeFixture(t, path, "seed document")
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/large-source", Title: "Large source", Description: "Many acquired files.", License: "CC0-1.0",
		Source: PlanSource{Name: "large-source", URL: "https://example.test/data", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	seed := plan.Inputs[0]
	plan.Inputs = make([]PlanInput, 25_000)
	root := t.TempDir()
	for position := range plan.Inputs {
		input := seed
		input.Artifact.Path = filepath.Join(root, fmt.Sprintf("%06d.txt", position))
		input.SourcePath = fmt.Sprintf("%06d.txt", position)
		plan.Inputs[position] = input
	}
	assembly := AssemblyResult{
		Objects:   []ObjectResult{{SHA256: fmt.Sprintf("%064x", 1), Bytes: 1024, Docs: 25_000, Tokens: 50_000, LogicalBytes: 500_000, License: plan.License, Redaction: index.ContentRedaction{Policy: shard.PrivacyRedactionPolicy, NamesRetained: true}}},
		InputDocs: 25_000, RetainedDocs: 25_000,
	}
	manifest, err := BuildManifest(plan, assembly, "s3://openwaldo/lookaside/v1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 4<<10 {
		t.Fatalf("manifest is %d bytes for 25,000 inputs; want at most 4 KiB", len(data))
	}
	if bytes.Contains(data, []byte(`"files"`)) {
		t.Fatal("manifest contains per-input evidence")
	}
}

func TestCompactCollectorPinsCleanAndDirtyRecipes(t *testing.T) {
	clean := &index.IngestRecipeEvidence{
		Path: "recipes/common-pile/foodista.yaml", Repository: "git@github.com:openwaldo/waldo-fetchers.git",
		Commit: "abc123", SHA256: fmt.Sprintf("%064x", 1),
	}
	if got, want := compactCollector(clean), "git@github.com:openwaldo/waldo-fetchers@abc123:recipes/common-pile/foodista.yaml"; got != want {
		t.Fatalf("clean collector = %q, want %q", got, want)
	}
	dirty := *clean
	dirty.Dirty = true
	if got, want := compactCollector(&dirty), "git@github.com:openwaldo/waldo-fetchers@abc123+dirty:recipes/common-pile/foodista.yaml#sha256="+dirty.SHA256; got != want {
		t.Fatalf("dirty collector = %q, want %q", got, want)
	}
}
