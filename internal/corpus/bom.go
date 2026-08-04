package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/openwaldo/waldo-new/internal/index"
)

const BOMSchema = 1

type BOM struct {
	Kind      string                    `json:"kind"`
	Schema    int                       `json:"schema"`
	Subject   string                    `json:"subject"`
	Index     index.Identity            `json:"index"`
	Paths     []string                  `json:"paths"`
	Policy    LicensePolicy             `json:"license_policy,omitempty"`
	Manifests []ManifestPin             `json:"manifests"`
	Shards    []ShardPin                `json:"shards"`
	Totals    index.Measures            `json:"totals"`
	Licenses  map[string]index.Measures `json:"licenses"`
}

type ManifestPin struct {
	Path        string                    `json:"path"`
	SHA256      string                    `json:"sha256"`
	Name        string                    `json:"name"`
	Title       string                    `json:"title"`
	Description string                    `json:"description"`
	License     string                    `json:"license"`
	Sources     []index.Source            `json:"sources"`
	Totals      index.Measures            `json:"totals"`
	Licenses    map[string]index.Measures `json:"licenses"`
}

type ShardPin struct {
	Manifest string   `json:"manifest"`
	URL      string   `json:"url"`
	SHA256   string   `json:"sha256"`
	Format   string   `json:"format,omitempty"`
	License  string   `json:"license"`
	Sources  []string `json:"sources,omitempty"`
	Docs     int64    `json:"docs"`
	Tokens   int64    `json:"tokens"`
	Bytes    int64    `json:"bytes"`
}

// BuildBOM resolves targets from one checkout into immutable manifest and
// shard pins. It reads no object bytes; materialization through lookaside is a
// separate step over this resolved plan.
func BuildBOM(targets []index.Target, policy LicensePolicy) (BOM, error) {
	if len(targets) == 0 {
		return BOM{}, fmt.Errorf("at least one index target is required")
	}
	root := targets[0].Root
	bom := BOM{
		Kind:     "openwaldo-bom",
		Schema:   BOMSchema,
		Subject:  "corpus",
		Index:    index.Identify(root),
		Policy:   policy,
		Licenses: map[string]index.Measures{},
	}
	targets = append([]index.Target(nil), targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].Rel < targets[j].Rel })
	seenPaths := map[string]bool{}
	seenManifests := map[string]bool{}
	for _, target := range targets {
		if target.Root != root {
			return BOM{}, fmt.Errorf("index targets span different checkouts: %s and %s", root, target.Root)
		}
		if !seenPaths[target.Rel] {
			bom.Paths = append(bom.Paths, target.Rel)
			seenPaths[target.Rel] = true
		}
		err := index.WalkCorpora(target, func(corpus index.Corpus) error {
			if seenManifests[corpus.Path] {
				return nil
			}
			seenManifests[corpus.Path] = true
			return bom.addManifest(root, corpus, policy)
		})
		if err != nil {
			return BOM{}, err
		}
	}
	sort.Strings(bom.Paths)
	return bom, nil
}

func (bom *BOM) addManifest(root string, corpus index.Corpus, policy LicensePolicy) error {
	if corpus.Manifest.Rollup != nil {
		return fmt.Errorf("%s: rollup-backed corpora are not supported by OpenWALDO BOMs yet", corpus.Path)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(corpus.Path)))
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	pin := ManifestPin{
		Path:        corpus.Path,
		SHA256:      hex.EncodeToString(digest[:]),
		Name:        corpus.Manifest.Name,
		Title:       corpus.Manifest.Title,
		Description: corpus.Manifest.Description,
		License:     corpus.Manifest.License,
		Sources:     append([]index.Source(nil), corpus.Manifest.Sources...),
		Licenses:    map[string]index.Measures{},
	}
	for _, shard := range corpus.Manifest.Shards {
		license := corpus.Manifest.EffectiveLicense(shard)
		if !policy.Allows(license) {
			continue
		}
		shardPin := ShardPin{
			Manifest: corpus.Path,
			URL:      shard.URL,
			SHA256:   shard.SHA256,
			Format:   effectiveFormat(corpus.Manifest.Format, shard.Format),
			License:  license,
			Sources:  append([]string(nil), shard.Sources...),
			Docs:     shard.Docs,
			Tokens:   shard.Tokens,
			Bytes:    shard.Bytes,
		}
		bom.Shards = append(bom.Shards, shardPin)
		addMeasures(&bom.Totals, shardPin)
		addLicenseMeasures(bom.Licenses, shardPin)
		addMeasures(&pin.Totals, shardPin)
		addLicenseMeasures(pin.Licenses, shardPin)
	}
	// A selected manifest stays in the OpenWALDO BOM even when policy excludes all
	// of its shards. That makes an empty policy result explainable rather than
	// silently pretending the path was never considered.
	bom.Manifests = append(bom.Manifests, pin)
	return nil
}

func addMeasures(measures *index.Measures, shard ShardPin) {
	measures.Shards++
	measures.Docs += shard.Docs
	measures.Tokens += shard.Tokens
	measures.Bytes += shard.Bytes
}

func addLicenseMeasures(licenses map[string]index.Measures, shard ShardPin) {
	measures := licenses[shard.License]
	measures.Shards++
	measures.Docs += shard.Docs
	measures.Tokens += shard.Tokens
	measures.Bytes += shard.Bytes
	licenses[shard.License] = measures
}

func effectiveFormat(manifestFormat, shardFormat string) string {
	if shardFormat != "" {
		return shardFormat
	}
	if manifestFormat == "" {
		return "parquet"
	}
	return manifestFormat
}
