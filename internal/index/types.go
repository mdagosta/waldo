// Package index reads and verifies WALDO's Git metadata tree.
package index

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
	Kind        string     `json:"kind"`
	Schema      int        `json:"schema"`
	Name        string     `json:"name"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	License     string     `json:"license"`
	Format      string     `json:"format,omitempty"`
	Sources     []Source   `json:"sources"`
	ConvertedBy Conversion `json:"converted_by"`
	Shards      []Shard    `json:"shards,omitempty"`
	Rollup      *Rollup    `json:"rollup,omitempty"`
}

type Source struct {
	Name          string `json:"name"`
	Source        string `json:"source"`
	Version       string `json:"version,omitempty"`
	URL           string `json:"url"`
	Category      string `json:"category,omitempty"`
	CollectedFrom string `json:"collected_from,omitempty"`
	CollectedTo   string `json:"collected_to,omitempty"`
	SHA256        string `json:"sha256"`
}

type Conversion struct {
	Tool      string `json:"tool"`
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	Collector string `json:"collector,omitempty"`
	Profile   string `json:"profile"`
	Recipe    string `json:"recipe"`
	Tokenizer string `json:"tokenizer"`
}

type Shard struct {
	URL         string     `json:"url"`
	SHA256      string     `json:"sha256"`
	Format      string     `json:"format,omitempty"`
	License     string     `json:"license,omitempty"`
	Sources     []string   `json:"sources,omitempty"`
	ConvertedBy Conversion `json:"converted_by,omitempty"`
	Docs        int64      `json:"docs"`
	Tokens      int64      `json:"tokens"`
	Bytes       int64      `json:"bytes"`
}

// Rollup describes an external submanifest tree. Its aggregate counts are
// enough for offline summaries; object-level verification belongs to the
// network-enabled verification slice.
type Rollup struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Shards int64  `json:"shards"`
	Docs   int64  `json:"docs"`
	Tokens int64  `json:"tokens"`
	Bytes  int64  `json:"bytes"`
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
