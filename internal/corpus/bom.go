package corpus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/lookaside"
)

const BOMSchema = 1

type BOM struct {
	Kind         string                    `json:"kind"`
	Schema       int                       `json:"schema"`
	Subject      string                    `json:"subject"`
	Index        index.Identity            `json:"index"`
	Paths        []string                  `json:"paths"`
	Policy       LicensePolicy             `json:"license_policy,omitempty"`
	Manifests    []ManifestPin             `json:"manifests"`
	SubManifests []SubManifestPin          `json:"sub_manifests,omitempty"`
	Shards       []ShardPin                `json:"shards"`
	Totals       index.Measures            `json:"totals"`
	Licenses     map[string]index.Measures `json:"licenses"`
}

type SubManifestPin struct {
	Manifest     string `json:"manifest"`
	ParentSHA256 string `json:"parent_sha256,omitempty"`
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	Count        int64  `json:"count"`
	Docs         int64  `json:"docs"`
	Tokens       int64  `json:"tokens"`
	Bytes        int64  `json:"bytes"`
	EncodedBytes int64  `json:"encoded_bytes"`
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
// shard pins. Inline selections are local. Rollup-backed selections first read
// their content-addressed submanifest objects through the verified cache;
// fetching the much larger leaf objects remains a separate materialization.
func BuildBOM(ctx context.Context, targets []index.Target, policy LicensePolicy, cache *lookaside.Cache) (BOM, error) {
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
			return bom.addManifest(ctx, root, corpus, policy, cache)
		})
		if err != nil {
			return BOM{}, err
		}
	}
	sort.Strings(bom.Paths)
	return bom, nil
}

func (bom *BOM) addManifest(ctx context.Context, root string, corpus index.Corpus, policy LicensePolicy, cache *lookaside.Cache) error {
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
	addShard := func(shard index.Shard) {
		license := corpus.Manifest.EffectiveLicense(shard)
		if !policy.Allows(license) {
			return
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
	if corpus.Manifest.Rollup != nil {
		if cache == nil {
			return fmt.Errorf("%s: a lookaside cache is required to resolve its sub-manifest tree", corpus.Path)
		}
		seen := map[string]bool{}
		if _, err := bom.expandRollup(ctx, corpus.Path, corpus.Manifest, *corpus.Manifest.Rollup, "", cache, seen, addShard); err != nil {
			return fmt.Errorf("%s: %w", corpus.Path, err)
		}
	} else {
		for _, shard := range corpus.Manifest.Shards {
			addShard(shard)
		}
	}
	// A selected manifest stays in the OpenWALDO BOM even when policy excludes all
	// of its shards. That makes an empty policy result explainable rather than
	// silently pretending the path was never considered.
	bom.Manifests = append(bom.Manifests, pin)
	return nil
}

func (bom *BOM) expandRollup(ctx context.Context, manifestPath string, manifest index.Manifest, rollup index.Rollup, parent string, cache *lookaside.Cache, seen map[string]bool, addShard func(index.Shard)) (index.Measures, error) {
	if err := validateRollup(rollup); err != nil {
		return index.Measures{}, err
	}
	if seen[rollup.SHA256] {
		return index.Measures{}, fmt.Errorf("sub-manifest %s is referenced more than once", rollup.SHA256[:12])
	}
	seen[rollup.SHA256] = true
	objectPath, err := cache.Fetch(ctx, rollup.URL, rollup.SHA256, 0)
	if err != nil {
		return index.Measures{}, fmt.Errorf("resolve sub-manifest %s: %w", rollup.SHA256[:12], err)
	}
	data, err := os.ReadFile(objectPath)
	if err != nil {
		return index.Measures{}, err
	}
	var sub index.SubManifest
	if err := json.Unmarshal(data, &sub); err != nil {
		return index.Measures{}, fmt.Errorf("sub-manifest %s: %w", rollup.SHA256[:12], err)
	}
	if sub.Kind != "sub-manifest" || sub.Schema != 1 {
		return index.Measures{}, fmt.Errorf("sub-manifest %s has unsupported identity %q schema %d", rollup.SHA256[:12], sub.Kind, sub.Schema)
	}
	bom.SubManifests = append(bom.SubManifests, SubManifestPin{
		Manifest: manifestPath, ParentSHA256: parent, URL: rollup.URL,
		SHA256: rollup.SHA256, Count: rollup.Count, Docs: rollup.Docs,
		Tokens: rollup.Tokens, Bytes: rollup.Bytes, EncodedBytes: int64(len(data)),
	})
	knownSources := make(map[string]bool, len(manifest.Sources))
	for _, source := range manifest.Sources {
		knownSources[source.Name] = true
	}
	actual := index.Measures{}
	for i, shard := range sub.Shards {
		if err := validateSubManifestShard(shard, knownSources); err != nil {
			return index.Measures{}, fmt.Errorf("sub-manifest %s shard %d: %w", rollup.SHA256[:12], i+1, err)
		}
		actual.Shards++
		actual.Docs += shard.Docs
		actual.Tokens += shard.Tokens
		actual.Bytes += shard.Bytes
		addShard(shard)
	}
	for _, child := range sub.Children {
		childActual, err := bom.expandRollup(ctx, manifestPath, manifest, child, rollup.SHA256, cache, seen, addShard)
		if err != nil {
			return index.Measures{}, err
		}
		actual.Shards += childActual.Shards
		actual.Docs += childActual.Docs
		actual.Tokens += childActual.Tokens
		actual.Bytes += childActual.Bytes
	}
	declared := index.Measures{Shards: rollup.Count, Docs: rollup.Docs, Tokens: rollup.Tokens, Bytes: rollup.Bytes}
	if actual != declared {
		return index.Measures{}, fmt.Errorf("sub-manifest %s totals are %+v, reference declares %+v", rollup.SHA256[:12], actual, declared)
	}
	return actual, nil
}

func validateRollup(rollup index.Rollup) error {
	if rollup.URL == "" || !validSHA256(rollup.SHA256) {
		return fmt.Errorf("sub-manifest reference requires a URL and lowercase 64-character sha256")
	}
	if rollup.Count <= 0 || rollup.Docs <= 0 || rollup.Tokens <= 0 || rollup.Bytes <= 0 {
		return fmt.Errorf("sub-manifest reference count, docs, tokens, and bytes must be positive")
	}
	return nil
}

func validateSubManifestShard(shard index.Shard, sources map[string]bool) error {
	if shard.URL == "" || !validSHA256(shard.SHA256) {
		return fmt.Errorf("requires a URL and lowercase 64-character sha256")
	}
	if shard.Docs <= 0 || shard.Tokens <= 0 || shard.Bytes <= 0 {
		return fmt.Errorf("docs, tokens, and bytes must be positive")
	}
	seen := map[string]bool{}
	for _, source := range shard.Sources {
		if !sources[source] {
			return fmt.Errorf("refers to unknown source %q", source)
		}
		if seen[source] {
			return fmt.Errorf("lists source %q more than once", source)
		}
		seen[source] = true
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
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
