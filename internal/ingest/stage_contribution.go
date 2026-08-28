// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/openwaldo/waldo/internal/index"
)

type ContributionResult struct {
	Root      string   `json:"root"`
	Files     []string `json:"files"`
	Removed   []string `json:"removed,omitempty"`
	Applied   bool     `json:"applied,omitempty"`
	IndexRoot string   `json:"index_root,omitempty"`
}

type contributionWrite struct {
	relative string
	target   string
	data     []byte
	original []byte
	existed  bool
	remove   bool
}

// ApplyContribution atomically replaces each individual index file, validates
// the complete checkout, and restores the original files if any step fails.
// The staged overlay is retained as a durable record of what was applied.
func ApplyContribution(indexRoot string, contribution ContributionResult) (ContributionResult, error) {
	root, err := filepath.Abs(indexRoot)
	if err != nil {
		return contribution, err
	}
	overlay, err := filepath.Abs(contribution.Root)
	if err != nil {
		return contribution, err
	}
	if pathWithin(root, overlay) {
		return contribution, fmt.Errorf("contribution overlay must be outside the index checkout")
	}
	writes := make([]contributionWrite, 0, len(contribution.Files))
	touched := map[string]bool{}
	for _, relative := range contribution.Files {
		target, err := safeContributionPath(root, relative)
		if err != nil {
			return contribution, err
		}
		source, err := safeContributionPath(overlay, relative)
		if err != nil {
			return contribution, err
		}
		info, err := os.Lstat(source)
		if err != nil {
			return contribution, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return contribution, fmt.Errorf("contribution file %s is not a regular file", relative)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return contribution, err
		}
		write := contributionWrite{relative: relative, target: target, data: data}
		if current, exists, readErr := readOptionalRegular(target); readErr != nil {
			return contribution, fmt.Errorf("read index target %s: %w", relative, readErr)
		} else if exists {
			write.original, write.existed = current, true
		}
		writes = append(writes, write)
		touched[relative] = true
	}
	for _, relative := range contribution.Removed {
		if touched[relative] {
			return contribution, fmt.Errorf("contribution both writes and removes %s", relative)
		}
		target, err := safeContributionPath(root, relative)
		if err != nil {
			return contribution, err
		}
		write := contributionWrite{relative: relative, target: target, remove: true}
		if current, exists, readErr := readOptionalRegular(target); readErr != nil {
			return contribution, fmt.Errorf("read removed index target %s: %w", relative, readErr)
		} else if exists {
			write.original, write.existed = current, true
		}
		writes = append(writes, write)
		touched[relative] = true
	}
	rollback := func(applyErr error) error {
		var rollbackErr error
		for _, write := range writes {
			if write.existed {
				rollbackErr = errors.Join(rollbackErr, writeIndexFileAtomic(write.target, write.original))
			} else if err := os.Remove(write.target); err != nil && !os.IsNotExist(err) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		if rollbackErr != nil {
			return errors.Join(applyErr, fmt.Errorf("rollback contribution: %w", rollbackErr))
		}
		return applyErr
	}
	for _, write := range writes {
		if write.remove {
			if err := os.Remove(write.target); err != nil && !os.IsNotExist(err) {
				return contribution, rollback(err)
			}
			continue
		}
		if err := writeIndexFileAtomic(write.target, write.data); err != nil {
			return contribution, rollback(err)
		}
	}
	target, err := index.Resolve(root, "")
	if err == nil {
		err = index.WalkCorpora(target, func(index.Corpus) error { return nil })
	}
	if err != nil {
		return contribution, rollback(fmt.Errorf("validate applied index: %w", err))
	}
	contribution.Applied, contribution.IndexRoot = true, root
	return contribution, nil
}

func safeContributionPath(root, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if relative == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid contribution path %q", relative)
	}
	target := filepath.Join(root, clean)
	if !pathWithin(root, target) {
		return "", fmt.Errorf("contribution path %q escapes its root", relative)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("contribution root %s is not a non-symlink directory", root)
	}
	current := root
	parts := strings.Split(filepath.Dir(clean), string(filepath.Separator))
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("contribution path %q has a non-directory or symlink parent", relative)
		}
	}
	return target, nil
}

func readOptionalRegular(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("not a regular non-symlink file")
	}
	data, err := os.ReadFile(path)
	return data, err == nil, err
}

func writeIndexFileAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".waldo-index-apply-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncDirectory(directory)
}

// StageContribution writes a minimal overlay containing the new manifest,
// leaf index, and every changed ancestor index.yaml. Existing JSON/YML
// navigation files superseded by those writes are returned in Removed. It does
// not mutate the Git checkout; the overlay is intended for review and an
// explicit apply-and-remove step.
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
	manifestRelative := filepath.ToSlash(filepath.Join(filepath.FromSlash(plan.Destination), name+index.YAMLExtension))
	if err := writeContributionYAML(temporary, manifestRelative, manifest); err != nil {
		return ContributionResult{}, err
	}
	result.Files = append(result.Files, manifestRelative)
	leaf := index.Directory{
		Kind: "index", Schema: index.DirectorySchema, Path: plan.Destination,
		Entries: []index.Entry{{Name: name + index.YAMLExtension, Type: "manifest"}},
	}
	leafRelative := filepath.ToSlash(filepath.Join(filepath.FromSlash(plan.Destination), "index.yaml"))
	if err := writeContributionYAML(temporary, leafRelative, leaf); err != nil {
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
			existingPath, err := index.DirectoryPath(parentPath)
			if err != nil {
				return ContributionResult{}, err
			}
			if filepath.Base(existingPath) != "index.yaml" {
				existingRelative, err := filepath.Rel(root, existingPath)
				if err != nil {
					return ContributionResult{}, err
				}
				result.Removed = append(result.Removed, filepath.ToSlash(existingRelative))
			}
			for _, entry := range directory.Entries {
				if entry.Name == child {
					return ContributionResult{}, fmt.Errorf("index directory %q already contains %q", parent, child)
				}
			}
			directory.Entries = append(directory.Entries, index.Entry{Name: child, Type: "dir"})
			directory.Entries = index.SortedEntries(directory.Entries)
			relative := filepath.ToSlash(filepath.Join(filepath.FromSlash(parent), "index.yaml"))
			if err := writeContributionYAML(temporary, relative, directory); err != nil {
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
		relative := filepath.ToSlash(filepath.Join(filepath.FromSlash(parent), "index.yaml"))
		if err := writeContributionYAML(temporary, relative, directory); err != nil {
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
	slices.Sort(result.Removed)
	result.Removed = slices.Compact(result.Removed)
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

// CheckContributionDestinationPath rejects an occupied index destination.
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

func writeContributionYAML(root, relative string, value any) error {
	if relative == "" || filepath.IsAbs(relative) || strings.HasPrefix(filepath.Clean(relative), "..") {
		return fmt.Errorf("invalid contribution path %q", relative)
	}
	data, err := index.MarshalYAML(value)
	if err != nil {
		return err
	}
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
