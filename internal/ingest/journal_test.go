package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/openwaldo/waldo-new/internal/lookaside"
)

type fakePublisher struct {
	mu         sync.Mutex
	objects    map[string]int64
	active     int
	maxActive  int
	failOnce   bool
	publishTry int
}

func (publisher *fakePublisher) BaseURL() string { return "s3://fixture/lookaside/v1" }

func (publisher *fakePublisher) Publish(ctx context.Context, source, digest string, size int64, progress func(lookaside.PublishProgress)) (lookaside.PublishedObject, error) {
	publisher.mu.Lock()
	publisher.publishTry++
	publisher.active++
	if publisher.active > publisher.maxActive {
		publisher.maxActive = publisher.active
	}
	shouldFail := publisher.failOnce
	publisher.failOnce = false
	publisher.mu.Unlock()
	defer func() {
		publisher.mu.Lock()
		publisher.active--
		publisher.mu.Unlock()
	}()
	if err := lookaside.VerifyFile(source, digest, size); err != nil {
		return lookaside.PublishedObject{}, err
	}
	select {
	case <-time.After(75 * time.Millisecond):
	case <-ctx.Done():
		return lookaside.PublishedObject{}, ctx.Err()
	}
	if shouldFail {
		return lookaside.PublishedObject{}, fmt.Errorf("injected upload failure")
	}
	if progress != nil {
		progress(lookaside.PublishProgress{Written: size, Total: size})
	}
	publisher.mu.Lock()
	if publisher.objects == nil {
		publisher.objects = map[string]int64{}
	}
	publisher.objects[digest] = size
	publisher.mu.Unlock()
	return lookaside.PublishedObject{URL: publisher.BaseURL() + "/" + digest[:2] + "/" + digest[2:4] + "/" + digest, SHA256: digest, Bytes: size}, nil
}

func (publisher *fakePublisher) Verify(_ context.Context, digest string, size int64) (lookaside.PublishedObject, error) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if publisher.objects[digest] != size {
		return lookaside.PublishedObject{}, fmt.Errorf("remote object mismatch")
	}
	return lookaside.PublishedObject{URL: publisher.BaseURL() + "/" + digest[:2] + "/" + digest[2:4] + "/" + digest, SHA256: digest, Bytes: size, AlreadyExists: true}, nil
}

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

func TestExecutePublicationOverlapsUploadsAndPurgesVerifiedObjects(t *testing.T) {
	input := publicationFixtureDirectory(t, "alpha", "beta", "gamma", "delta")
	plan := textFixturePlan(t, input)
	plan.Writer.RowGroupLogicalBytes = 5
	plan.Writer.CompressedTarget = 1
	plan.Writer.CompressedMaximum = 1 << 20
	staging := t.TempDir()
	publisher := &fakePublisher{}
	var events []ProgressEvent
	ctx := WithProgress(context.Background(), func(event ProgressEvent) { events = append(events, event) })
	assembly, publication, err := ExecutePublication(ctx, plan, staging, publisher, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(assembly.Objects) < 3 || len(publication.Objects) != len(assembly.Objects) {
		t.Fatalf("assembly/publication = %+v / %+v", assembly, publication)
	}
	if publisher.maxActive != 2 {
		t.Fatalf("max concurrent uploads = %d, want 2", publisher.maxActive)
	}
	for _, object := range assembly.Objects {
		if _, err := os.Stat(object.Path); !os.IsNotExist(err) {
			t.Fatalf("staged object %s was not purged", object.Path)
		}
	}
	var ready, verified, purged int
	for _, event := range events {
		switch event.Status {
		case "ready":
			ready++
		case "verified":
			verified++
		case "purged":
			purged++
		}
	}
	if ready != len(assembly.Objects) || verified != ready || purged != ready {
		t.Fatalf("progress ready=%d verified=%d purged=%d", ready, verified, purged)
	}
	if _, _, err := ExecutePublication(context.Background(), plan, staging, publisher, 2); err != nil {
		t.Fatalf("resume published ingestion: %v", err)
	}
}

func TestExecutePublicationRecoversAfterPartialFailure(t *testing.T) {
	input := publicationFixtureDirectory(t, "alpha", "beta", "gamma")
	plan := textFixturePlan(t, input)
	plan.Writer.RowGroupLogicalBytes = 5
	plan.Writer.CompressedTarget = 1
	plan.Writer.CompressedMaximum = 1 << 20
	staging := t.TempDir()
	publisher := &fakePublisher{failOnce: true}
	if _, _, err := ExecutePublication(context.Background(), plan, staging, publisher, 2); err == nil {
		t.Fatal("expected injected upload failure")
	}
	assembly, publication, err := ExecutePublication(context.Background(), plan, staging, publisher, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(publication.Objects) != len(assembly.Objects) {
		t.Fatalf("recovered publication = %+v", publication)
	}
}

func publicationFixtureDirectory(t *testing.T, documents ...string) string {
	t.Helper()
	directory := t.TempDir()
	for index, document := range documents {
		writeFixture(t, filepath.Join(directory, fmt.Sprintf("%02d.txt", index)), document)
	}
	return directory
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
