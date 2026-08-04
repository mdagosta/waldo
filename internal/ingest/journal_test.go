package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/openwaldo/waldo-new/internal/lookaside"
)

func TestExecuteAssemblyResumesVerifiedJournal(t *testing.T) {
	input := filepath.Join(t.TempDir(), "input.txt")
	writeFixture(t, input, "durable")
	plan := textFixturePlan(t, input)
	staging := t.TempDir()
	first, err := ExecuteAssembly(context.Background(), plan, staging)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(first.Objects[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExecuteAssembly(context.Background(), plan, staging)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(second.Objects[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Objects[0].SHA256 != second.Objects[0].SHA256 || !info.ModTime().Equal(after.ModTime()) {
		t.Fatalf("resume rebuilt object: %+v / %+v", first, second)
	}
}

func TestExecuteAssemblyRefusesChangedPlan(t *testing.T) {
	input := filepath.Join(t.TempDir(), "input.txt")
	writeFixture(t, input, "durable")
	plan := textFixturePlan(t, input)
	staging := t.TempDir()
	if _, err := ExecuteAssembly(context.Background(), plan, staging); err != nil {
		t.Fatal(err)
	}
	plan.Title = "Different"
	if _, err := ExecuteAssembly(context.Background(), plan, staging); err == nil {
		t.Fatal("expected changed plan refusal")
	}
}

func TestExecuteAssemblyRefusesCorruptCheckpoint(t *testing.T) {
	input := filepath.Join(t.TempDir(), "input.txt")
	writeFixture(t, input, "durable")
	plan := textFixturePlan(t, input)
	staging := t.TempDir()
	result, err := ExecuteAssembly(context.Background(), plan, staging)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.Objects[0].Path, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteAssembly(context.Background(), plan, staging); err == nil {
		t.Fatal("expected corrupt checkpoint refusal")
	}
}

func TestExecuteAdmissionPublishesAndResumesLookasideObjects(t *testing.T) {
	input := filepath.Join(t.TempDir(), "input.txt")
	writeFixture(t, input, "durable")
	plan := textFixturePlan(t, input)
	staging := t.TempDir()
	cache, err := lookaside.NewCache(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assembly, first, err := ExecuteAdmission(context.Background(), plan, staging, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Objects) != 1 || first.Objects[0].SHA256 != assembly.Objects[0].SHA256 {
		t.Fatalf("admission = %+v", first)
	}
	_, second, err := ExecuteAdmission(context.Background(), plan, staging, cache)
	if err != nil {
		t.Fatal(err)
	}
	if second.Objects[0] != first.Objects[0] {
		t.Fatalf("resumed admission = %+v, want %+v", second, first)
	}
}

func textFixturePlan(t *testing.T, input string) Plan {
	t.Helper()
	probe, err := ProbePaths(context.Background(), []string{input})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/example", Title: "Example", License: "CC0-1.0",
		Source: PlanSource{Name: "fixture", URL: "https://example.test/data", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
