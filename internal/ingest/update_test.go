package ingest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/openwaldo/waldo/internal/record"
)

func TestAppendSeedDropsExistingContentWithoutProducingShard(t *testing.T) {
	input := filepath.Join(t.TempDir(), "document.txt")
	writeFixture(t, input, "already indexed")
	plan := textFixturePlan(t, input)
	result, err := AssembleTextObjectsWithSeedAndSink(context.Background(), plan, t.TempDir(), func(add func([]DedupIdentity) error) error {
		return add([]DedupIdentity{{SHA256: record.TextHash("already indexed"), License: plan.License}})
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.InputDocs != 1 || result.RetainedDocs != 0 || result.DuplicateDocs != 1 || len(result.Objects) != 0 {
		t.Fatalf("result = %+v", result)
	}
}
