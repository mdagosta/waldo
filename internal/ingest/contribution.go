// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/shard"
	"github.com/openwaldo/waldo/internal/tokenizer"
)

// BuildManifest converts a completed assembly into a compact index manifest:
// one identity per declared source and one entry per physical shard.
func BuildManifest(plan Plan, assembly AssemblyResult, objectBase string) (index.Manifest, error) {
	if err := plan.Validate(); err != nil {
		return index.Manifest{}, err
	}
	if len(assembly.Objects) == 0 || assembly.RetainedDocs <= 0 || strings.TrimSpace(objectBase) == "" {
		return index.Manifest{}, fmt.Errorf("completed assembly and public object base are required")
	}
	name := path.Base(plan.Destination)
	manifest := index.Manifest{
		Kind: "manifest", Schema: index.ManifestSchema, Name: name, Title: plan.Title,
		Description:  plan.Description,
		RecordSchema: shard.TextRecordSchema,
		ConvertedBy: index.Conversion{
			Tool: "waldo index ingest", Version: "0.1.0-dev",
			Collector: compactCollector(plan.RecipeEvidence), Profile: conversionProfile(plan),
			Recipe: shard.TextWriterRecipe, Tokenizer: tokenizer.Default,
		},
	}
	planSources := plan.Sources
	if len(planSources) == 0 {
		legacy := plan.Source
		legacy.License = plan.License
		planSources = []PlanSource{legacy}
	}
	for _, source := range planSources {
		sourceHash, err := sourceAcquisitionIdentity(plan, source.ID)
		if err != nil {
			return index.Manifest{}, err
		}
		manifest.Sources = append(manifest.Sources, index.Source{
			Name: source.Name, Source: source.Name, URL: source.URL, License: source.License,
			Version: source.Version, Category: source.Category,
			CollectedFrom: source.CollectedFrom, CollectedTo: source.CollectedTo,
			LicenseEvidence: source.LicenseEvidence, Content: source.Content, Acquisition: source.Acquisition,
			SHA256: sourceHash,
		})
	}
	licenseSet := map[string]bool{}
	for _, object := range assembly.Objects {
		licenses := object.Licenses
		if len(licenses) == 0 && object.License != "" {
			licenses = []string{object.License}
		}
		if len(licenses) == 0 {
			return index.Manifest{}, fmt.Errorf("assembled object %s has no effective license", object.SHA256)
		}
		for _, license := range licenses {
			licenseSet[license] = true
		}
		objectURL, err := contentAddressedURL(objectBase, object.SHA256)
		if err != nil {
			return index.Manifest{}, err
		}
		manifest.Shards = append(manifest.Shards, index.Shard{
			URL: objectURL, SHA256: object.SHA256, Sources: object.Sources,
			Docs: object.Docs, Tokens: object.Tokens, Bytes: object.Bytes,
			LicenseUsage: object.LicenseUsage,
		})
		if len(licenses) == 1 {
			manifest.Shards[len(manifest.Shards)-1].License = licenses[0]
		} else {
			manifest.Shards[len(manifest.Shards)-1].Licenses = append([]string(nil), licenses...)
		}
	}
	allLicenses := make([]string, 0, len(licenseSet))
	for license := range licenseSet {
		allLicenses = append(allLicenses, license)
	}
	sort.Strings(allLicenses)
	if len(allLicenses) == 1 {
		manifest.License = allLicenses[0]
	} else {
		manifest.Licenses = allLicenses
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

func sourceAcquisitionIdentity(plan Plan, sourceID string) (string, error) {
	// This is an aggregate identity, not a Git-resident artifact inventory.
	// Length-prefix every field so concatenated inputs cannot be ambiguous, and
	// stream into the digest so source count does not imply equivalent memory.
	hasher := sha256.New()
	writeIdentityString(hasher, "waldo-acquisition-identity")
	writeIdentityString(hasher, "2")
	source, license, err := plan.sourceFor(PlanInput{SourceID: sourceID})
	if err != nil {
		return "", err
	}
	if source.License == "" {
		source.License = license
	}
	encodedSource, err := json.Marshal(source)
	if err != nil {
		return "", err
	}
	writeIdentityString(hasher, string(encodedSource))
	for _, input := range plan.Inputs {
		if input.SourceID != sourceID {
			continue
		}
		writeIdentityString(hasher, input.Artifact.SHA256)
		writeIdentityInt64(hasher, input.Artifact.Bytes)
		writeIdentityString(hasher, input.Artifact.Format)
		writeIdentityString(hasher, input.Artifact.Compression)
		writeIdentityString(hasher, input.Adapter)
		writeIdentityString(hasher, input.TextColumn)
		writeIdentityString(hasher, input.SourcePath)
		profile, err := json.Marshal(input.Profile)
		if err != nil {
			return "", err
		}
		writeIdentityString(hasher, string(profile))
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func conversionProfile(plan Plan) string {
	profiles := make([]InputProfile, len(plan.Inputs))
	configured := false
	for position, input := range plan.Inputs {
		profiles[position] = input.Profile
		configured = configured || input.Profile.Type != ""
	}
	if !configured {
		return "canonical-text-schema-1"
	}
	encoded, _ := json.Marshal(profiles)
	digest := sha256.Sum256(encoded)
	return "canonical-text-schema-1@sha256:" + hex.EncodeToString(digest[:])
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
