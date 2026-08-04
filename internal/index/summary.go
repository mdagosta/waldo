package index

import (
	"path"
	"sort"
	"strings"
)

type CorpusInfo struct {
	Path        string `json:"path"`
	Manifest    string `json:"manifest"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Measures
	Licenses []string `json:"licenses"`
	Sources  int      `json:"sources"`
}

// ListCorpora returns one summary for every manifest beneath target, ordered by
// logical corpus path. A corpus's logical path normally names its containing
// directory rather than exposing the repeated manifest filename.
func ListCorpora(target Target) ([]CorpusInfo, error) {
	var corpora []CorpusInfo
	err := WalkCorpora(target, func(corpus Corpus) error {
		manifest := corpus.Manifest
		measures := Measures{}
		licenses := map[string]bool{}
		if manifest.Rollup != nil {
			measures = Measures{
				Shards: manifest.Rollup.Shards,
				Docs:   manifest.Rollup.Docs,
				Tokens: manifest.Rollup.Tokens,
				Bytes:  manifest.Rollup.Bytes,
			}
			licenses[effectiveLicense(manifest.License)] = true
		} else {
			for _, shard := range manifest.Shards {
				measures.Shards++
				measures.Docs += shard.Docs
				measures.Tokens += shard.Tokens
				measures.Bytes += shard.Bytes
				licenses[effectiveLicense(manifest.EffectiveLicense(shard))] = true
			}
		}
		licenseList := make([]string, 0, len(licenses))
		for license := range licenses {
			licenseList = append(licenseList, license)
		}
		sort.Strings(licenseList)
		corpora = append(corpora, CorpusInfo{
			Path:        logicalCorpusPath(corpus.Path, manifest.Name),
			Manifest:    corpus.Path,
			Name:        manifest.Name,
			Title:       manifest.Title,
			Description: manifest.Description,
			Measures:    measures,
			Licenses:    licenseList,
			Sources:     len(manifest.Sources),
		})
		return nil
	})
	sort.Slice(corpora, func(i, j int) bool { return corpora[i].Path < corpora[j].Path })
	return corpora, err
}

// Summarize computes exact totals from the manifests indexed beneath target.
func Summarize(target Target) (Totals, error) {
	totals := Totals{Licenses: map[string]Measures{}}
	err := WalkCorpora(target, func(corpus Corpus) error {
		totals.Corpora++
		manifest := corpus.Manifest
		if manifest.Rollup != nil {
			add(&totals, manifest.License, manifest.Rollup.Shards, manifest.Rollup.Docs, manifest.Rollup.Tokens, manifest.Rollup.Bytes)
			return nil
		}
		for _, shard := range manifest.Shards {
			add(&totals, manifest.EffectiveLicense(shard), 1, shard.Docs, shard.Tokens, shard.Bytes)
		}
		return nil
	})
	return totals, err
}

func add(totals *Totals, license string, shards, docs, tokens, bytes int64) {
	license = effectiveLicense(license)
	totals.Shards += shards
	totals.Docs += docs
	totals.Tokens += tokens
	totals.Bytes += bytes
	licenseTotals := totals.Licenses[license]
	licenseTotals.Shards += shards
	licenseTotals.Docs += docs
	licenseTotals.Tokens += tokens
	licenseTotals.Bytes += bytes
	totals.Licenses[license] = licenseTotals
}

func effectiveLicense(license string) string {
	if license == "" {
		return "(none declared)"
	}
	return license
}

func logicalCorpusPath(manifestPath, name string) string {
	manifestPath = strings.TrimPrefix(path.Clean("/"+manifestPath), "/")
	dir := path.Dir(manifestPath)
	base := strings.TrimSuffix(path.Base(manifestPath), path.Ext(manifestPath))
	if base == name && path.Base(dir) == name {
		return dir
	}
	return strings.TrimSuffix(manifestPath, path.Ext(manifestPath))
}
