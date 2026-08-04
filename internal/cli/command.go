package cli

import (
	"context"
	"io"
)

type Context struct {
	Execution context.Context
	IndexPath string
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
		Details: "Global options:\n  --index <checkout>  Use a specific index checkout.\n  --json              Emit structured command output; progress remains on stderr.\n  --help              Show command help.\n  --version           Show the WALDO version.",
		Children: []Command{
			{Name: "index", Summary: "Manage indexed training data", Children: []Command{
				{Name: "init", Summary: "Initialize an index checkout"},
				{Name: "list", Summary: "List all corpora beneath an index path", Usage: "waldo index list [path] [--index <checkout>] [--json]", Handler: runIndexList},
				{Name: "show", Summary: "Show an index entry or corpus manifest", Usage: "waldo index show [path] [--index <checkout>] [--json]", Handler: runIndexShow},
				{Name: "summary", Summary: "Summarize corpora, licenses, and totals", Usage: "waldo index summary [path] [--index <checkout>] [--json]", Handler: runIndexSummary},
				{Name: "verify", Summary: "Verify index structure and optionally its objects", Usage: "waldo index verify [path] [--objects] [--index <checkout>] [--json]", Details: "Options:\n  --objects  Download each referenced object into scratch, verify it, then purge it after success.\n  path       Limit verification to one index subtree; defaults to the index root.", Handler: runIndexVerify},
				{Name: "ingest", Summary: "Ingest acquired material into an index", Usage: "waldo index ingest <input> <destination> --title <title> --license <id> --source <url> --source-category <category> [--description <text>] [--source-name <name>] [--text-column <column>] [--staging <directory>] [--dry-run] [--json]", Details: "Required:\n  <input>             File or recursively scanned directory.\n  <destination>       New corpus path inside the index.\n  --title             Human-readable corpus title.\n  --license           License applying to this contribution.\n  --source            Acquisition/source URL recorded in provenance.\n  --source-category   GPAI-compatible source category.\n\nOptional:\n  --description       Corpus description; WALDO generates a default otherwise.\n  --source-name       Source label; defaults from the destination name.\n  --text-column       Raw-Parquet text column when it cannot be inferred.\n  --staging           Scratch and recovery directory; normally automatic.\n  --dry-run           Probe and print the immutable plan without writing.\n\nA writable lookaside must first be set with `waldo lookaside configure`.", Handler: runIndexIngest},
				{Name: "update", Summary: "Append new material to an existing corpus"},
				{Name: "export", Summary: "Export a verified corpus selection and BOM", Usage: "waldo index export <path...> --output <directory> [--format native|jsonl] [--license <glob,...>] [--exclude-license <glob,...>] [--force] [--index <checkout>] [--json]", Details: "Options:\n  --output            Required destination directory.\n  --format native     Preserve canonical Parquet objects (default).\n  --format jsonl      Stream verified Parquet records into JSONL.\n  --license           Include matching license identifiers.\n  --exclude-license   Exclude matching license identifiers.\n  --force             Replace conflicting export files; matching files resume safely.\n\nDownloaded scratch objects are purged only after the export and its OpenWALDO BOM succeed.", Handler: runIndexExport},
				{Name: "remove", Summary: "Remove a corpus from the current index revision"},
			}},
			{Name: "lookaside", Summary: "Configure and maintain content-addressed objects", Children: []Command{
				{Name: "configure", Summary: "Configure scratch, read mirrors, and publication", Usage: "waldo lookaside configure [--scratch <directory> | --default-scratch] [--mirror <base-url>] [--remove-mirror <base-url>] [--clear-mirrors] [--publish <s3-url> | --publish-local <directory>] [--publish-region <region>] [--upload-workers <1..32>] [--clear-publish] [--json]", Details: "Options:\n  --scratch           Set verified-download scratch space.\n  --default-scratch   Restore the operating-system scratch default.\n  --mirror            Add a fallback read location.\n  --remove-mirror     Remove one fallback read location.\n  --clear-mirrors     Remove every fallback read location.\n  --publish           Set the writable production S3 lookaside.\n  --publish-local     Set a test-only filesystem publisher.\n  --publish-region    Pin AWS region when it cannot be inferred.\n  --upload-workers    Bound concurrent completed-shard uploads (default 4).\n  --clear-publish     Remove writable-lookaside configuration.\n\nCredentials come from the standard AWS credential chain and are never saved by WALDO.", Handler: runLookasideConfigure},
				{Name: "login", Summary: "Save credentials for lookaside writes"},
				{Name: "status", Summary: "Show verified-download scratch and leftovers", Usage: "waldo lookaside status [--json]", Handler: runLookasideStatus},
				{Name: "verify", Summary: "Scrub leftover objects against their hashes", Usage: "waldo lookaside verify [--json]", Handler: runLookasideVerify},
				{Name: "mirror", Summary: "Copy verified objects to another lookaside"},
				{Name: "gc", Summary: "Safely reclaim unreferenced lookaside objects"},
			}},
			{Name: "model", Summary: "Build and inspect provenance-carrying models", Children: []Command{
				{Name: "create", Summary: "Create an untrained model"},
				{Name: "build", Summary: "Build a model from a declarative recipe"},
				{Name: "train", Summary: "Add an explicit training run"},
				{Name: "inspect", Summary: "Inspect architecture, runs, and lineage"},
				{Name: "test", Summary: "Evaluate a model on a corpus selection"},
				{Name: "chat", Summary: "Generate interactively with a local model"},
				{Name: "fork", Summary: "Create a model with inherited lineage"},
				{Name: "export", Summary: "Export weights with model provenance"},
				{Name: "remove", Summary: "Remove a local model artifact"},
			}},
			{Name: "bom", Summary: "Inspect and exchange provenance records", Children: []Command{
				{Name: "show", Summary: "Show an exported OpenWALDO BOM", Usage: "waldo bom show <export-directory|EXPORT.json> [--json]", Handler: runBOMShow},
				{Name: "verify", Summary: "Validate a BOM and hash its exported files", Usage: "waldo bom verify <export-directory|EXPORT.json> [--json]", Handler: runBOMVerify},
				{Name: "export", Summary: "Export a BOM in a supported interchange format"},
			}},
			{Name: "config", Summary: "Inspect and change machine-local preferences", Children: []Command{
				{Name: "show", Summary: "Show effective configuration"},
				{Name: "get", Summary: "Read one configuration value"},
				{Name: "set", Summary: "Set one configuration value"},
			}},
		},
	}
}
