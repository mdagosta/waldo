// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type Context struct {
	Execution context.Context
	JSON      bool
	Command   *cobra.Command
}

type Handler func(Context, []string, io.Writer, io.Writer) error

type cobraState struct {
	json bool
}

type flagKind uint8

const (
	boolFlag flagKind = iota
	stringFlag
	stringArrayFlag
	intFlag
	int64Flag
	uint64Flag
	float64Flag
)

type commandFlag struct {
	name         string
	usage        string
	kind         flagKind
	defaultValue any
	required     bool
}

func booleanFlag(name, usage string) commandFlag {
	return commandFlag{name: name, usage: usage, kind: boolFlag}
}

func textFlag(name, defaultValue, usage string) commandFlag {
	return commandFlag{name: name, usage: usage, kind: stringFlag, defaultValue: defaultValue}
}

func requiredTextFlag(name, usage string) commandFlag {
	return commandFlag{name: name, usage: usage, kind: stringFlag, defaultValue: "", required: true}
}

func repeatedTextFlag(name, usage string) commandFlag {
	return commandFlag{name: name, usage: usage, kind: stringArrayFlag}
}

func integerFlag(name string, defaultValue int, usage string) commandFlag {
	return commandFlag{name: name, usage: usage, kind: intFlag, defaultValue: defaultValue}
}

func integer64Flag(name string, defaultValue int64, usage string) commandFlag {
	return commandFlag{name: name, usage: usage, kind: int64Flag, defaultValue: defaultValue}
}

func unsigned64Flag(name string, defaultValue uint64, usage string) commandFlag {
	return commandFlag{name: name, usage: usage, kind: uint64Flag, defaultValue: defaultValue}
}

func decimalFlag(name string, defaultValue float64, usage string) commandFlag {
	return commandFlag{name: name, usage: usage, kind: float64Flag, defaultValue: defaultValue}
}

func newRootCommand() *cobra.Command {
	state := &cobraState{}
	root := &cobra.Command{
		Use:     "waldo",
		Short:   "Build and use auditable AI training data",
		Long:    "Build and use auditable AI training data.\n\nRelative and omitted index paths use the configured contributor checkout, or the managed read-only checkout at ~/.waldo/index when index is unset. WALDO automatically fetches and fast-forwards the selected Git checkout before use, but refuses dirty, ahead, or diverged states. `waldo index verify --offline` uses the current local revision. Absolute paths and paths beginning with ~/ explicitly select another checkout. Omitting an index selection selects the entire resolved index.",
		Version: version,
	}
	root.SetVersionTemplate("waldo {{.Version}}\n")
	root.PersistentFlags().BoolVar(&state.json, "json", false, "emit structured JSON output; progress remains on stderr")
	root.AddCommand(
		newIndexCommand(state),
		newShardCommand(state),
		newLookasideCommand(state),
		newModelCommand(state),
		newConfigCommand(state),
	)
	return root
}

func newIndexCommand(state *cobraState) *cobra.Command {
	command := group("index", "Manage indexed training data", "Index paths are positional. Relative paths resolve beneath the configured contributor checkout, or ~/.waldo/index when index is unset, including paths beginning with ./. Absolute paths and paths beginning with ~/ explicitly discover another checkout. Git checkouts are automatically fast-forwarded only when clean and behind their tracking branch. With no path, WALDO selects the entire resolved index. The managed checkout is read-only to authoring commands. Recursive commands begin at the selected path.")
	command.AddCommand(
		leaf(state, "init <directory>", "Initialize an empty index", "Creates the smallest valid schema-1 index.yaml in a new or empty directory. Readers accept existing JSON or YAML metadata, while all new WALDO index writes use YAML. The command refuses nonempty directories and the managed ~/.waldo/index path; it does not initialize Git, configure a lookaside, or create a corpus.", cobra.ExactArgs(1), runIndexInit),
		leaf(state, "pull", "Fast-forward the selected index", "Fetches and fast-forwards the configured index or managed default to its tracking branch. WALDO refuses a dirty worktree, local-only commits, divergence, detached HEAD, or missing tracking remote.", cobra.NoArgs, runIndexPull),
		leaf(state, "list [path]", "List all corpora beneath an index path", "", cobra.MaximumNArgs(1), runIndexList),
		leaf(state, "show [path]", "Show an index entry or corpus manifest", "", cobra.MaximumNArgs(1), runIndexShow),
		leaf(state, "summary [path]", "Summarize corpora, licenses, and totals", "", cobra.MaximumNArgs(1), runIndexSummary),
		leaf(state, "bom [path...]", "Emit an index selection or exported corpus OpenWALDO BOM", "Index paths resolve like other index commands. An export directory or explicitly named EXPORT.json displays its persisted BOM instead.", cobra.ArbitraryArgs, runIndexBOM),
		leaf(state, "verify [path]", "Verify an index, canonical objects, or exported corpus", "An export directory or explicitly named EXPORT.json validates the persisted BOM and hashes every exported file.\n\nIndex verification levels:\n  (default)  Recursively validate metadata, then check every canonical object URL and declared size without downloading bodies.\n  --objects  Download every referenced object, verify its SHA-256, then purge it after success.\n  --offline  Validate only local index and manifest structure; make no network requests.\n\nMirrors never hide an unavailable canonical URL during the default check.", cobra.MaximumNArgs(1), runIndexVerify,
			booleanFlag("objects", "download and hash every referenced object"),
			booleanFlag("offline", "validate only local metadata without network access")),
		leaf(state, "audit [path]", "Verify indexed shards and their ingest attestations", "Audits the exact selected checkout without fetching Git. The default streams each unique content-addressed shard through the bounded lookaside cache, verifies its embedded ingest attestation, Parquet schema and aggregate totals, then reconciles manifest totals. Unattested legacy shards fall back to a complete record scan. --deep materializes the full selection and explicitly revalidates every record hash and token count.", cobra.MaximumNArgs(1), runIndexAudit,
			integerFlag("workers", 0, "concurrent shard validators (1..32; 0 selects automatically)"),
			booleanFlag("deep", "revalidate every record hash and token count"),
			booleanFlag("show-boms", "list each verified shard BOM identity")),
		leaf(state, "ingest <input-or-recipe> <destination>", "Ingest acquired material into an index", "Arguments:\n  <input-or-recipe>  A file, recursively scanned directory, or strict waldo-ingest-recipe YAML/JSON file.\n  <destination>       New corpus path in an explicit contributor checkout. A relative path requires `waldo config set index <directory>`; an absolute or ~/ path explicitly discovers a checkout. The managed ~/.waldo/index checkout is read-only.\n\nRequired: direct input only\n  --title             Human-readable corpus title.\n  --license           License applying to this contribution.\n  --source            Acquisition/source URL recorded in provenance.\n  --source-category   GPAI-compatible source category.\n\nDirect input options:\n  --description       Corpus description; WALDO generates a default otherwise.\n  --source-name       Source label; defaults from the destination name.\n  --text-column       Raw-Parquet text column when it cannot be inferred.\n  --input-profile     Strict standalone YAML/JSON declarative input profile.\n\nUniversal option:\n  --dry-run           Direct input is probed; recipe input resolves and hashes commands but never executes them.\n\nAn ingest recipe owns all corpus metadata and rejects metadata flags. Each step uses `exec`: a bare command resolves through PATH, while a value containing a path separator is explicit and relative to the recipe file unless absolute. Commands run directly and sequentially with one WALDO-owned temporary directory as their working directory and WALDO_FETCH_DIR; no shell is implied. WALDO then uses the normal probe, conversion, publication, purge, and contribution path. Set machine locations with `waldo config set`; ingestion has no transport or scratch flags.", cobra.ExactArgs(2), runIndexIngest, ingestFlags()...),
		leaf(state, "update <input-or-recipe> <manifest>", "Append or completely rebuild an existing corpus", "Normal update audits existing shards and publishes only new records. --rebuild-shards treats the supplied input as the complete authoritative corpus.", cobra.ExactArgs(2), runIndexUpdate, append(ingestFlags(), booleanFlag("rebuild-shards", "replace the existing shard and source set"))...),
		leaf(state, "export [path...] <directory>", "Export a verified corpus selection and BOM", "Arguments:\n  [path...]           Optional corpus or index paths. Relative paths use the configured or managed index; absolute and ~/ paths explicitly discover another checkout. When omitted, WALDO exports the entire resolved index.\n  <directory>         Required local destination; always the final positional argument.\n\nOptions:\n  --format native     Preserve canonical Parquet objects (default).\n  --format jsonl      Stream verified Parquet records into JSONL.\n  --license           Include matching license identifiers.\n  --exclude-license   Exclude matching license identifiers.\n  --force             Replace conflicting export files; matching files resume safely.\n\nThe export contains data files and EXPORT.json with its OpenWALDO BOM. Downloaded scratch objects are purged only after both are complete.", cobra.MinimumNArgs(1), runIndexExport,
			repeatedTextFlag("license", "include matching license identifiers (repeatable or comma-separated)"),
			repeatedTextFlag("exclude-license", "exclude matching license identifiers (repeatable or comma-separated)"),
			booleanFlag("force", "replace conflicting export files"),
			textFlag("format", "native", "export format: native or jsonl")),
	)
	return command
}

func ingestFlags() []commandFlag {
	return []commandFlag{
		booleanFlag("dry-run", "probe and plan without writing"),
		textFlag("title", "", "human-readable corpus title"),
		textFlag("description", "", "corpus description"),
		textFlag("license", "", "license applying to the contribution"),
		textFlag("source", "", "acquisition or source URL"),
		textFlag("source-name", "", "source label"),
		textFlag("source-category", "", "GPAI-compatible source category"),
		textFlag("text-column", "", "raw Parquet text column"),
		textFlag("input-profile", "", "declarative input profile path"),
	}
}

func newShardCommand(state *cobraState) *cobra.Command {
	command := group("shard", "Inspect and audit local canonical Parquet shards", "Shard commands operate directly on local Parquet files.")
	command.AddCommand(
		leaf(state, "summary <path...>", "Aggregate shard footer and record totals", "", cobra.MinimumNArgs(1), runShardSummary),
		leaf(state, "bom <path...>", "Show embedded shard OpenWALDO BOMs", "Shows embedded builder BOMs and their independent SHA-256 identities. Writer-v4 shards report implicit-v4 because they predate embedded BOM metadata.", cobra.MinimumNArgs(1), runShardBOM),
		leaf(state, "audit <path...>", "Validate every record in one or more shards", "", cobra.MinimumNArgs(1), runShardAudit,
			integerFlag("workers", 0, "concurrent shard validators (1..32; 0 selects automatically)")),
		leaf(state, "list-records <shard-file>", "List compact record summaries from one shard", "", cobra.ExactArgs(1), runShardListRecords),
		leaf(state, "export-record <shard-file> <record-id>", "Write one record's text to standard output", "", cobra.ExactArgs(2), runShardExportRecord),
	)
	return command
}

func newLookasideCommand(state *cobraState) *cobra.Command {
	command := group("lookaside", "Inspect and maintain content-addressed objects", "")
	command.AddCommand(
		leaf(state, "login", "Verify and store S3 credentials in ~/.waldo", "Requires a configured s3:// lookaside. Prompts interactively for an S3 access key and hidden secret key. WALDO writes, lists, inspects, reads, and deletes a tiny probe object beneath the configured prefix before storing bucket-scoped credentials in ~/.waldo/credentials with mode 0600. Credentials are never written to WALDO configuration, output, manifests, or command history.", cobra.NoArgs, runLookasideLogin),
		leaf(state, "logout", "Remove stored S3 credentials", "", cobra.NoArgs, runLookasideLogout),
		leaf(state, "list [index-path]", "List lookaside objects and optional index references", "Inventories the configured lookaside without downloading object bodies.", cobra.MaximumNArgs(1), runLookasideList,
			booleanFlag("all", "include objects not referenced by the selected index")),
		leaf(state, "status", "Show retained cache, download scratch, and storage", "", cobra.NoArgs, runLookasideStatus),
		leaf(state, "verify", "Scrub leftover objects against their hashes", "", cobra.NoArgs, runLookasideVerify),
		unavailable("mirror", "Copy verified objects to another lookaside"),
		leaf(state, "rm <sha256>...", "Remove explicitly named lookaside objects", "Every object name must be a complete 64-character lowercase SHA-256.", cobra.MinimumNArgs(1), runLookasideRemove),
	)
	return command
}

func newModelCommand(state *cobraState) *cobra.Command {
	command := group("model", "Create, train, compose, and use auditable models", "")
	command.AddCommand(
		leaf(state, "init <name>", "Initialize an untrained model", "Available presets: 10m, 35m, 90m, 300m, 1b, 3b, 7b, 13b, 34b, 70b.", cobra.ExactArgs(1), runModelInit,
			requiredTextFlag("preset", "model architecture preset")),
		leaf(state, "pull <name> <huggingface-source>", "Pull training-quality open weights", "Pulls and validates a Hugging Face Safetensors model into WALDO's managed model store.", cobra.ExactArgs(2), runModelPull),
		leaf(state, "list [pattern...]", "List locally managed models", "Patterns use shell-style *, ?, and character classes.", cobra.ArbitraryArgs, runModelList),
		leaf(state, "summary <name>", "Summarize architecture and training history", "", cobra.ExactArgs(1), runModelSummary),
		leaf(state, "bom <name> [output.json]", "Emit a model OpenWALDO BOM", "Writes canonical JSON to stdout when output is omitted.", cobra.RangeArgs(1, 2), runModelBOM),
		leaf(state, "disclose <model-name-or-path> [output.json]", "Map a model BOM to a disclosure format", "Normal disclosure generation fails before emitting anything if required facts are absent.", cobra.RangeArgs(1, 2), runModelDisclosure,
			requiredTextFlag("format", "disclosure format: eu-gpai"),
			textFlag("provider", "", "provider profile override"),
			booleanFlag("allow-incomplete", "emit a marked incomplete draft"),
			booleanFlag("force", "replace an existing output file")),
		leaf(state, "forecast [index-path...] | <compose.yaml>", "Estimate viable GPU configurations and runtime", "Forecasts one pass over selected index tokens or a declared model compose.", cobra.ArbitraryArgs, runModelForecast),
		leaf(state, "train <name> [index-path...]", "Train an existing model on indexed corpora", "With no index path, WALDO trains on the entire resolved index.", modelTrainArgs, runModelTrain,
			integer64Flag("epochs", 1, "complete training passes (1..1000000)")),
		leaf(state, "compose <name> <compose-file>", "Create and train from a model compose", "Preflights every stage before publishing the model.", cobra.ExactArgs(2), runModelCompose,
			booleanFlag("replace", "replace an existing model after all stages complete")),
		leaf(state, "export <name> <directory>", "Export a model release package", "Exports WALDO, Hugging Face, MLX, GGUF, or Ollama artifacts with provenance.", cobra.ExactArgs(2), runModelExport,
			textFlag("format", "waldo", "export format: waldo, huggingface, mlx, gguf, or ollama"),
			textFlag("quant", "", "GGUF quantization profile: 2, 3, 4, 5, 6, or 8"),
			textFlag("calibration", "", "WALDO index path for quantization calibration"),
			booleanFlag("allow-incomplete", "allow a marked incomplete disclosure draft")),
		leaf(state, "chat <name> [prompt]", "Generate with a trained local model", "With no prompt in a terminal, opens an interactive session.", cobra.RangeArgs(1, 2), runModelChat,
			integerFlag("max-tokens", 256, "maximum generated tokens"),
			decimalFlag("temperature", 0.8, "sampling temperature"),
			decimalFlag("top-p", 0.95, "nucleus sampling probability"),
			unsigned64Flag("seed", 0, "deterministic sampling seed")),
		leaf(state, "rm <name...>", "Remove explicitly named local models", "Names are preflighted before removal.", cobra.MinimumNArgs(1), runModelRemove),
	)
	return command
}

func newConfigCommand(state *cobraState) *cobra.Command {
	command := group("config", "Inspect and change machine-local preferences", "")
	command.AddCommand(
		leaf(state, "show", "Show effective configuration", "", cobra.NoArgs, runConfigShow),
		leaf(state, "get [key-or-prefix]", "Discover configuration keys and values", "With no argument, lists every supported configuration key.", cobra.MaximumNArgs(1), runConfigGet),
		leaf(state, "set <key> <value...>", "Set one configuration value", "Keys:\n  index                 Contributor override; unset uses managed ~/.waldo/index.\n  lookaside             Writable s3:// or file:// lookaside URL.\n  lookaside.region      AWS region when it cannot be inferred.\n  lookaside.workers     Concurrent completed-shard uploads (1..32).\n  lookaside.mirrors     Ordered fallback read URLs; values replace the list.\n  lookaside.cache       Retained verified objects; default ~/.waldo/cache.\n  lookaside.cache.max-size  Retention bound; default 20GiB.\n  lookaside.scratch     Partial downloads; default beneath system temp.\n  ingest.staging        Ingestion recovery state; default beneath system temp.\n  model.root            Durable model artifacts; default ~/.waldo/models.\n  model.backend         auto (default), mlx, torchtitan, pytorch, or fake.\n  disclosure.provider  Strict provider-level disclosure JSON file.\n  signing.method       sigstore-keyless or sigstore-key.\n  signing.key          Private-key file used by sigstore-key.\n\nExamples:\n  waldo config set index /path/to/waldo-index\n  waldo config set lookaside file:///tmp/waldo-published\n  waldo config set lookaside s3://bucket/prefix\n  waldo config set lookaside.cache /fast-disk/waldo-cache\n  waldo config set lookaside.cache.max-size 100GiB\n  waldo config set disclosure.provider ./provider.json\n  waldo config set signing.method sigstore-keyless\n\nUse `waldo lookaside login` to store S3 keys in ~/.waldo/credentials. Environment and workload-role credentials remain available when no WALDO login exists.", cobra.MinimumNArgs(2), runConfigSet),
		leaf(state, "unset <key>", "Return one configuration value to its default", "", cobra.ExactArgs(1), runConfigUnset),
	)
	return command
}

func group(use, short, long string) *cobra.Command {
	command := &cobra.Command{
		Use: use, Short: short, Long: long, Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	return command
}

func unavailable(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return fmt.Errorf("%s is not available yet", command.CommandPath())
		},
	}
}

func modelTrainArgs(command *cobra.Command, args []string) error {
	if err := cobra.MinimumNArgs(1)(command, args); err != nil {
		return err
	}
	if len(args) == 1 && looksLikeIndexPath(args[0]) {
		return fmt.Errorf("model name is required before index path %q", args[0])
	}
	return nil
}

func leaf(state *cobraState, use, short, long string, args cobra.PositionalArgs, handler Handler, flags ...commandFlag) *cobra.Command {
	command := &cobra.Command{Use: use, Short: short, Long: long, Args: args}
	for index := range flags {
		registerFlag(command, &flags[index])
	}
	command.RunE = func(command *cobra.Command, positional []string) error {
		return handler(Context{Execution: command.Context(), JSON: state.json, Command: command}, positional, command.OutOrStdout(), command.ErrOrStderr())
	}
	return command
}

func registerFlag(command *cobra.Command, flag *commandFlag) {
	switch flag.kind {
	case boolFlag:
		command.Flags().Bool(flag.name, false, flag.usage)
	case stringFlag:
		command.Flags().String(flag.name, flag.defaultValue.(string), flag.usage)
	case stringArrayFlag:
		command.Flags().StringArray(flag.name, nil, flag.usage)
	case intFlag:
		command.Flags().Int(flag.name, flag.defaultValue.(int), flag.usage)
	case int64Flag:
		command.Flags().Int64(flag.name, flag.defaultValue.(int64), flag.usage)
	case uint64Flag:
		command.Flags().Uint64(flag.name, flag.defaultValue.(uint64), flag.usage)
	case float64Flag:
		command.Flags().Float64(flag.name, flag.defaultValue.(float64), flag.usage)
	}
	if flag.required {
		_ = command.MarkFlagRequired(flag.name)
	}
}

func optionChanged(context Context, name string) bool {
	return context.Command != nil && context.Command.Flags().Changed(name)
}

func boolOption(context Context, name string) bool {
	value, _ := context.Command.Flags().GetBool(name)
	return value
}

func stringOption(context Context, name string) string {
	value, _ := context.Command.Flags().GetString(name)
	return value
}

func stringArrayOption(context Context, name string) []string {
	value, _ := context.Command.Flags().GetStringArray(name)
	return value
}

func intOption(context Context, name string) int {
	value, _ := context.Command.Flags().GetInt(name)
	return value
}

func int64Option(context Context, name string) int64 {
	value, _ := context.Command.Flags().GetInt64(name)
	return value
}

func uint64Option(context Context, name string) uint64 {
	value, _ := context.Command.Flags().GetUint64(name)
	return value
}

func float64Option(context Context, name string) float64 {
	value, _ := context.Command.Flags().GetFloat64(name)
	return value
}
