package ingest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/openwaldo/waldo-new/internal/index"
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
	if _, err := index.LoadDirectory(root); err != nil {
		return ContributionResult{}, fmt.Errorf("load index root: %w", err)
	}
	destination := filepath.Join(root, filepath.FromSlash(plan.Destination))
	if _, err := os.Stat(destination); err == nil {
		return ContributionResult{}, fmt.Errorf("index destination %s already exists", plan.Destination)
	} else if !os.IsNotExist(err) {
		return ContributionResult{}, err
	}
	stagingRoot, err := filepath.Abs(stagingDirectory)
	if err != nil {
		return ContributionResult{}, err
	}
	finalRoot := filepath.Join(stagingRoot, "contribution")
	if _, err := os.Stat(finalRoot); err == nil {
		return ContributionResult{}, fmt.Errorf("staged contribution already exists at %s", finalRoot)
	} else if !os.IsNotExist(err) {
		return ContributionResult{}, err
	}
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
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
		Kind: "index", Schema: 2, Path: plan.Destination,
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
			Kind: "index", Schema: 2, Path: parent,
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
	if err := os.Rename(temporary, finalRoot); err != nil {
		return ContributionResult{}, err
	}
	if err := syncDirectory(stagingRoot); err != nil {
		return ContributionResult{}, err
	}
	committed = true
	return result, nil
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
