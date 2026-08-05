package model

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Listing struct {
	Name       string   `json:"name"`
	ID         string   `json:"id"`
	Path       string   `json:"path"`
	Parameters uint64   `json:"approximate_parameters"`
	Runs       int      `json:"runs"`
	State      RunState `json:"state,omitempty"`
	Updated    string   `json:"updated"`
}

func List(root string, patterns []string) ([]Listing, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, pattern := range patterns {
		if _, err := filepath.Match(pattern, ""); err != nil {
			return nil, fmt.Errorf("invalid model pattern %q: %w", pattern, err)
		}
	}
	result := make([]Listing, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !validName.MatchString(entry.Name()) || !matchesAny(entry.Name(), patterns) {
			continue
		}
		inspection, err := Inspect(root, entry.Name())
		if err != nil {
			return nil, err
		}
		listing := Listing{
			Name: entry.Name(), ID: inspection.Model.ID, Path: inspection.Path,
			Parameters: inspection.Model.Forecast.ApproximateParameters,
			Runs:       len(inspection.Model.Runs), Updated: inspection.Model.Updated,
		}
		if len(inspection.Model.Runs) > 0 {
			listing.State = inspection.Model.Runs[len(inspection.Model.Runs)-1].State
		}
		result = append(result, listing)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func matchesAny(name string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
	}
	return false
}

func Remove(root string, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("at least one model name is required")
	}
	paths := make([]string, len(names))
	seen := map[string]bool{}
	for index, name := range names {
		if err := ValidateName(name); err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, fmt.Errorf("model %q was named more than once", name)
		}
		seen[name] = true
		inspection, err := Inspect(root, name)
		if err != nil {
			return nil, err
		}
		paths[index] = inspection.Path
	}
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			return nil, err
		}
	}
	return append([]string(nil), names...), nil
}

func Exists(root, name string) (bool, error) {
	if err := ValidateName(name); err != nil {
		return false, err
	}
	info, err := os.Stat(filepath.Join(root, name))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("model path for %q is not a directory", name)
	}
	if _, err := Inspect(root, name); err != nil {
		return false, err
	}
	return true, nil
}

func Export(root, name, destination string) (string, error) {
	inspection, err := Inspect(root, name)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	inside, err := filepath.Rel(inspection.Path, absolute)
	if err != nil {
		return "", err
	}
	if inside == "." || inside != ".." && !strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("model export destination must not be inside the source model")
	}
	if _, err := os.Stat(absolute); err == nil {
		return "", fmt.Errorf("%s already exists", absolute)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(absolute)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	temporary, err := os.MkdirTemp(parent, ".waldo-model-export-*")
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := copyTree(inspection.Path, temporary); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, absolute); err != nil {
		return "", err
	}
	committed = true
	return absolute, nil
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("model export refuses symbolic link %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = sourceFile.Close()
			return err
		}
		_, copyErr := io.Copy(targetFile, sourceFile)
		sourceCloseErr := sourceFile.Close()
		closeErr := targetFile.Close()
		if copyErr != nil {
			return copyErr
		}
		if sourceCloseErr != nil {
			return sourceCloseErr
		}
		return closeErr
	})
}
