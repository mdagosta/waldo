package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/openwaldo/waldo/internal/index"
)

func BuildUpdatedManifest(plan Plan, existing index.Manifest, assembly AssemblyResult, objectBase, manifestPath string) (index.Manifest, error) {
	if plan.Update == nil {
		return index.Manifest{}, fmt.Errorf("update plan is required")
	}
	if plan.Update.Mode == "append" && len(assembly.Objects) == 0 {
		return existing, nil
	}
	fresh, err := BuildManifest(plan, assembly, objectBase)
	if err != nil {
		return index.Manifest{}, err
	}
	fresh.Name = existing.Name
	if plan.Update.Mode == "rebuild-shards" {
		fresh.Sources = preserveSourceContext(existing.Sources, fresh.Sources)
		if fresh.Processing == nil {
			fresh.Processing = existing.Processing
		}
		if err := index.ValidateManifest(manifestPath, fresh); err != nil {
			return index.Manifest{}, err
		}
		return fresh, nil
	}
	if existing.Rollup != nil {
		return index.Manifest{}, fmt.Errorf("append update does not support rollup-backed manifest %s; use --rebuild-shards", manifestPath)
	}
	if existing.RecordSchema != 0 && existing.RecordSchema != fresh.RecordSchema {
		return index.Manifest{}, fmt.Errorf("existing record schema %d cannot append schema %d", existing.RecordSchema, fresh.RecordSchema)
	}
	updated := existing
	if updated.Format == "" {
		updated.Format = fresh.Format
	}
	if updated.RecordSchema == 0 {
		updated.RecordSchema = fresh.RecordSchema
	}
	newSource := fresh.Sources[0]
	newSource.Name = uniqueSourceName(updated.Sources, newSource)
	newSource.Source = fresh.Sources[0].Source
	knownSource := false
	for _, candidate := range updated.Sources {
		if candidate.Name == newSource.Name && candidate.SHA256 == newSource.SHA256 {
			knownSource = true
			break
		}
	}
	if !knownSource {
		updated.Sources = append(updated.Sources, newSource)
	}
	for _, shard := range fresh.Shards {
		shard.Sources = []string{newSource.Name}
		if updated.License != plan.License {
			shard.License = plan.License
		}
		if !reflect.DeepEqual(updated.ConvertedBy, fresh.ConvertedBy) {
			conversion := fresh.ConvertedBy
			shard.ConvertedBy = &conversion
		}
		updated.Shards = append(updated.Shards, shard)
	}
	if err := index.ValidateManifest(manifestPath, updated); err != nil {
		return index.Manifest{}, err
	}
	return updated, nil
}

func preserveSourceContext(existing, fresh []index.Source) []index.Source {
	for position := range fresh {
		for _, prior := range existing {
			if prior.Name != fresh[position].Name && prior.URL != fresh[position].URL {
				continue
			}
			prior.Name = fresh[position].Name
			prior.Source = fresh[position].Source
			prior.URL = fresh[position].URL
			prior.Category = fresh[position].Category
			prior.SHA256 = fresh[position].SHA256
			prior.Files = fresh[position].Files
			if fresh[position].Version != "" {
				prior.Version = fresh[position].Version
			}
			if fresh[position].CollectedFrom != "" {
				prior.CollectedFrom = fresh[position].CollectedFrom
			}
			if fresh[position].CollectedTo != "" {
				prior.CollectedTo = fresh[position].CollectedTo
			}
			fresh[position] = prior
			break
		}
	}
	return fresh
}

func uniqueSourceName(existing []index.Source, source index.Source) string {
	for _, candidate := range existing {
		if candidate.Name == source.Name && candidate.SHA256 == source.SHA256 {
			return candidate.Name
		}
	}
	base := source.Name
	for _, candidate := range existing {
		if candidate.Name == base {
			base += "-" + source.SHA256[:12]
			break
		}
	}
	used := map[string]bool{}
	for _, candidate := range existing {
		used[candidate.Name] = true
	}
	name := base
	for suffix := 2; used[name]; suffix++ {
		name = fmt.Sprintf("%s-%d", base, suffix)
	}
	return name
}

func StageUpdateContribution(indexRoot, stagingDirectory string, plan Plan, manifest index.Manifest) (ContributionResult, error) {
	if plan.Update == nil {
		return ContributionResult{}, fmt.Errorf("update plan is required")
	}
	root, err := filepath.Abs(indexRoot)
	if err != nil {
		return ContributionResult{}, err
	}
	original := filepath.Join(root, filepath.FromSlash(plan.Update.Manifest))
	if err := verifyUpdateBase(original, plan.Update.ManifestSHA256); err != nil {
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
	finalExists := finalErr == nil
	if finalErr != nil && !os.IsNotExist(finalErr) {
		return ContributionResult{}, finalErr
	}
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		return ContributionResult{}, err
	}
	temporary, err := os.MkdirTemp(stagingRoot, ".waldo-update-contribution-*")
	if err != nil {
		return ContributionResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(temporary)
		}
	}()
	directoryRel := filepath.ToSlash(filepath.Dir(filepath.FromSlash(plan.Update.Manifest)))
	if directoryRel == "." {
		directoryRel = ""
	}
	manifestRel := filepath.ToSlash(filepath.Join(filepath.FromSlash(directoryRel), manifest.Name+index.YAMLExtension))
	result := ContributionResult{Root: finalRoot, Files: []string{manifestRel}}
	if err := writeContributionYAML(temporary, manifestRel, manifest); err != nil {
		return ContributionResult{}, err
	}
	parentPath := filepath.Join(root, filepath.FromSlash(directoryRel))
	directory, err := index.LoadDirectory(parentPath)
	if err != nil {
		return ContributionResult{}, err
	}
	originalName := filepath.Base(filepath.FromSlash(plan.Update.Manifest))
	found := false
	for position := range directory.Entries {
		if directory.Entries[position].Type == "manifest" && directory.Entries[position].Name == originalName {
			directory.Entries[position].Name = filepath.Base(manifestRel)
			found = true
		}
	}
	if !found {
		return ContributionResult{}, fmt.Errorf("directory index does not reference update manifest %s", plan.Update.Manifest)
	}
	directory.Entries = index.SortedEntries(directory.Entries)
	directoryIndexRel := filepath.ToSlash(filepath.Join(filepath.FromSlash(directoryRel), "index.yaml"))
	if err := writeContributionYAML(temporary, directoryIndexRel, directory); err != nil {
		return ContributionResult{}, err
	}
	result.Files = append(result.Files, directoryIndexRel)
	if plan.Update.Manifest != manifestRel {
		result.Removed = append(result.Removed, plan.Update.Manifest)
	}
	existingDirectoryPath, err := index.DirectoryPath(parentPath)
	if err != nil {
		return ContributionResult{}, err
	}
	existingDirectoryRel, err := filepath.Rel(root, existingDirectoryPath)
	if err != nil {
		return ContributionResult{}, err
	}
	existingDirectorySlash := filepath.ToSlash(existingDirectoryRel)
	if existingDirectorySlash != directoryIndexRel {
		result.Removed = append(result.Removed, existingDirectorySlash)
	}
	slices.Sort(result.Files)
	result.Files = slices.Compact(result.Files)
	slices.Sort(result.Removed)
	result.Removed = slices.Compact(result.Removed)
	if err := syncContributionTree(temporary); err != nil {
		return ContributionResult{}, err
	}
	if err := verifyUpdateBase(original, plan.Update.ManifestSHA256); err != nil {
		return ContributionResult{}, err
	}
	if finalExists {
		if err := compareContributionTrees(finalRoot, temporary, result.Files); err != nil {
			return ContributionResult{}, fmt.Errorf("existing staged contribution differs: %w", err)
		}
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

func ManifestFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func verifyUpdateBase(path, expected string) error {
	actual, err := ManifestFileSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("update manifest %s changed: sha256 is %s, expected %s", path, actual, expected)
	}
	return nil
}
