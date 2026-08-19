// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/openwaldo/waldo/internal/lookaside"
)

const journalFile = "INGESTION.json"

type Journal struct {
	Kind         string             `json:"kind"`
	Schema       int                `json:"schema"`
	PlanIdentity string             `json:"plan_identity"`
	Status       string             `json:"status"`
	Assembly     *AssemblyResult    `json:"assembly,omitempty"`
	Publication  *PublicationResult `json:"publication,omitempty"`
}

type PublicationResult struct {
	BaseURL string              `json:"base_url"`
	Workers int                 `json:"workers"`
	Objects []PublicationObject `json:"objects"`
}

type PublicationObject struct {
	Sequence int    `json:"sequence"`
	SHA256   string `json:"sha256"`
	Bytes    int64  `json:"bytes"`
	URL      string `json:"url"`
}

// ExecuteAssembly wraps object generation in an atomic recovery journal. An
// assembled journal is verified and reused; changed plans or corrupt staged
// objects are refused rather than combined with earlier state.
func ExecuteAssembly(ctx context.Context, plan Plan, stagingDirectory string) (AssemblyResult, error) {
	identity, err := plan.Identity()
	if err != nil {
		return AssemblyResult{}, err
	}
	if plan.Mode != "streaming" {
		return AssemblyResult{}, fmt.Errorf("canonical ingestion execution requires the external-sort stage, which is not enabled yet")
	}
	if stagingDirectory == "" {
		return AssemblyResult{}, fmt.Errorf("staging directory is required")
	}
	abs, err := filepath.Abs(stagingDirectory)
	if err != nil {
		return AssemblyResult{}, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return AssemblyResult{}, err
	}
	journalPath := filepath.Join(abs, journalFile)
	journal, exists, err := loadJournal(journalPath)
	if err != nil {
		return AssemblyResult{}, err
	}
	if exists {
		if journal.PlanIdentity != identity {
			return AssemblyResult{}, fmt.Errorf("staging journal belongs to ingestion plan %s, not %s", journal.PlanIdentity, identity)
		}
		if journal.Status == "assembled" {
			if journal.Assembly == nil {
				return AssemblyResult{}, fmt.Errorf("assembled journal has no assembly result")
			}
			if err := verifyJournalAssembly(abs, *journal.Assembly); err != nil {
				return AssemblyResult{}, fmt.Errorf("verify assembled journal: %w", err)
			}
			return *journal.Assembly, nil
		}
		if journal.Status != "assembling" {
			return AssemblyResult{}, fmt.Errorf("unsupported ingestion journal status %q", journal.Status)
		}
	} else {
		journal = Journal{Kind: "waldo-ingest-journal", Schema: 1, PlanIdentity: identity}
	}
	journal.Status = "assembling"
	journal.Assembly = nil
	if err := writeJournal(journalPath, journal); err != nil {
		return AssemblyResult{}, err
	}
	if err := cleanupIncompleteObjects(filepath.Join(abs, "objects")); err != nil {
		return AssemblyResult{}, err
	}
	result, err := AssembleTextObjects(ctx, plan, abs)
	if err != nil {
		return AssemblyResult{}, err
	}
	journal.Status = "assembled"
	journal.Assembly = &result
	if err := writeJournal(journalPath, journal); err != nil {
		return AssemblyResult{}, err
	}
	return result, nil
}

// ExecutePublication completes and audits the entire local assembly before it
// starts bounded-concurrency publication. Each verified remote object is
// journaled before its staging file is removed, so publication interruption is
// recoverable without trusting either side implicitly.
func ExecutePublication(ctx context.Context, plan Plan, stagingDirectory string, publisher lookaside.Publisher, workers int) (AssemblyResult, PublicationResult, error) {
	if publisher == nil {
		return AssemblyResult{}, PublicationResult{}, fmt.Errorf("lookaside publisher is required")
	}
	if workers < 1 || workers > 32 {
		return AssemblyResult{}, PublicationResult{}, fmt.Errorf("publication workers must be in 1..32")
	}
	identity, err := plan.Identity()
	if err != nil {
		return AssemblyResult{}, PublicationResult{}, err
	}
	abs, err := filepath.Abs(stagingDirectory)
	if err != nil {
		return AssemblyResult{}, PublicationResult{}, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return AssemblyResult{}, PublicationResult{}, err
	}
	journalPath := filepath.Join(abs, journalFile)
	journal, exists, err := loadJournal(journalPath)
	if err != nil {
		return AssemblyResult{}, PublicationResult{}, err
	}
	if exists && journal.PlanIdentity != identity {
		return AssemblyResult{}, PublicationResult{}, fmt.Errorf("staging journal belongs to ingestion plan %s, not %s", journal.PlanIdentity, identity)
	}
	if exists && journal.Status == "published" {
		if journal.Assembly == nil || journal.Publication == nil {
			return AssemblyResult{}, PublicationResult{}, fmt.Errorf("published journal is incomplete")
		}
		if err := verifyPublished(ctx, publisher, *journal.Assembly, *journal.Publication); err != nil {
			return AssemblyResult{}, PublicationResult{}, err
		}
		return *journal.Assembly, *journal.Publication, nil
	}
	if exists && journal.Status != "publishing" && journal.Status != "assembling" && journal.Status != "assembled" {
		return AssemblyResult{}, PublicationResult{}, fmt.Errorf("cannot publish journal in status %q", journal.Status)
	}
	if !exists {
		journal = Journal{Kind: "waldo-ingest-journal", Schema: 1, PlanIdentity: identity}
	}
	// Journals written by the former streaming publisher entered publishing
	// before assembly completed. Restart those locally; already uploaded
	// content-addressed objects remain harmless and may be reused by Publish.
	if journal.Status == "publishing" && journal.Assembly == nil {
		journal.Status, journal.Publication = "assembling", nil
		if err := writeJournal(journalPath, journal); err != nil {
			return AssemblyResult{}, PublicationResult{}, err
		}
	}
	var assembly AssemblyResult
	if journal.Status == "publishing" {
		assembly = *journal.Assembly
	} else {
		assembly, err = ExecuteAssembly(ctx, plan, abs)
		if err != nil {
			return AssemblyResult{}, PublicationResult{}, err
		}
		journal, _, err = loadJournal(journalPath)
		if err != nil {
			return AssemblyResult{}, PublicationResult{}, err
		}
	}
	publication := PublicationResult{BaseURL: publisher.BaseURL(), Workers: workers}
	if journal.Publication != nil && journal.Publication.BaseURL == publication.BaseURL {
		publication.Objects = append(publication.Objects, journal.Publication.Objects...)
	}
	publication.Objects = publicationObjectsForAssembly(publication.Objects, assembly.Objects)
	for _, object := range publication.Objects {
		if _, err := publisher.Verify(ctx, object.SHA256, object.Bytes); err != nil {
			return AssemblyResult{}, PublicationResult{}, fmt.Errorf("verify previously published object %s: %w", object.SHA256, err)
		}
	}
	journal.Status, journal.Assembly, journal.Publication = "publishing", &assembly, &publication
	if err := writeJournal(journalPath, journal); err != nil {
		return AssemblyResult{}, PublicationResult{}, err
	}

	type job struct {
		sequence int
		object   ObjectResult
	}
	type outcome struct {
		job       job
		published lookaside.PublishedObject
		err       error
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan job, workers)
	outcomes := make(chan outcome, workers)
	var workerGroup sync.WaitGroup
	for worker := 1; worker <= workers; worker++ {
		workerGroup.Add(1)
		go func(worker int) {
			defer workerGroup.Done()
			for item := range jobs {
				emitProgress(ctx, ProgressEvent{Phase: "upload", Status: "started", Shard: item.object.SHA256, Sequence: item.sequence, Worker: worker, TotalBytes: item.object.Bytes})
				published, err := publisher.Publish(runCtx, item.object.Path, item.object.SHA256, item.object.Bytes, func(progress lookaside.PublishProgress) {
					emitProgress(ctx, ProgressEvent{Phase: "upload", Status: "progress", Shard: item.object.SHA256, Sequence: item.sequence, Worker: worker, Bytes: progress.Written, TotalBytes: progress.Total})
				})
				outcomes <- outcome{job: item, published: published, err: err}
			}
		}(worker)
	}
	collectorDone := make(chan error, 1)
	go func() {
		var firstErr error
		for result := range outcomes {
			if result.err != nil {
				if firstErr == nil {
					firstErr = result.err
					cancel()
				}
				continue
			}
			entry := PublicationObject{Sequence: result.job.sequence, SHA256: result.published.SHA256, Bytes: result.published.Bytes, URL: result.published.URL}
			publication.Objects = upsertPublicationObject(publication.Objects, entry)
			journal.Publication = &publication
			if err := writeJournal(journalPath, journal); err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				continue
			}
			emitProgress(ctx, ProgressEvent{Phase: "upload", Status: "verified", Shard: entry.SHA256, Remote: entry.URL, Sequence: entry.Sequence, Bytes: entry.Bytes, TotalBytes: entry.Bytes})
			if err := os.Remove(result.job.object.Path); err != nil && !os.IsNotExist(err) {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				continue
			}
			if err := syncDirectory(filepath.Dir(result.job.object.Path)); err != nil && firstErr == nil {
				firstErr = err
				cancel()
				continue
			}
			emitProgress(ctx, ProgressEvent{Phase: "staging", Status: "purged", Shard: entry.SHA256, Sequence: entry.Sequence, ReclaimedBytes: entry.Bytes})
		}
		collectorDone <- firstErr
	}()

	published := make(map[string]bool, len(publication.Objects))
	for _, object := range publication.Objects {
		published[object.SHA256] = true
	}
queueObjects:
	for position, object := range assembly.Objects {
		if published[object.SHA256] {
			continue
		}
		sequence := position + 1
		emitProgress(ctx, ProgressEvent{Phase: "upload", Status: "queued", Shard: object.SHA256, Sequence: sequence, TotalBytes: object.Bytes})
		select {
		case jobs <- job{sequence: sequence, object: object}:
		case <-runCtx.Done():
			break queueObjects
		}
	}
	close(jobs)
	workerGroup.Wait()
	close(outcomes)
	collectorErr := <-collectorDone
	if collectorErr != nil {
		return AssemblyResult{}, PublicationResult{}, collectorErr
	}
	if err := ctx.Err(); err != nil {
		return AssemblyResult{}, PublicationResult{}, err
	}
	publication.Objects = publicationObjectsForAssembly(publication.Objects, assembly.Objects)
	slices.SortFunc(publication.Objects, func(a, b PublicationObject) int { return a.Sequence - b.Sequence })
	if len(publication.Objects) != len(assembly.Objects) {
		return AssemblyResult{}, PublicationResult{}, fmt.Errorf("published %d objects for %d assembled objects", len(publication.Objects), len(assembly.Objects))
	}
	journal.Status, journal.Assembly, journal.Publication = "published", &assembly, &publication
	if err := writeJournal(journalPath, journal); err != nil {
		return AssemblyResult{}, PublicationResult{}, err
	}
	return assembly, publication, nil
}

func publicationObjectsForAssembly(published []PublicationObject, assembled []ObjectResult) []PublicationObject {
	wanted := make(map[string]struct{}, len(assembled))
	for _, object := range assembled {
		wanted[object.SHA256] = struct{}{}
	}
	filtered := published[:0]
	for _, object := range published {
		if _, ok := wanted[object.SHA256]; ok {
			filtered = append(filtered, object)
		}
	}
	return filtered
}

func upsertPublicationObject(objects []PublicationObject, entry PublicationObject) []PublicationObject {
	for index := range objects {
		if objects[index].SHA256 == entry.SHA256 {
			objects[index] = entry
			return objects
		}
	}
	return append(objects, entry)
}

func verifyPublished(ctx context.Context, publisher lookaside.Publisher, assembly AssemblyResult, publication PublicationResult) error {
	if publication.BaseURL != publisher.BaseURL() || len(publication.Objects) != len(assembly.Objects) {
		return fmt.Errorf("publication journal belongs to a different publisher or object set")
	}
	byDigest := make(map[string]PublicationObject, len(publication.Objects))
	for _, object := range publication.Objects {
		byDigest[object.SHA256] = object
	}
	for _, object := range assembly.Objects {
		published, ok := byDigest[object.SHA256]
		if !ok || published.Bytes != object.Bytes {
			return fmt.Errorf("publication journal is missing object %s", object.SHA256)
		}
		if _, err := publisher.Verify(ctx, object.SHA256, object.Bytes); err != nil {
			return fmt.Errorf("verify published object %s: %w", object.SHA256, err)
		}
	}
	return nil
}

func loadJournal(path string) (Journal, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Journal{}, false, nil
	}
	if err != nil {
		return Journal{}, false, err
	}
	var journal Journal
	if err := json.Unmarshal(data, &journal); err != nil {
		return Journal{}, false, fmt.Errorf("%s: %w", path, err)
	}
	if journal.Kind != "waldo-ingest-journal" || journal.Schema != 1 || journal.PlanIdentity == "" {
		return Journal{}, false, fmt.Errorf("%s: unsupported ingestion journal", path)
	}
	return journal, true, nil
}

func writeJournal(path string, journal Journal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".waldo-journal-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	committed = true
	return nil
}

func verifyJournalAssembly(stagingDirectory string, result AssemblyResult) error {
	if result.InputDocs <= 0 || result.RetainedDocs <= 0 || result.RejectedDocs < 0 || result.DuplicateDocs != result.InputDocs-result.RejectedDocs-result.RetainedDocs || len(result.Objects) == 0 {
		return fmt.Errorf("journal assembly totals are inconsistent")
	}
	objectDirectory := filepath.Join(stagingDirectory, "objects")
	var docs int64
	for _, object := range result.Objects {
		clean := filepath.Clean(object.Path)
		relative, err := filepath.Rel(objectDirectory, clean)
		if err != nil || relative != object.SHA256 || strings.Contains(relative, string(filepath.Separator)) {
			return fmt.Errorf("journal object path %q is outside its content-addressed staging location", object.Path)
		}
		if _, err := verifyAssembledObject(object); err != nil {
			return err
		}
		docs += object.Docs
	}
	if docs != result.RetainedDocs {
		return fmt.Errorf("journal objects contain %d documents, want %d", docs, result.RetainedDocs)
	}
	return nil
}

func cleanupIncompleteObjects(directory string) error {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), ".waldo-shard-") {
			if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
				return err
			}
		}
	}
	return syncDirectory(directory)
}
