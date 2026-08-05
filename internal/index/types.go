// Package index reads and verifies WALDO's Git metadata tree.
package index

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	DirectorySchema = 1
	ManifestSchema  = 1
)

// Directory is one index.json file. Directory indexes are generated navigation
// data; manifests remain the authority for corpus meaning.
type Directory struct {
	Kind    string  `json:"kind"`
	Schema  int     `json:"schema"`
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

type Entry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Manifest is the schema-1 corpus metadata needed for read compatibility. The
// decoder deliberately permits unknown fields so additive metadata does not
// make an older reader reject an otherwise compatible index.
type Manifest struct {
	Kind         string                `json:"kind"`
	Schema       int                   `json:"schema"`
	Name         string                `json:"name"`
	Title        string                `json:"title"`
	Description  string                `json:"description"`
	License      string                `json:"license"`
	Format       string                `json:"format,omitempty"`
	Sources      []Source              `json:"sources"`
	ConvertedBy  Conversion            `json:"converted_by"`
	RecordSchema int                   `json:"record_schema,omitempty"`
	Processing   *Processing           `json:"processing,omitempty"`
	ComposedBy   *IngestRecipeEvidence `json:"composed_by,omitempty"`
	Shards       []Shard               `json:"-"`
	Rollup       *Rollup               `json:"-"`
}

// UnmarshalJSON accepts the schema-1 polymorphic shards field: an inline
// array, or a content-addressed sub-manifest reference.
func (manifest *Manifest) UnmarshalJSON(data []byte) error {
	type plain Manifest
	var wire struct {
		*plain
		Shards json.RawMessage `json:"shards"`
	}
	wire.plain = (*plain)(manifest)
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	manifest.Shards = nil
	manifest.Rollup = nil
	raw := bytes.TrimSpace(wire.Shards)
	if len(raw) == 0 {
		return nil
	}
	switch raw[0] {
	case '[':
		return json.Unmarshal(raw, &manifest.Shards)
	case '{':
		manifest.Rollup = &Rollup{}
		return json.Unmarshal(raw, manifest.Rollup)
	default:
		return fmt.Errorf("manifest %q: shards must be an array or sub-manifest reference", manifest.Name)
	}
}

// MarshalJSON emits the schema-1 polymorphic shards field in the same shape
// accepted by UnmarshalJSON.
func (manifest Manifest) MarshalJSON() ([]byte, error) {
	type plain Manifest
	wire := struct {
		plain
		Shards any `json:"shards"`
	}{plain: plain(manifest)}
	if manifest.Rollup != nil {
		wire.Shards = manifest.Rollup
	} else {
		wire.Shards = manifest.Shards
	}
	return json.Marshal(wire)
}

type Source struct {
	Name          string       `json:"name"`
	Source        string       `json:"source"`
	Version       string       `json:"version,omitempty"`
	URL           string       `json:"url"`
	Category      string       `json:"category,omitempty"`
	CollectedFrom string       `json:"collected_from,omitempty"`
	CollectedTo   string       `json:"collected_to,omitempty"`
	SHA256        string       `json:"sha256"`
	Files         []SourceFile `json:"files,omitempty"`
	Usage         Modalities   `json:"usage,omitempty"`
	Content       *Content     `json:"content,omitempty"`
	Acquisition   *Acquisition `json:"acquisition,omitempty"`
}

// Modalities contains exact, additive measures keyed by a stable modality
// name such as text, image, audio, or video. A multimodal sample may contribute
// to Samples in more than one entry, so modality sample counts are not summed
// to derive a manifest's logical document count.
type Modalities map[string]ModalityMeasure

type ModalityMeasure struct {
	Samples      int64 `json:"samples,omitempty"`
	Items        int64 `json:"items,omitempty"`
	Tokens       int64 `json:"tokens,omitempty"`
	DurationMS   int64 `json:"duration_ms,omitempty"`
	ContentBytes int64 `json:"content_bytes,omitempty"`
}

// Content describes source material independently of how WALDO acquired or
// encoded it. Tri-state declarations use "yes", "no", or "unknown"; absence
// means the contributor did not make a declaration.
type Content struct {
	Types            []string `json:"types,omitempty"`
	Languages        []string `json:"languages,omitempty"`
	Geographies      []string `json:"geographies,omitempty"`
	Demographics     []string `json:"demographics,omitempty"`
	From             string   `json:"from,omitempty"`
	To               string   `json:"to,omitempty"`
	Selection        string   `json:"selection,omitempty"`
	PersonalData     string   `json:"personal_data,omitempty"`
	Copyrighted      string   `json:"copyrighted,omitempty"`
	MachineGenerated string   `json:"machine_generated,omitempty"`
}

// Acquisition records how an upstream source was obtained. Variant details
// are present only for the applicable source category.
type Acquisition struct {
	Basis     string          `json:"basis,omitempty"`
	Crawler   *Crawler        `json:"crawler,omitempty"`
	UserData  *UserData       `json:"user_data,omitempty"`
	Synthetic *SyntheticData  `json:"synthetic,omitempty"`
	Domains   []DomainMeasure `json:"domains,omitempty"`
}

type Crawler struct {
	Name      string   `json:"name"`
	Purpose   string   `json:"purpose"`
	Behaviour string   `json:"behaviour"`
	Protocols []string `json:"protocols,omitempty"`
}

type UserData struct {
	Service     string `json:"service"`
	Interaction string `json:"interaction"`
}

type SyntheticData struct {
	Model       string `json:"model"`
	Version     string `json:"version,omitempty"`
	SummaryURL  string `json:"summary_url,omitempty"`
	Description string `json:"description,omitempty"`
}

type DomainMeasure struct {
	Domain        string `json:"domain"`
	AcquiredBytes int64  `json:"acquired_bytes,omitempty"`
	RetainedBytes int64  `json:"retained_bytes,omitempty"`
}

type Processing struct {
	Steps                     []ProcessingStep `json:"steps,omitempty"`
	RightsReservationMeasures []string         `json:"rights_reservation_measures,omitempty"`
	IllegalContentMeasures    []string         `json:"illegal_content_measures,omitempty"`
}

type ProcessingStep struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type SourceFile struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes,omitempty"`
	Format     string `json:"format,omitempty"`
	Adapter    string `json:"adapter,omitempty"`
	TextColumn string `json:"text_column,omitempty"`
}

// IngestRecipeEvidence pins an explicitly supplied historical ingest recipe
// and every external command WALDO executed before probing its local artifacts.
// Arguments remain pinned by the recipe hash and are not repeated here.
type IngestRecipeEvidence struct {
	Path       string               `json:"path"`
	SHA256     string               `json:"sha256"`
	Repository string               `json:"repository,omitempty"`
	Commit     string               `json:"commit,omitempty"`
	Dirty      bool                 `json:"dirty"`
	Steps      []RecipeStepEvidence `json:"steps"`
}

type RecipeStepEvidence struct {
	Name       string `json:"name"`
	Executable string `json:"script"`
	SHA256     string `json:"sha256"`
}

type Conversion struct {
	Tool      string `json:"tool"`
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	Collector string `json:"collector,omitempty"`
	Profile   string `json:"profile"`
	Recipe    string `json:"recipe"`
	Tokenizer string `json:"tokenizer,omitempty"`
}

type Shard struct {
	URL         string      `json:"url"`
	SHA256      string      `json:"sha256"`
	Format      string      `json:"format,omitempty"`
	License     string      `json:"license,omitempty"`
	Sources     []string    `json:"sources,omitempty"`
	ConvertedBy *Conversion `json:"converted_by,omitempty"`
	Docs        int64       `json:"docs"`
	Tokens      int64       `json:"tokens"`
	Bytes       int64       `json:"bytes"`
	RecordsRoot string      `json:"records_root,omitempty"`
	Modalities  Modalities  `json:"modalities,omitempty"`
}

// Rollup describes an external submanifest tree. Its aggregate counts are
// enough for offline summaries; object-level verification belongs to the
// network-enabled verification slice.
type Rollup struct {
	URL        string     `json:"url"`
	SHA256     string     `json:"sha256"`
	Count      int64      `json:"count"`
	Docs       int64      `json:"docs"`
	Tokens     int64      `json:"tokens"`
	Bytes      int64      `json:"bytes"`
	Modalities Modalities `json:"modalities,omitempty"`
}

// SubManifest is the content-addressed overflow form of a manifest's shard
// list. Children may nest to any depth.
type SubManifest struct {
	Kind     string   `json:"kind"`
	Schema   int      `json:"schema"`
	Shards   []Shard  `json:"shards,omitempty"`
	Children []Rollup `json:"children,omitempty"`
}

type Corpus struct {
	Path     string
	Manifest Manifest
}

type Measures struct {
	Shards int64 `json:"shards"`
	Docs   int64 `json:"docs"`
	Tokens int64 `json:"tokens"`
	Bytes  int64 `json:"bytes"`
}

// Totals are exact integer aggregates. Human formatting belongs to the CLI.
type Totals struct {
	Corpora int64 `json:"corpora"`
	Measures
	Licenses map[string]Measures `json:"licenses,omitempty"`
}

func (m Manifest) EffectiveLicense(shard Shard) string {
	if shard.License != "" {
		return shard.License
	}
	return m.License
}
