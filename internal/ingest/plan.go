package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/openwaldo/waldo-new/internal/index"
)

type Plan struct {
	Kind        string      `json:"kind"`
	Schema      int         `json:"schema"`
	Destination string      `json:"destination"`
	Title       string      `json:"title"`
	License     string      `json:"license"`
	Source      PlanSource  `json:"source"`
	Mode        string      `json:"mode"`
	MemoryBytes int64       `json:"memory_bytes"`
	Writer      WriterPlan  `json:"writer"`
	Inputs      []PlanInput `json:"inputs"`
}

type PlanSource struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Category string `json:"category"`
}

type WriterPlan struct {
	Format               string `json:"format"`
	RecordSchema         int    `json:"record_schema"`
	CompressedTarget     int64  `json:"compressed_target_bytes"`
	CompressedMaximum    int64  `json:"compressed_maximum_bytes"`
	RowGroupLogicalBytes int64  `json:"row_group_logical_bytes"`
	PageBytes            int64  `json:"page_bytes"`
	Compression          string `json:"compression"`
}

type PlanInput struct {
	Artifact   Artifact `json:"artifact"`
	Adapter    string   `json:"adapter"`
	TextColumn string   `json:"text_column,omitempty"`
}

type PlanRequest struct {
	Destination string
	Title       string
	License     string
	Source      PlanSource
	Mode        string
	MemoryBytes int64
	TextColumn  string
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
		Destination: request.Destination, Title: request.Title, License: request.License,
		Source: request.Source, Mode: mode, MemoryBytes: memory,
		Writer: WriterPlan{
			Format: "parquet", RecordSchema: 2,
			CompressedTarget: 256 << 20, CompressedMaximum: 512 << 20,
			RowGroupLogicalBytes: 64 << 20, PageBytes: 1 << 20,
			Compression: "zstd-level-6",
		},
	}
	for _, artifact := range probe.Artifacts {
		input := PlanInput{Artifact: artifact}
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
		default:
			return Plan{}, fmt.Errorf("%s: detected format %q has no enabled adapter", artifact.Path, artifact.Format)
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
	if plan.Kind != "waldo-ingest-plan" || plan.Schema != 1 || plan.Writer.Format != "parquet" || plan.Writer.RecordSchema != 2 {
		return fmt.Errorf("unsupported ingestion plan identity or writer")
	}
	cleanDestination := filepath.ToSlash(filepath.Clean(plan.Destination))
	if plan.Destination == "" || plan.Destination == "." || filepath.IsAbs(plan.Destination) || strings.HasPrefix(cleanDestination, "..") || plan.Destination != cleanDestination {
		return fmt.Errorf("destination must be a relative index path")
	}
	if plan.Title == "" || plan.License == "" || plan.Source.Name == "" || plan.Source.URL == "" || plan.Source.Category == "" {
		return fmt.Errorf("ingestion plan is missing corpus or source identity")
	}
	if plan.Mode != "streaming" && plan.Mode != "canonical" {
		return fmt.Errorf("unsupported ingestion mode %q", plan.Mode)
	}
	if plan.MemoryBytes < 256<<20 || plan.Writer.CompressedTarget <= 0 || plan.Writer.CompressedMaximum < plan.Writer.CompressedTarget || plan.Writer.RowGroupLogicalBytes <= 0 || plan.Writer.PageBytes <= 0 {
		return fmt.Errorf("ingestion plan has invalid resource or writer limits")
	}
	previous := ""
	for _, input := range plan.Inputs {
		artifact := input.Artifact
		if !filepath.IsAbs(artifact.Path) || artifact.Path <= previous || artifact.Bytes < 0 || !validSHA256(artifact.SHA256) {
			return fmt.Errorf("plan inputs must have sorted absolute paths, sizes, and hashes")
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
