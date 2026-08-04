package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openwaldo/waldo-new/internal/lookaside"
)

const journalFile = "INGESTION.json"

type Journal struct {
	Kind         string           `json:"kind"`
	Schema       int              `json:"schema"`
	PlanIdentity string           `json:"plan_identity"`
	Status       string           `json:"status"`
	Assembly     *AssemblyResult  `json:"assembly,omitempty"`
	Admission    *AdmissionResult `json:"admission,omitempty"`
}

type AdmissionResult struct {
	CacheRoot string            `json:"cache_root"`
	Objects   []AdmissionObject `json:"objects"`
}

type AdmissionObject struct {
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Path   string `json:"path"`
}

// ExecuteAssembly wraps object generation in an atomic recovery journal. An
// assembled journal is verified and reused; changed plans or corrupt staged
// objects are refused rather than combined with earlier state.
func ExecuteAssembly(ctx context.Context, plan Plan, stagingDirectory string) (AssemblyResult, error) {
	identity, err := plan.Identity()
	if err != nil {
		return AssemblyResult{}, err
	}
	if stagingDirectory == "" {
		return AssemblyResult{}, fmt.Errorf("staging directory is required")
	}
	abs, err := filepath.Abs(stagingDirectory)
	if err != nil {
		return AssemblyResult{}, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
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
		if journal.Status == "assembled" || journal.Status == "admitted" {
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
	journal.Admission = nil
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

// ExecuteAdmission assembles or resumes the plan, then atomically admits every
// verified Parquet object to one pinned local lookaside cache.
func ExecuteAdmission(ctx context.Context, plan Plan, stagingDirectory string, cache *lookaside.Cache) (AssemblyResult, AdmissionResult, error) {
	if cache == nil {
		return AssemblyResult{}, AdmissionResult{}, fmt.Errorf("lookaside cache is required")
	}
	assembly, err := ExecuteAssembly(ctx, plan, stagingDirectory)
	if err != nil {
		return AssemblyResult{}, AdmissionResult{}, err
	}
	abs, err := filepath.Abs(stagingDirectory)
	if err != nil {
		return AssemblyResult{}, AdmissionResult{}, err
	}
	journalPath := filepath.Join(abs, journalFile)
	journal, exists, err := loadJournal(journalPath)
	if err != nil {
		return AssemblyResult{}, AdmissionResult{}, fmt.Errorf("load admission journal: %w", err)
	}
	if !exists {
		return AssemblyResult{}, AdmissionResult{}, fmt.Errorf("admission journal disappeared after assembly")
	}
	if journal.Status == "admitted" {
		if journal.Admission == nil {
			return AssemblyResult{}, AdmissionResult{}, fmt.Errorf("admitted journal has no admission result")
		}
		if err := verifyAdmission(cache, assembly, *journal.Admission); err != nil {
			return AssemblyResult{}, AdmissionResult{}, err
		}
		return assembly, *journal.Admission, nil
	}
	if journal.Status != "assembled" {
		return AssemblyResult{}, AdmissionResult{}, fmt.Errorf("cannot admit journal in status %q", journal.Status)
	}
	admission := AdmissionResult{CacheRoot: cache.Root()}
	for _, object := range assembly.Objects {
		path, err := cache.Admit(ctx, object.Path, object.SHA256, object.Bytes)
		if err != nil {
			return AssemblyResult{}, AdmissionResult{}, err
		}
		admission.Objects = append(admission.Objects, AdmissionObject{SHA256: object.SHA256, Bytes: object.Bytes, Path: path})
	}
	journal.Status = "admitted"
	journal.Admission = &admission
	if err := writeJournal(journalPath, journal); err != nil {
		return AssemblyResult{}, AdmissionResult{}, err
	}
	return assembly, admission, nil
}

func verifyAdmission(cache *lookaside.Cache, assembly AssemblyResult, admission AdmissionResult) error {
	if admission.CacheRoot != cache.Root() || len(admission.Objects) != len(assembly.Objects) {
		return fmt.Errorf("admission journal belongs to a different lookaside cache or object set")
	}
	for index, object := range admission.Objects {
		assembled := assembly.Objects[index]
		expectedPath, err := cache.Path(object.SHA256)
		if err != nil {
			return err
		}
		if object.SHA256 != assembled.SHA256 || object.Bytes != assembled.Bytes || filepath.Clean(object.Path) != expectedPath {
			return fmt.Errorf("admission object %d does not match its assembled object", index+1)
		}
		if err := lookaside.VerifyFile(object.Path, object.SHA256, object.Bytes); err != nil {
			return fmt.Errorf("verify admitted object %s: %w", object.SHA256, err)
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
	if result.InputDocs <= 0 || result.RetainedDocs <= 0 || result.DuplicateDocs != result.InputDocs-result.RetainedDocs || len(result.Objects) == 0 {
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
