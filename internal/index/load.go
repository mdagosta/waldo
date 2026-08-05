package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const indexFile = "index.json"

// Target is a resolved path inside one explicit index checkout.
type Target struct {
	Root string
	Abs  string
	Rel  string
}

// Resolve locates a target inside an index checkout. A knownRoot is used only
// by internal callers resolving several logical paths in one already-discovered
// checkout. User-facing commands discover the checkout from a positional
// filesystem path or, for logical paths, from the current directory.
func Resolve(knownRoot, target string) (Target, error) {
	if knownRoot != "" {
		root, err := findRoot(knownRoot)
		if err != nil {
			return Target{}, fmt.Errorf("index checkout %s: %w", knownRoot, err)
		}
		if target == "" {
			return Target{Root: root, Abs: root}, nil
		}
		return within(root, target)
	}

	if target != "" && isFilesystemPath(target) {
		abs, err := filepath.Abs(target)
		if err != nil {
			return Target{}, err
		}
		root, err := findRoot(abs)
		if err != nil {
			return Target{}, err
		}
		return within(root, abs)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return Target{}, err
	}
	root, err := findRoot(cwd)
	if err != nil {
		return Target{}, fmt.Errorf("current directory: %w (pass an index path positionally)", err)
	}
	if target == "" {
		return Target{Root: root, Abs: root}, nil
	}
	return within(root, target)
}

// ResolveDestination discovers the checkout for a prospective positional
// destination. Unlike Resolve, the leaf need not exist yet; its nearest
// existing ancestor must be inside an index checkout.
func ResolveDestination(target string) (Target, error) {
	if strings.TrimSpace(target) == "" {
		return Target{}, fmt.Errorf("index destination is required")
	}
	expanded, err := expandUser(target)
	if err != nil {
		return Target{}, err
	}
	var abs string
	if isFilesystemPath(expanded) {
		abs, err = filepath.Abs(expanded)
		if err != nil {
			return Target{}, err
		}
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return Target{}, err
		}
		root, err := findRoot(cwd)
		if err != nil {
			return Target{}, fmt.Errorf("current directory: %w (pass an absolute destination inside an index checkout)", err)
		}
		abs = filepath.Join(root, filepath.FromSlash(expanded))
	}
	abs = filepath.Clean(abs)
	ancestor := abs
	for {
		if _, err := os.Stat(ancestor); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return Target{}, err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return Target{}, fmt.Errorf("index destination %s has no existing ancestor", target)
		}
		ancestor = parent
	}
	root, err := findRoot(ancestor)
	if err != nil {
		return Target{}, fmt.Errorf("index destination %s: %w", target, err)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return Target{}, err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Target{}, fmt.Errorf("index destination must name a new path beneath checkout %s", root)
	}
	return Target{Root: root, Abs: abs, Rel: filepath.ToSlash(rel)}, nil
}

func findRoot(location string) (string, error) {
	location, err := expandUser(location)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(location)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}

	var nearest string
	for dir := abs; ; dir = filepath.Dir(dir) {
		if isIndexDirectory(dir) {
			nearest = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	if nearest == "" {
		return "", fmt.Errorf("no %s found in this path or its parents", indexFile)
	}

	root := nearest
	for {
		parent := filepath.Dir(root)
		if parent == root || !isIndexDirectory(parent) {
			return root, nil
		}
		root = parent
	}
}

func within(root, target string) (Target, error) {
	abs, err := expandUser(target)
	if err != nil {
		return Target{}, err
	}
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(abs))
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return Target{}, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Target{}, fmt.Errorf("target %s is outside index checkout %s", target, root)
	}
	if _, err := os.Stat(abs); err != nil {
		return Target{}, fmt.Errorf("index path %s: %w", target, err)
	}
	if rel == "." {
		rel = ""
	}
	return Target{Root: root, Abs: abs, Rel: filepath.ToSlash(rel)}, nil
}

func isFilesystemPath(value string) bool {
	return filepath.IsAbs(value) || strings.HasPrefix(value, ".") || strings.HasPrefix(value, "~")
}

func expandUser(value string) (string, error) {
	if value != "~" && !strings.HasPrefix(value, "~/") {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand %s: %w", value, err)
	}
	if value == "~" {
		return home, nil
	}
	return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(value, "~/"))), nil
}

func isIndexDirectory(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, indexFile))
	return err == nil && !info.IsDir()
}

func LoadDirectory(dir string) (Directory, error) {
	path := filepath.Join(dir, indexFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return Directory{}, err
	}
	var index Directory
	if err := json.Unmarshal(data, &index); err != nil {
		return Directory{}, fmt.Errorf("%s: %w", path, err)
	}
	return index, nil
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", path, err)
	}
	return manifest, nil
}

// WalkCorpora visits every manifest indexed beneath target. Filesystem content
// absent from index.json is deliberately ignored.
func WalkCorpora(target Target, visit func(Corpus) error) error {
	info, err := os.Stat(target.Abs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		manifest, err := LoadManifest(target.Abs)
		if err != nil {
			return err
		}
		return visit(Corpus{Path: target.Rel, Manifest: manifest})
	}
	return walkDirectory(target.Root, target.Abs, visit)
}

func walkDirectory(root, dir string, visit func(Corpus) error) error {
	index, err := LoadDirectory(dir)
	if err != nil {
		return err
	}
	for _, entry := range index.Entries {
		path := filepath.Join(dir, entry.Name)
		switch entry.Type {
		case "dir":
			if err := walkDirectory(root, path, visit); err != nil {
				return err
			}
		case "manifest":
			manifest, err := LoadManifest(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if err := visit(Corpus{Path: filepath.ToSlash(rel), Manifest: manifest}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: entry %q has unsupported type %q", filepath.Join(dir, indexFile), entry.Name, entry.Type)
		}
	}
	return nil
}

func SortedEntries(entries []Entry) []Entry {
	result := append([]Entry(nil), entries...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
