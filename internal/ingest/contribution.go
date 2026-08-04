package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/shard"
)

// BuildManifest converts a completed assembly into a public-index-compatible
// schema-1 manifest using the additive schema-2 provenance fields.
func BuildManifest(plan Plan, assembly AssemblyResult, objectBase string) (index.Manifest, error) {
	if err := plan.Validate(); err != nil {
		return index.Manifest{}, err
	}
	if len(assembly.Objects) == 0 || assembly.RetainedDocs <= 0 || strings.TrimSpace(objectBase) == "" {
		return index.Manifest{}, fmt.Errorf("completed assembly and public object base are required")
	}
	name := path.Base(plan.Destination)
	usage := index.ModalityMeasure{Samples: assembly.RetainedDocs, Items: assembly.RetainedDocs}
	var tokens int64
	for _, object := range assembly.Objects {
		usage.ContentBytes += object.LogicalBytes
		usage.Tokens += object.Tokens
		tokens += object.Tokens
	}
	sourceHash, files, err := sourceAcquisitionIdentity(plan)
	if err != nil {
		return index.Manifest{}, err
	}
	manifest := index.Manifest{
		Kind: "manifest", Schema: 1, Name: name, Title: plan.Title,
		Description: plan.Description, License: plan.License, Format: "parquet",
		RecordSchema: shard.TextRecordSchema,
		Sources: []index.Source{{
			Name: plan.Source.Name, Source: plan.Source.Name, URL: plan.Source.URL,
			Category: plan.Source.Category, SHA256: sourceHash, Files: files,
			Usage: index.Modalities{"text": usage},
			Content: &index.Content{
				Types: []string{"text"}, PersonalData: "unknown",
				Copyrighted: "unknown", MachineGenerated: "unknown",
			},
		}},
		ConvertedBy: index.Conversion{
			Tool: "waldo index add", Version: "0.1.0-dev",
			Profile: "canonical-text-schema-2", Recipe: shard.TextWriterRecipe,
		},
		Processing: &index.Processing{Steps: []index.ProcessingStep{
			{Name: "decode", Description: "Read the accepted text or projected Parquet mapping without an interchange materialization."},
			{Name: "validate", Description: "Require scalar NUL-free UTF-8 records within the accepted size limit."},
			{Name: "deduplicate", Description: "Retain the first occurrence of each exact SHA-256 text identity in stable acquisition order."},
			{Name: "encode", Description: "Write canonical text record schema 2 using the manifest's pinned Parquet recipe."},
		}},
	}
	for _, object := range assembly.Objects {
		objectURL, err := contentAddressedURL(objectBase, object.SHA256)
		if err != nil {
			return index.Manifest{}, err
		}
		manifest.Shards = append(manifest.Shards, index.Shard{
			URL: objectURL, SHA256: object.SHA256, Sources: []string{plan.Source.Name},
			Docs: object.Docs, Tokens: object.Tokens, Bytes: object.Bytes,
			Modalities: index.Modalities{"text": {
				Samples: object.Docs, Items: object.Docs, Tokens: object.Tokens, ContentBytes: object.LogicalBytes,
			}},
		})
	}
	if tokens != usage.Tokens {
		return index.Manifest{}, fmt.Errorf("manifest token totals are inconsistent")
	}
	validationPath := filepath.Join(plan.Destination, name+".json")
	if err := index.ValidateManifest(validationPath, manifest); err != nil {
		return index.Manifest{}, err
	}
	return manifest, nil
}

func sourceAcquisitionIdentity(plan Plan) (string, []index.SourceFile, error) {
	type artifactIdentity struct {
		SHA256     string `json:"sha256"`
		Bytes      int64  `json:"bytes"`
		Format     string `json:"format"`
		Adapter    string `json:"adapter"`
		TextColumn string `json:"text_column,omitempty"`
	}
	wire := struct {
		Kind      string             `json:"kind"`
		Schema    int                `json:"schema"`
		Artifacts []artifactIdentity `json:"artifacts"`
	}{Kind: "waldo-acquisition-identity", Schema: 1}
	files := make([]index.SourceFile, 0, len(plan.Inputs))
	seenNames := map[string]bool{}
	for _, input := range plan.Inputs {
		wire.Artifacts = append(wire.Artifacts, artifactIdentity{
			SHA256: input.Artifact.SHA256, Bytes: input.Artifact.Bytes,
			Format: input.Artifact.Format, Adapter: input.Adapter, TextColumn: input.TextColumn,
		})
		name := filepath.Base(input.Artifact.Path)
		if seenNames[name] {
			name = input.Artifact.SHA256[:12] + "-" + name
		}
		seenNames[name] = true
		files = append(files, index.SourceFile{
			Name: name, URL: artifactEvidenceURL(plan.Source.URL, input.Artifact.SHA256), SHA256: input.Artifact.SHA256,
		})
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), files, nil
}

func artifactEvidenceURL(base, digest string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return base + "#sha256=" + digest
	}
	if parsed.Fragment == "" {
		parsed.Fragment = "sha256=" + digest
	} else {
		parsed.Fragment += "&sha256=" + digest
	}
	return parsed.String()
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
