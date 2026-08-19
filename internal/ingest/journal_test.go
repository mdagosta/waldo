// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openwaldo/waldo/internal/lookaside"
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

func TestPublicationObjectsForAssemblyDropsStaleObjects(t *testing.T) {
	current := []ObjectResult{{SHA256: fmt.Sprintf("%064x", 1)}, {SHA256: fmt.Sprintf("%064x", 2)}}
	published := []PublicationObject{
		{Sequence: 99, SHA256: fmt.Sprintf("%064x", 3)},
		{Sequence: 1, SHA256: current[0].SHA256},
		{Sequence: 2, SHA256: current[1].SHA256},
	}
	filtered := publicationObjectsForAssembly(published, current)
	if len(filtered) != 2 || filtered[0].SHA256 != current[0].SHA256 || filtered[1].SHA256 != current[1].SHA256 {
		t.Fatalf("filtered publication = %+v", filtered)
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

func TestExecutePublicationWaitsForAssemblyThenUploadsAndPurges(t *testing.T) {
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
	lastReady, firstUpload := -1, -1
	for position, event := range events {
		switch event.Status {
		case "ready":
			ready++
			lastReady = position
		case "verified":
			verified++
		case "purged":
			purged++
		}
		if event.Phase == "upload" && firstUpload == -1 {
			firstUpload = position
		}
	}
	if ready != len(assembly.Objects) || verified != ready || purged != ready {
		t.Fatalf("progress ready=%d verified=%d purged=%d", ready, verified, purged)
	}
	if firstUpload <= lastReady {
		t.Fatalf("upload began before assembly completed: last ready event %d, first upload event %d", lastReady, firstUpload)
	}
	if _, _, err := ExecutePublication(context.Background(), plan, staging, publisher, 2); err != nil {
		t.Fatalf("resume published ingestion: %v", err)
	}
}

func TestExecutePublicationDoesNotUploadWhenAssemblyFails(t *testing.T) {
	input := publicationFixtureDirectory(t, "alpha", "beta", "gamma")
	plan := textFixturePlan(t, input)
	plan.Writer.RowGroupLogicalBytes = 5
	plan.Writer.CompressedTarget = 1
	plan.Writer.CompressedMaximum = 1 << 20
	if err := os.WriteFile(plan.Inputs[len(plan.Inputs)-1].Artifact.Path, []byte("changed after planning"), 0o644); err != nil {
		t.Fatal(err)
	}
	publisher := &fakePublisher{}
	if _, _, err := ExecutePublication(context.Background(), plan, t.TempDir(), publisher, 2); err == nil {
		t.Fatal("expected changed input to fail assembly")
	}
	if publisher.publishTry != 0 || len(publisher.objects) != 0 {
		t.Fatalf("failed assembly attempted %d uploads and published %d objects", publisher.publishTry, len(publisher.objects))
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

func TestObjectVerificationPipelineUsesWorkersAndPreservesOrder(t *testing.T) {
	directory := t.TempDir()
	started := make(chan int, 2)
	release := make(chan struct{})
	var mu sync.Mutex
	active, maximum := 0, 0
	verify := func(_ string, object ObjectResult) (ObjectResult, error) {
		mu.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		mu.Unlock()
		started <- int(object.Docs)
		<-release
		mu.Lock()
		active--
		mu.Unlock()
		return object, nil
	}
	var delivered []int64
	pipeline := newObjectVerificationPipeline(context.Background(), directory, 2, func(object ObjectResult) error {
		delivered = append(delivered, object.Docs)
		return nil
	}, verify)
	for sequence := 1; sequence <= 2; sequence++ {
		object := ObjectResult{Path: filepath.Join(directory, fmt.Sprintf("input-%d", sequence)), SHA256: fmt.Sprintf("%064d", sequence), Docs: int64(sequence)}
		if err := pipeline.enqueue(object); err != nil {
			t.Fatal(err)
		}
	}
	first, second := <-started, <-started
	if first == second {
		t.Fatalf("workers started duplicate jobs %d and %d", first, second)
	}
	mu.Lock()
	gotMaximum := maximum
	mu.Unlock()
	if gotMaximum != 2 {
		t.Fatalf("maximum concurrent audits = %d, want 2", gotMaximum)
	}
	close(release)
	objects, err := pipeline.closeAndWait()
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 2 || len(delivered) != 2 || delivered[0] != 1 || delivered[1] != 2 {
		t.Fatalf("ordered audit results = %+v, delivered = %v", objects, delivered)
	}
}

func TestObjectVerificationPipelineRefusesFailedShard(t *testing.T) {
	directory := t.TempDir()
	var delivered []int64
	pipeline := newObjectVerificationPipeline(context.Background(), directory, 2, func(object ObjectResult) error {
		delivered = append(delivered, object.Docs)
		return nil
	}, func(_ string, object ObjectResult) (ObjectResult, error) {
		if object.Docs == 2 {
			return ObjectResult{}, fmt.Errorf("injected audit failure")
		}
		return object, nil
	})
	for sequence := 1; sequence <= 2; sequence++ {
		object := ObjectResult{Path: filepath.Join(directory, fmt.Sprintf("input-%d", sequence)), SHA256: fmt.Sprintf("%064d", sequence), Docs: int64(sequence)}
		if err := pipeline.enqueue(object); err != nil {
			t.Fatal(err)
		}
	}
	objects, err := pipeline.closeAndWait()
	if err == nil || !strings.Contains(err.Error(), "injected audit failure") {
		t.Fatalf("audit failure = %v", err)
	}
	if len(objects) != 1 || len(delivered) != 1 || delivered[0] != 1 {
		t.Fatalf("failed audit published results = %+v, delivered = %v", objects, delivered)
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
