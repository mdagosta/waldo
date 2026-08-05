package cli

import (
	"context"
	"io"
)

type Context struct {
	Execution context.Context
	JSON      bool
}

type Handler func(Context, []string, io.Writer, io.Writer) error

// Command describes one stable part of the WALDO command vocabulary. Phase 0
// intentionally contains no operational handlers: the command tree is a UX
// contract that implementation slices will fill in one domain at a time.
type Command struct {
	Name     string
	Summary  string
	Usage    string
	Details  string
	Children []Command
	Handler  Handler
}

func commandTree() Command {
	return Command{
		Name:    "waldo",
		Summary: "Build and use auditable AI training data",
		Details: "Global options:\n  --json     Emit structured command output; progress remains on stderr.\n  --help     Show command help.\n  --version  Show the WALDO version.\n\nIndex commands discover the checkout from their positional path, or from the current directory when no path is given.",
		Children: []Command{
			{Name: "index", Summary: "Manage indexed training data", Details: "Index paths are positional. Existing checkout, subtree, corpus-directory, and manifest paths discover their enclosing checkout automatically. With no path, WALDO discovers from the current directory. Recursive commands begin at the selected path.", Children: []Command{
				{Name: "init", Summary: "Initialize an empty index", Usage: "waldo index init <directory> [--json]", Details: "Creates the smallest valid schema-2 index in a new or empty directory. It refuses nonempty directories and does not initialize Git, configure a lookaside, or create a corpus.", Handler: runIndexInit},
				{Name: "list", Summary: "List all corpora beneath an index path", Usage: "waldo index list [path] [--json]", Handler: runIndexList},
				{Name: "show", Summary: "Show an index entry or corpus manifest", Usage: "waldo index show [path] [--json]", Handler: runIndexShow},
				{Name: "summary", Summary: "Summarize corpora, licenses, and totals", Usage: "waldo index summary [path] [--json]", Handler: runIndexSummary},
				{Name: "verify", Summary: "Verify an index and its canonical object locations", Usage: "waldo index verify [path] [--offline|--objects] [--json]", Details: "Verification levels:\n  (default)  Recursively validate metadata, then check every canonical object URL and declared size without downloading bodies.\n  --objects  Download every referenced object, verify its SHA-256, then purge it after success.\n  --offline  Validate only local index and manifest structure; make no network requests.\n\n  path       Existing checkout, index directory, corpus directory, or manifest. WALDO discovers the checkout from this path and recurses beneath it. When omitted, discovery starts at the current directory.\n\nMirrors never hide an unavailable canonical URL during the default check.", Handler: runIndexVerify},
				{Name: "ingest", Summary: "Ingest acquired material into an index", Usage: "waldo index ingest <input> <destination> --title <title> --license <id> --source <url> --source-category <category> [--description <text>] [--source-name <name>] [--text-column <column>] [--dry-run] [--json]", Details: "Required:\n  <input>             File or recursively scanned directory.\n  <destination>       New corpus path. An absolute or ./ path discovers its checkout; a logical path uses the checkout containing the current directory.\n  --title             Human-readable corpus title.\n  --license           License applying to this contribution.\n  --source            Acquisition/source URL recorded in provenance.\n  --source-category   GPAI-compatible source category.\n\nOptional:\n  --description       Corpus description; WALDO generates a default otherwise.\n  --source-name       Source label; defaults from the destination name.\n  --text-column       Raw-Parquet text column when it cannot be inferred.\n  --dry-run           Probe and print the immutable plan without writing.\n\nSet machine locations with `waldo config set`; ingestion has no transport or scratch flags.", Handler: runIndexIngest},
				{Name: "update", Summary: "Append new material to an existing corpus"},
				{Name: "export", Summary: "Export a verified corpus selection and BOM", Usage: "waldo index export <path...> <directory> [--format native|jsonl] [--license <glob,...>] [--exclude-license <glob,...>] [--force] [--json]", Details: "Arguments:\n  <path...>           One or more corpus or index paths. The first discovers the checkout; later logical paths use that same checkout.\n  <directory>         Required local destination; always the final positional argument.\n\nOptions:\n  --format native     Preserve canonical Parquet objects (default).\n  --format jsonl      Stream verified Parquet records into JSONL.\n  --license           Include matching license identifiers.\n  --exclude-license   Exclude matching license identifiers.\n  --force             Replace conflicting export files; matching files resume safely.\n\nThe export contains data files and EXPORT.json with its OpenWALDO BOM. Downloaded scratch objects are purged only after both are complete.", Handler: runIndexExport},
				{Name: "remove", Summary: "Remove a corpus from the current index revision"},
			}},
			{Name: "lookaside", Summary: "Inspect and maintain content-addressed objects", Children: []Command{
				{Name: "login", Summary: "Verify and store S3 credentials in the OS keychain", Usage: "waldo lookaside login [--json]", Details: "Requires a configured s3:// lookaside. Prompts interactively for an S3 access key and hidden secret key. WALDO writes, lists, inspects, reads, and deletes a tiny probe object beneath the configured prefix before storing the credentials in the operating system credential vault, scoped to the bucket. Credentials are never written to WALDO configuration, output, manifests, or command history.", Handler: runLookasideLogin},
				{Name: "logout", Summary: "Remove stored S3 credentials", Usage: "waldo lookaside logout [--json]", Handler: runLookasideLogout},
				{Name: "list", Summary: "List lookaside objects and optional index references", Usage: "waldo lookaside list [index-path] [--all] [--json]", Details: "For S3, inventories the configured lookaside's entire bucket without downloading object bodies. A file:// lookaside inventories its configured root. With an index path, only matching object hashes are shown; --all includes unmatched objects with `--` in the INDEX column. Human output is only a compact header and rows: a 16-character object prefix, size, immutable storage timestamp, storage prefix, and index reference. JSON retains complete hashes, paths, timestamps, references, inventory context, and totals. Unmatched objects are not necessarily safe to remove; another index or BOM may still reference them.", Handler: runLookasideList},
				{Name: "status", Summary: "Show verified-download scratch and leftovers", Usage: "waldo lookaside status [--json]", Handler: runLookasideStatus},
				{Name: "verify", Summary: "Scrub leftover objects against their hashes", Usage: "waldo lookaside verify [--json]", Handler: runLookasideVerify},
				{Name: "mirror", Summary: "Copy verified objects to another lookaside"},
				{Name: "rm", Summary: "Remove explicitly named lookaside objects", Usage: "waldo lookaside rm <sha256>... [--json]", Details: "Every object name must be a complete 64-character lowercase SHA-256. WALDO preflights the entire list against the configured writable lookaside before deleting anything. URLs, prefixes, globs, and reachability-based garbage collection are intentionally unsupported.", Handler: runLookasideRemove},
			}},
			{Name: "model", Summary: "Build and inspect provenance-carrying models", Children: []Command{
				{Name: "create", Summary: "Create an untrained model"},
				{Name: "build", Summary: "Build a model from a declarative recipe", Usage: "waldo model build <recipe.yaml> [--json]", Details: "Preflights the complete recipe and exported corpus files, creates an immutable model plan, and executes its ordered stages. Phase 4 enables only the deterministic fake backend; real training backends come later. Existing models are never continued or replaced implicitly.", Handler: runModelBuild},
				{Name: "train", Summary: "Add an explicit training run"},
				{Name: "inspect", Summary: "Inspect architecture, runs, and lineage", Usage: "waldo model inspect <name-or-path> [--json]", Handler: runModelInspect},
				{Name: "test", Summary: "Evaluate a model on a corpus selection"},
				{Name: "chat", Summary: "Generate interactively with a local model"},
				{Name: "fork", Summary: "Create a model with inherited lineage"},
				{Name: "export", Summary: "Export weights with model provenance"},
				{Name: "remove", Summary: "Remove a local model artifact"},
			}},
			{Name: "bom", Summary: "Inspect and exchange provenance records", Children: []Command{
				{Name: "show", Summary: "Show an exported OpenWALDO BOM", Usage: "waldo bom show <export-directory|EXPORT.json> [--json]", Handler: runBOMShow},
				{Name: "verify", Summary: "Validate a BOM and hash its exported files", Usage: "waldo bom verify <export-directory|EXPORT.json> [--json]", Handler: runBOMVerify},
				{Name: "export", Summary: "Map a model BOM to a disclosure format", Usage: "waldo bom export <model-name-or-path> [output.json] --format eu-gpai [--provider <profile.json>] [--allow-incomplete] [--force] [--json]", Details: "Arguments:\n  <model-name-or-path>  Verified local model name or directory.\n  [output.json]        Optional destination. When omitted, the converted document is written to stdout.\n\nOptions:\n  --format eu-gpai     Pin and map the supported Commission template version.\n  --provider           Strict schema-1 provider and model-release profile.\n  --allow-incomplete   Emit a conspicuously marked draft when required facts are missing.\n  --force              Replace an existing output file; invalid when writing to stdout.\n\nNormal export fails before emitting anything if any required disclosure fact is absent. WALDO reports provenance gaps; it does not make a legal compliance finding or yet render the official editable document.", Handler: runBOMExport},
			}},
			{Name: "config", Summary: "Inspect and change machine-local preferences", Children: []Command{
				{Name: "show", Summary: "Show effective configuration", Usage: "waldo config show [--json]", Handler: runConfigShow},
				{Name: "get", Summary: "Discover configuration keys and values", Usage: "waldo config get [key-or-prefix] [--json]", Details: "With no argument, lists every supported configuration key. A prefix such as `lookaside` lists every matching key. An exact leaf such as `lookaside.region` prints only its value. Unset values remain visible.\n\nExamples:\n  waldo config get\n  waldo config get lookaside\n  waldo config get lookaside.region", Handler: runConfigGet},
				{Name: "set", Summary: "Set one configuration value", Usage: "waldo config set <key> <value...> [--json]", Details: "Keys:\n  lookaside             Writable s3:// or file:// lookaside URL.\n  lookaside.region      AWS region when it cannot be inferred.\n  lookaside.workers     Concurrent completed-shard uploads (1..32).\n  lookaside.mirrors     Ordered fallback read URLs; values replace the list.\n  lookaside.scratch     Verified-download scratch directory.\n  ingest.staging        Ingestion scratch and recovery parent directory.\n  model.root            Durable local model, run, BOM, and artifact directory.\n\nExamples:\n  waldo config set lookaside file:///tmp/waldo-published\n  waldo config set lookaside s3://bucket/prefix\n  waldo config set lookaside.workers 4\n  waldo config set model.root /fast-disk/waldo-models\n\nUse `waldo lookaside login` to store S3 keys in the OS credential vault. Environment and workload-role credentials remain available as a headless fallback.", Handler: runConfigSet},
				{Name: "unset", Summary: "Return one configuration value to its default", Usage: "waldo config unset <key> [--json]", Handler: runConfigUnset},
			}},
		},
	}
}
