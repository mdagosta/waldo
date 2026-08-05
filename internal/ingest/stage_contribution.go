package ingest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/openwaldo/waldo/internal/index"
)

type ContributionResult struct {
	Root  string   `json:"root"`
	Files []string `json:"files"`
}

// StageContribution writes a minimal overlay containing the new manifest,
// leaf index, and every changed ancestor index.json. It does not mutate the Git
// checkout; the overlay is intended for review and an explicit apply step.
func StageContribution(indexRoot, stagingDirectory string, plan Plan, manifest index.Manifest) (ContributionResult, error) {
	root, err := filepath.Abs(indexRoot)
	if err != nil {
		return ContributionResult{}, err
	}
	if err := CheckContributionDestination(root, plan); err != nil {
		return ContributionResult{}, err
	}
	stagingRoot, err := filepath.Abs(stagingDirectory)
	if err != nil {
		return ContributionResult{}, err
	}
	if pathWithin(root, stagingRoot) {
		return ContributionResult{}, fmt.Errorf("staging directory must be outside the index checkout")
	}
	finalRoot := filepath.Join(stagingRoot, "contribution")
	_, finalErr := os.Stat(finalRoot)
	if finalErr != nil && !os.IsNotExist(finalErr) {
		return ContributionResult{}, finalErr
	}
	finalExists := finalErr == nil
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		return ContributionResult{}, err
	}
	temporary, err := os.MkdirTemp(stagingRoot, ".waldo-contribution-*")
	if err != nil {
		return ContributionResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(temporary)
		}
	}()
	result := ContributionResult{Root: finalRoot}
	name := manifest.Name
	manifestRelative := filepath.ToSlash(filepath.Join(filepath.FromSlash(plan.Destination), name+".json"))
	if err := writeContributionJSON(temporary, manifestRelative, manifest); err != nil {
		return ContributionResult{}, err
	}
	result.Files = append(result.Files, manifestRelative)
	leaf := index.Directory{
		Kind: "index", Schema: index.DirectorySchema, Path: plan.Destination,
		Entries: []index.Entry{{Name: name + ".json", Type: "manifest"}},
	}
	leafRelative := filepath.ToSlash(filepath.Join(filepath.FromSlash(plan.Destination), "index.json"))
	if err := writeContributionJSON(temporary, leafRelative, leaf); err != nil {
		return ContributionResult{}, err
	}
	result.Files = append(result.Files, leafRelative)

	child := name
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(plan.Destination)))
	if parent == "." {
		parent = ""
	}
	for {
		parentPath := filepath.Join(root, filepath.FromSlash(parent))
		directory, loadErr := index.LoadDirectory(parentPath)
		if loadErr == nil {
			for _, entry := range directory.Entries {
				if entry.Name == child {
					return ContributionResult{}, fmt.Errorf("index directory %q already contains %q", parent, child)
				}
			}
			directory.Entries = append(directory.Entries, index.Entry{Name: child, Type: "dir"})
			directory.Entries = index.SortedEntries(directory.Entries)
			relative := filepath.ToSlash(filepath.Join(filepath.FromSlash(parent), "index.json"))
			if err := writeContributionJSON(temporary, relative, directory); err != nil {
				return ContributionResult{}, err
			}
			result.Files = append(result.Files, relative)
			break
		}
		if !os.IsNotExist(loadErr) {
			return ContributionResult{}, fmt.Errorf("load ancestor index %q: %w", parent, loadErr)
		}
		directory = index.Directory{
			Kind: "index", Schema: index.DirectorySchema, Path: parent,
			Entries: []index.Entry{{Name: child, Type: "dir"}},
		}
		relative := filepath.ToSlash(filepath.Join(filepath.FromSlash(parent), "index.json"))
		if err := writeContributionJSON(temporary, relative, directory); err != nil {
			return ContributionResult{}, err
		}
		result.Files = append(result.Files, relative)
		child = filepath.Base(filepath.FromSlash(parent))
		parent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(parent)))
		if parent == "." {
			parent = ""
		}
	}
	slices.Sort(result.Files)
	result.Files = slices.Compact(result.Files)
	if err := syncContributionTree(temporary); err != nil {
		return ContributionResult{}, err
	}
	if finalExists {
		if err := compareContributionTrees(finalRoot, temporary, result.Files); err != nil {
			return ContributionResult{}, fmt.Errorf("existing staged contribution differs: %w", err)
		}
		result.Root = finalRoot
		return result, nil
	}
	if err := os.Rename(temporary, finalRoot); err != nil {
		return ContributionResult{}, err
	}
	if err := syncDirectory(stagingRoot); err != nil {
		return ContributionResult{}, err
	}
	committed = true
	return result, nil
}

// ValidateWorkLocations keeps machine-local state out of the Git checkout and
// prevents staging cleanup from ever sharing a tree with lookaside objects.
func ValidateWorkLocations(indexRoot, stagingDirectory, scratchRoot string) error {
	root, err := filepath.Abs(indexRoot)
	if err != nil {
		return err
	}
	staging, err := filepath.Abs(stagingDirectory)
	if err != nil {
		return err
	}
	scratch, err := filepath.Abs(scratchRoot)
	if err != nil {
		return err
	}
	if pathWithin(root, staging) || pathWithin(root, scratch) {
		return fmt.Errorf("staging and lookaside scratch must be outside the index checkout")
	}
	if pathWithin(staging, scratch) || pathWithin(scratch, staging) {
		return fmt.Errorf("staging and lookaside scratch must not overlap")
	}
	return nil
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// CheckContributionDestination performs the checkout collision check before
// expensive conversion or lookaside admission begins.
func CheckContributionDestination(indexRoot string, plan Plan) error {
	return CheckContributionDestinationPath(indexRoot, plan.Destination)
}

// CheckContributionDestinationPath allows recipe-driven ingestion to reject an
// occupied destination before it executes potentially expensive fetchers.
func CheckContributionDestinationPath(indexRoot, destinationPath string) error {
	root, err := filepath.Abs(indexRoot)
	if err != nil {
		return err
	}
	if _, err := index.LoadDirectory(root); err != nil {
		return fmt.Errorf("load index root: %w", err)
	}
	destination := filepath.Join(root, filepath.FromSlash(destinationPath))
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("index destination %s already exists", destinationPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func compareContributionTrees(left, right string, expected []string) error {
	actual := []string{}
	err := filepath.WalkDir(left, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(left, path)
		if err != nil {
			return err
		}
		actual = append(actual, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return err
	}
	slices.Sort(actual)
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("file set is %v, want %v", actual, expected)
	}
	for _, relative := range expected {
		leftData, err := os.ReadFile(filepath.Join(left, filepath.FromSlash(relative)))
		if err != nil {
			return err
		}
		rightData, err := os.ReadFile(filepath.Join(right, filepath.FromSlash(relative)))
		if err != nil {
			return err
		}
		if !slices.Equal(leftData, rightData) {
			return fmt.Errorf("%s differs", relative)
		}
	}
	return nil
}

func writeContributionJSON(root, relative string, value any) error {
	if relative == "" || filepath.IsAbs(relative) || strings.HasPrefix(filepath.Clean(relative), "..") {
		return fmt.Errorf("invalid contribution path %q", relative)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	destination := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o644)
}

func syncContributionTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	})
}
