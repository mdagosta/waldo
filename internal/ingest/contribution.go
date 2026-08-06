package ingest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/shard"
	"github.com/openwaldo/waldo/internal/tokenizer"
)

// BuildManifest converts a completed assembly into the compact schema-1 shape
// used by the public index: one source identity and one entry per shard.
func BuildManifest(plan Plan, assembly AssemblyResult, objectBase string) (index.Manifest, error) {
	if err := plan.Validate(); err != nil {
		return index.Manifest{}, err
	}
	if len(assembly.Objects) == 0 || assembly.RetainedDocs <= 0 || strings.TrimSpace(objectBase) == "" {
		return index.Manifest{}, fmt.Errorf("completed assembly and public object base are required")
	}
	name := path.Base(plan.Destination)
	sourceHash, err := sourceAcquisitionIdentity(plan)
	if err != nil {
		return index.Manifest{}, err
	}
	manifest := index.Manifest{
		Kind: "manifest", Schema: index.ManifestSchema, Name: name, Title: plan.Title,
		Description: plan.Description, License: plan.License,
		RecordSchema: shard.TextRecordSchema,
		Sources: []index.Source{{
			Name: plan.Source.Name, Source: plan.Source.Name, URL: plan.Source.URL,
			Category: plan.Source.Category, SHA256: sourceHash,
		}},
		ConvertedBy: index.Conversion{
			Tool: "waldo index ingest", Version: "0.1.0-dev",
			Collector: compactCollector(plan.RecipeEvidence), Profile: "canonical-text-schema-1",
			Recipe: shard.TextWriterRecipe, Tokenizer: tokenizer.Default,
		},
	}
	for _, object := range assembly.Objects {
		objectURL, err := contentAddressedURL(objectBase, object.SHA256)
		if err != nil {
			return index.Manifest{}, err
		}
		manifest.Shards = append(manifest.Shards, index.Shard{
			URL: objectURL, SHA256: object.SHA256, Sources: []string{plan.Source.Name},
			Docs: object.Docs, Tokens: object.Tokens, Bytes: object.Bytes,
		})
	}
	validationPath := filepath.Join(plan.Destination, name+index.YAMLExtension)
	if err := index.ValidateManifest(validationPath, manifest); err != nil {
		return index.Manifest{}, err
	}
	return manifest, nil
}

func compactCollector(recipe *index.IngestRecipeEvidence) string {
	if recipe == nil {
		return ""
	}
	repository := strings.TrimSuffix(strings.TrimSpace(recipe.Repository), ".git")
	if repository == "" {
		repository = "local"
	}
	commit := strings.TrimSpace(recipe.Commit)
	if commit == "" {
		commit = "uncommitted"
	}
	if recipe.Dirty {
		commit += "+dirty"
	}
	collector := repository + "@" + commit + ":" + filepath.ToSlash(recipe.Path)
	if recipe.Dirty || recipe.Commit == "" {
		collector += "#sha256=" + recipe.SHA256
	}
	return collector
}

func sourceAcquisitionIdentity(plan Plan) (string, error) {
	// This is an aggregate identity, not a Git-resident artifact inventory.
	// Length-prefix every field so concatenated inputs cannot be ambiguous, and
	// stream into the digest so source count does not imply equivalent memory.
	hasher := sha256.New()
	writeIdentityString(hasher, "waldo-acquisition-identity")
	writeIdentityString(hasher, "1")
	for _, input := range plan.Inputs {
		writeIdentityString(hasher, input.Artifact.SHA256)
		writeIdentityInt64(hasher, input.Artifact.Bytes)
		writeIdentityString(hasher, input.Artifact.Format)
		writeIdentityString(hasher, input.Artifact.Compression)
		writeIdentityString(hasher, input.Adapter)
		writeIdentityString(hasher, input.TextColumn)
		writeIdentityString(hasher, input.SourcePath)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func writeIdentityString(destination hash.Hash, value string) {
	writeIdentityInt64(destination, int64(len(value)))
	_, _ = destination.Write([]byte(value))
}

func writeIdentityInt64(destination hash.Hash, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = destination.Write(encoded[:])
}

func contentAddressedURL(base, digest string) (string, error) {
	if len(digest) != sha256.Size*2 {
		return "", fmt.Errorf("invalid object digest %q", digest)
	}
	parsed, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return "", err
	}
	objectPath := path.Join(digest[:2], digest[2:4], digest)
	if parsed.Scheme == "" {
		return filepath.Join(base, filepath.FromSlash(objectPath)), nil
	}
	parsed.Path = path.Join(parsed.Path, objectPath)
	return parsed.String(), nil
}
