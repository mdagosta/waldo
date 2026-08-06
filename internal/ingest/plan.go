package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/shard"
)

type Plan struct {
	Kind           string                      `json:"kind"`
	Schema         int                         `json:"schema"`
	Destination    string                      `json:"destination"`
	Title          string                      `json:"title"`
	Description    string                      `json:"description"`
	License        string                      `json:"license"`
	Source         PlanSource                  `json:"source"`
	Mode           string                      `json:"mode"`
	MemoryBytes    int64                       `json:"memory_bytes"`
	Writer         WriterPlan                  `json:"writer"`
	Inputs         []PlanInput                 `json:"inputs"`
	RecipeEvidence *index.IngestRecipeEvidence `json:"ingest_recipe,omitempty"`
	Update         *UpdatePlan                 `json:"update,omitempty"`
}

type UpdatePlan struct {
	Manifest       string `json:"manifest"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Mode           string `json:"mode"`
}

type PlanSource struct {
	Name          string `json:"name"`
	Version       string `json:"version,omitempty"`
	URL           string `json:"url"`
	Category      string `json:"category"`
	CollectedFrom string `json:"collected_from,omitempty"`
	CollectedTo   string `json:"collected_to,omitempty"`
}

type WriterPlan struct {
	Format               string `json:"format"`
	RecordSchema         int    `json:"record_schema"`
	Recipe               string `json:"recipe"`
	CompressedTarget     int64  `json:"compressed_target_bytes"`
	CompressedMaximum    int64  `json:"compressed_maximum_bytes"`
	RowGroupLogicalBytes int64  `json:"row_group_logical_bytes"`
	PageBytes            int64  `json:"page_bytes"`
	AdapterBatchBytes    int64  `json:"adapter_batch_bytes"`
	RecordMaximumBytes   int64  `json:"record_maximum_bytes"`
	Compression          string `json:"compression"`
}

type PlanInput struct {
	Artifact   Artifact     `json:"artifact"`
	Adapter    string       `json:"adapter"`
	TextColumn string       `json:"text_column,omitempty"`
	SourcePath string       `json:"source_path,omitempty"`
	Profile    InputProfile `json:"profile,omitempty"`
}

type PlanRequest struct {
	Destination    string
	Title          string
	Description    string
	License        string
	Source         PlanSource
	Mode           string
	MemoryBytes    int64
	TextColumn     string
	Profile        InputProfile
	InputRoot      string
	RecipeEvidence *index.IngestRecipeEvidence
	Update         *UpdatePlan
}

func NewPlan(probe Probe, request PlanRequest) (Plan, error) {
	if probe.Kind != "waldo-ingest-probe" || probe.Schema != 1 || len(probe.Artifacts) == 0 {
		return Plan{}, fmt.Errorf("invalid or empty ingestion probe")
	}
	if strings.TrimSpace(request.Destination) == "" || strings.TrimSpace(request.Title) == "" || strings.TrimSpace(request.License) == "" {
		return Plan{}, fmt.Errorf("destination, title, and license are required")
	}
	if request.Source.Name == "" || request.Source.URL == "" || request.Source.Category == "" {
		return Plan{}, fmt.Errorf("source name, URL, and category are required")
	}
	category, ok := index.CanonicalSourceCategory(request.Source.Category)
	if !ok {
		return Plan{}, fmt.Errorf("unsupported source category %q", request.Source.Category)
	}
	request.Source.Category = category
	switch category {
	case index.SourcePublicDataset, index.SourcePrivateThirdParty, index.SourceOther:
	default:
		return Plan{}, fmt.Errorf("source category %q requires acquisition evidence fields that index ingest does not collect yet", category)
	}
	mode := request.Mode
	if mode == "" {
		mode = "streaming"
	}
	if mode != "streaming" && mode != "canonical" {
		return Plan{}, fmt.Errorf("ingestion mode must be streaming or canonical")
	}
	memory := request.MemoryBytes
	if memory == 0 {
		memory = 2 << 30
	}
	if memory < 256<<20 {
		return Plan{}, fmt.Errorf("ingestion memory budget must be at least 256 MiB")
	}
	plan := Plan{
		Kind: "waldo-ingest-plan", Schema: 1,
		Destination: request.Destination, Title: request.Title, Description: request.Description, License: request.License,
		Source: request.Source, Mode: mode, MemoryBytes: memory,
		RecipeEvidence: request.RecipeEvidence,
		Update:         request.Update,
		Writer: WriterPlan{
			Format: "parquet", RecordSchema: shard.TextRecordSchema, Recipe: shard.TextWriterRecipe,
			CompressedTarget: 256 << 20, CompressedMaximum: 512 << 20,
			RowGroupLogicalBytes: 64 << 20, PageBytes: 1 << 20,
			AdapterBatchBytes: 16 << 20, RecordMaximumBytes: 64 << 20,
			Compression: "zstd-level-6",
		},
	}
	if plan.Description == "" {
		plan.Description = "Training corpus acquired from " + request.Source.Name + "."
	}
	for _, artifact := range probe.Artifacts {
		input := PlanInput{Artifact: artifact, Profile: request.Profile}
		if request.InputRoot != "" {
			root, err := filepath.Abs(request.InputRoot)
			if err != nil {
				return Plan{}, err
			}
			relative, err := filepath.Rel(root, artifact.Path)
			if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return Plan{}, fmt.Errorf("input %s is outside recipe output %s", artifact.Path, root)
			}
			input.SourcePath = filepath.ToSlash(relative)
		}
		if err := input.Profile.Validate(); err != nil {
			return Plan{}, fmt.Errorf("%s: %w", artifact.Path, err)
		}
		if input.Profile.recordProfile() {
			switch artifact.Format {
			case "json", "jsonl", "parquet":
				input.Adapter = artifact.Format
			default:
				return Plan{}, fmt.Errorf("%s: profile %s requires JSON, JSONL, or Parquet, not %q", artifact.Path, input.Profile.Type, artifact.Format)
			}
			plan.Inputs = append(plan.Inputs, input)
			continue
		}
		switch input.Profile.Type {
		case ProfileGutenbergText:
			if artifact.Format != "text" {
				return Plan{}, fmt.Errorf("%s: gutenberg-text requires text input, not %q", artifact.Path, artifact.Format)
			}
			input.Adapter = ProfileGutenbergText
		case ProfileJATSXML:
			if artifact.Format != "xml" {
				return Plan{}, fmt.Errorf("%s: jats-xml requires XML input, not %q", artifact.Path, artifact.Format)
			}
			input.Adapter = ProfileJATSXML
		default:
			switch artifact.Format {
			case "text", "markdown":
				input.Adapter = artifact.Format
			case "parquet":
				input.Adapter = "parquet"
				column, err := chooseTextColumn(artifact, request.TextColumn)
				if err != nil {
					return Plan{}, fmt.Errorf("%s: %w", artifact.Path, err)
				}
				input.TextColumn = column
			case "jsonl":
				input.Adapter = "jsonl"
			default:
				return Plan{}, fmt.Errorf("%s: detected format %q has no enabled adapter", artifact.Path, artifact.Format)
			}
		}
		plan.Inputs = append(plan.Inputs, input)
	}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func chooseTextColumn(artifact Artifact, requested string) (string, error) {
	if artifact.Parquet == nil {
		return "", fmt.Errorf("Parquet footer information is missing")
	}
	if requested != "" {
		if slices.Contains(artifact.Parquet.Columns, requested) {
			if strings.Contains(requested, ".") {
				return "", fmt.Errorf("nested text column %q is not enabled yet", requested)
			}
			return requested, nil
		}
		return "", fmt.Errorf("requested text column %q is absent", requested)
	}
	var candidates []string
	for _, candidate := range []string{"text", "content", "document"} {
		if slices.Contains(artifact.Parquet.Columns, candidate) {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("cannot infer a text column; columns are %s", strings.Join(artifact.Parquet.Columns, ", "))
	}
	return "", fmt.Errorf("text column is ambiguous (%s); specify it explicitly", strings.Join(candidates, ", "))
}

func (plan Plan) Validate() error {
	if plan.Kind != "waldo-ingest-plan" || plan.Schema != 1 || plan.Writer.Format != "parquet" || plan.Writer.RecordSchema != shard.TextRecordSchema || plan.Writer.Recipe != shard.TextWriterRecipe {
		return fmt.Errorf("unsupported ingestion plan identity or writer")
	}
	cleanDestination := filepath.ToSlash(filepath.Clean(plan.Destination))
	if plan.Destination == "" || plan.Destination == "." || filepath.IsAbs(plan.Destination) || strings.HasPrefix(cleanDestination, "..") || plan.Destination != cleanDestination {
		return fmt.Errorf("destination must be a relative index path")
	}
	if plan.Title == "" || plan.Description == "" || plan.License == "" || plan.Source.Name == "" || plan.Source.URL == "" || plan.Source.Category == "" {
		return fmt.Errorf("ingestion plan is missing corpus or source identity")
	}
	if plan.Mode != "streaming" && plan.Mode != "canonical" {
		return fmt.Errorf("unsupported ingestion mode %q", plan.Mode)
	}
	if plan.Update != nil {
		cleanManifest := filepath.ToSlash(filepath.Clean(filepath.FromSlash(plan.Update.Manifest)))
		if cleanManifest == "." || cleanManifest != plan.Update.Manifest || filepath.IsAbs(filepath.FromSlash(plan.Update.Manifest)) || strings.HasPrefix(cleanManifest, "../") || !validSHA256(plan.Update.ManifestSHA256) || (plan.Update.Mode != "append" && plan.Update.Mode != "rebuild-shards") {
			return fmt.Errorf("ingestion update has invalid manifest identity or mode")
		}
	}
	if plan.MemoryBytes < 256<<20 || plan.Writer.CompressedTarget <= 0 || plan.Writer.CompressedMaximum < plan.Writer.CompressedTarget || plan.Writer.RowGroupLogicalBytes <= 0 || plan.Writer.PageBytes <= 0 || plan.Writer.AdapterBatchBytes <= 0 || plan.Writer.RecordMaximumBytes < plan.Writer.AdapterBatchBytes || plan.Writer.RecordMaximumBytes > plan.MemoryBytes/2 {
		return fmt.Errorf("ingestion plan has invalid resource or writer limits")
	}
	previous := ""
	for _, input := range plan.Inputs {
		artifact := input.Artifact
		if !filepath.IsAbs(artifact.Path) || artifact.Path <= previous || artifact.Bytes < 0 || !validSHA256(artifact.SHA256) {
			return fmt.Errorf("plan inputs must have sorted absolute paths, sizes, and hashes")
		}
		if input.SourcePath != "" {
			cleanSource := filepath.ToSlash(filepath.Clean(filepath.FromSlash(input.SourcePath)))
			if cleanSource == "." || cleanSource != input.SourcePath || strings.HasPrefix(cleanSource, "../") || filepath.IsAbs(filepath.FromSlash(input.SourcePath)) {
				return fmt.Errorf("input %s has invalid source path %q", artifact.Path, input.SourcePath)
			}
		}
		if err := input.Profile.Validate(); err != nil {
			return fmt.Errorf("input %s: %w", artifact.Path, err)
		}
		if input.Profile.recordProfile() {
			if input.Adapter != artifact.Format || (input.Adapter != "json" && input.Adapter != "jsonl" && input.Adapter != "parquet") || input.TextColumn != "" {
				return fmt.Errorf("input %s has an inconsistent record-profile adapter", artifact.Path)
			}
			previous = artifact.Path
			continue
		}
		switch input.Adapter {
		case "text", "markdown":
			if artifact.Format != input.Adapter || input.TextColumn != "" {
				return fmt.Errorf("input %s has an inconsistent text adapter", artifact.Path)
			}
		case "parquet":
			if artifact.Format != "parquet" || input.TextColumn == "" {
				return fmt.Errorf("Parquet input %s has no valid text column mapping", artifact.Path)
			}
		case "jsonl":
			if artifact.Format != "jsonl" || input.TextColumn != "" || (artifact.Compression != "" && artifact.Compression != "gzip" && artifact.Compression != "zstd") {
				return fmt.Errorf("input %s has an inconsistent JSONL adapter", artifact.Path)
			}
		case ProfileGutenbergText:
			if artifact.Format != "text" || input.Profile.Type != ProfileGutenbergText {
				return fmt.Errorf("input %s has an inconsistent Gutenberg adapter", artifact.Path)
			}
		case ProfileJATSXML:
			if artifact.Format != "xml" || input.Profile.Type != ProfileJATSXML {
				return fmt.Errorf("input %s has an inconsistent JATS adapter", artifact.Path)
			}
		default:
			return fmt.Errorf("input %s has unsupported adapter %q", artifact.Path, input.Adapter)
		}
		previous = artifact.Path
	}
	if len(plan.Inputs) == 0 {
		return fmt.Errorf("ingestion plan has no inputs")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Identity hashes the complete accepted plan. Execution journals pin this
// value and refuse to resume if any input, mapping, recipe, or limit changes.
func (plan Plan) Identity() (string, error) {
	if err := plan.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
