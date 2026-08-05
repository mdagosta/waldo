package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	"github.com/openwaldo/waldo-new/internal/config"
	waldoindex "github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/ingest"
	"github.com/openwaldo/waldo-new/internal/lookaside"
)

var newIngestPublisher = func(ctx context.Context, publish config.Publish) (lookaside.Publisher, error) {
	return lookaside.NewPublisher(ctx, publish)
}

var ingestComposeRunner ingest.CommandRunner = ingest.ExecCommandRunner{}

func runIndexIngest(context Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseIndexIngest(args)
	if err != nil {
		return err
	}
	loadedCompose, composed, err := ingest.LoadCompose(options.Inputs[0])
	if err != nil {
		return err
	}
	if composed {
		if len(options.MetadataOptions) > 0 {
			return usageError{message: fmt.Sprintf("compose input owns corpus metadata; remove %s", strings.Join(options.MetadataOptions, ", "))}
		}
		options.Request.Title = loadedCompose.Compose.Title
		options.Request.Description = loadedCompose.Compose.Description
		options.Request.License = loadedCompose.Compose.License
		options.Request.Source = ingest.PlanSource{
			Name: loadedCompose.Compose.Source.Name, URL: loadedCompose.Compose.Source.URL, Category: loadedCompose.Compose.Source.Category,
		}
		options.Request.TextColumn = loadedCompose.Compose.TextColumn
	} else if options.Request.Title == "" || options.Request.License == "" || options.Request.Source.URL == "" || options.Request.Source.Category == "" {
		return usageError{message: "direct index ingest requires --title, --license, --source, and --source-category"}
	}
	target, err := waldoindex.ResolveDestination(options.Request.Destination)
	if err != nil {
		return err
	}
	options.Request.Destination = target.Rel
	if options.Request.Source.Name == "" {
		options.Request.Source.Name = path.Base(strings.TrimSuffix(target.Rel, "/"))
	}
	if composed && options.DryRun {
		return writeComposePreflight(context, stdout, loadedCompose, target.Rel)
	}
	var configuration config.Config
	if !options.DryRun {
		configuration, err = config.Load()
		if err != nil {
			return err
		}
		if configuration.Lookaside.Publish == nil {
			return usageError{message: "index ingest needs a writable lookaside; run `waldo config set lookaside <s3-or-file-URL>`"}
		}
	}
	execution := ingest.WithProgress(context.Execution, ingestProgressReporter(stderr, context.JSON))
	var probe ingest.Probe
	var prepared *ingest.PreparedCompose
	if composed {
		if err := ingest.CheckContributionDestinationPath(target.Root, target.Rel); err != nil {
			return err
		}
		stagingBase, err := config.EffectiveStagingBase(configuration)
		if err != nil {
			return err
		}
		scratchRoot, err := config.EffectiveScratchRoot(configuration)
		if err != nil {
			return err
		}
		if err := ingest.ValidateWorkLocations(target.Root, stagingBase, scratchRoot); err != nil {
			return err
		}
		composeOutput := io.Writer(stderr)
		if context.JSON {
			composeOutput = &composeJSONLogWriter{output: stderr}
		}
		result, err := ingest.PrepareCompose(execution, loadedCompose, target.Rel, stagingBase, ingestComposeRunner, composeOutput, composeOutput)
		if err != nil {
			return err
		}
		prepared = &result
		probe = result.Probe
		composition := result.Loaded.Evidence
		options.Request.Composition = &composition
		options.Request.InputRoot = result.Inputs
	} else {
		probe, err = ingest.ProbePaths(execution, options.Inputs)
		if err != nil {
			return err
		}
	}
	plan, err := ingest.NewPlan(probe, options.Request)
	if err != nil {
		return err
	}
	identity, err := plan.Identity()
	if err != nil {
		return err
	}
	if options.DryRun && context.JSON {
		return writeJSON(stdout, struct {
			Identity string      `json:"identity"`
			Plan     ingest.Plan `json:"plan"`
		}{Identity: identity, Plan: plan})
	}
	if !options.DryRun {
		if err := ingest.CheckContributionDestination(target.Root, plan); err != nil {
			return err
		}
		staging, err := config.EffectiveStagingRoot(configuration, identity)
		if err != nil {
			return err
		}
		scratchRoot, err := config.EffectiveScratchRoot(configuration)
		if err != nil {
			return err
		}
		if err := ingest.ValidateWorkLocations(target.Root, staging, scratchRoot); err != nil {
			return err
		}
		publish := configuration.Lookaside.Publish
		publisher, err := newIngestPublisher(execution, *publish)
		if err != nil {
			return err
		}
		assembly, publication, err := ingest.ExecutePublication(execution, plan, staging, publisher, publish.Workers)
		if err != nil {
			return err
		}
		manifest, err := ingest.BuildManifest(plan, assembly, publication.BaseURL)
		if err != nil {
			return err
		}
		contribution, err := ingest.StageContribution(target.Root, staging, plan, manifest)
		if err != nil {
			return err
		}
		if prepared != nil {
			if err := ingest.PurgePreparedCompose(*prepared); err != nil {
				return err
			}
		}
		if context.JSON {
			return writeJSON(stdout, struct {
				Identity     string                    `json:"identity"`
				Plan         ingest.Plan               `json:"plan"`
				Assembly     ingest.AssemblyResult     `json:"assembly"`
				Publication  ingest.PublicationResult  `json:"publication"`
				Contribution ingest.ContributionResult `json:"contribution"`
			}{identity, plan, assembly, publication, contribution})
		}
		fmt.Fprintf(stdout, "ingestion %s complete\n", identity[:12])
		fmt.Fprintf(stdout, "  records      %s input, %s retained, %s duplicate\n", humanInteger(assembly.InputDocs), humanInteger(assembly.RetainedDocs), humanInteger(assembly.DuplicateDocs))
		var tokens int64
		for _, object := range assembly.Objects {
			tokens += object.Tokens
		}
		fmt.Fprintf(stdout, "  tokens       %s (%s)\n", humanCount(tokens), manifest.ConvertedBy.Tokenizer)
		fmt.Fprintf(stdout, "  objects      %s published to %s\n", humanInteger(int64(len(publication.Objects))), publication.BaseURL)
		fmt.Fprintf(stdout, "  contribution %s (%s changed files)\n", contribution.Root, humanInteger(int64(len(contribution.Files))))
		for _, file := range contribution.Files {
			fmt.Fprintf(stdout, "    %s\n", file)
		}
		if strings.HasPrefix(publication.BaseURL, "file://") {
			fmt.Fprintln(stdout, "local publication is for end-to-end testing only; do not commit this overlay to a shared index")
			return nil
		}
		fmt.Fprintln(stdout, "next steps (after reviewing the overlay and confirming the checkout is unchanged):")
		fmt.Fprintf(stdout, "  cp -R -- %s/. %s/\n", shellQuote(contribution.Root), shellQuote(target.Root))
		fmt.Fprintf(stdout, "  waldo index verify %s\n", shellQuote(target.Root))
		fmt.Fprintf(stdout, "  git -C %s add --", shellQuote(target.Root))
		for _, file := range contribution.Files {
			fmt.Fprintf(stdout, " %s", shellQuote(file))
		}
		fmt.Fprintln(stdout)
		fmt.Fprintf(stdout, "  git -C %s diff --cached --check\n", shellQuote(target.Root))
		fmt.Fprintf(stdout, "  git -C %s commit -s\n", shellQuote(target.Root))
		return nil
	}
	fmt.Fprintf(stdout, "ingestion plan %s\n", identity[:12])
	fmt.Fprintf(stdout, "  destination  %s\n", plan.Destination)
	fmt.Fprintf(stdout, "  title        %s\n", plan.Title)
	fmt.Fprintf(stdout, "  description  %s\n", plan.Description)
	fmt.Fprintf(stdout, "  license      %s\n", plan.License)
	fmt.Fprintf(stdout, "  source       %s (%s)\n", plan.Source.Name, plan.Source.Category)
	fmt.Fprintf(stdout, "  mode         %s\n", plan.Mode)
	fmt.Fprintf(stdout, "  memory       %s\n", humanBytes(plan.MemoryBytes))
	fmt.Fprintf(stdout, "  input        %s files, %s\n", humanInteger(int64(len(plan.Inputs))), humanBytes(probe.Totals.Bytes))
	for _, input := range plan.Inputs {
		mapping := input.Adapter
		if input.TextColumn != "" {
			mapping += ":" + input.TextColumn
		}
		fmt.Fprintf(stdout, "    %-18s %s (%s)\n", mapping, input.Artifact.Path, humanBytes(input.Artifact.Bytes))
	}
	fmt.Fprintf(stdout, "  writer       Parquet schema %d, %s target, %s row groups, %s\n",
		plan.Writer.RecordSchema, humanBytes(plan.Writer.CompressedTarget),
		humanBytes(plan.Writer.RowGroupLogicalBytes), plan.Writer.Compression)
	fmt.Fprintln(stdout, "dry run complete; no files were written")
	return nil
}

type composeJSONLogWriter struct {
	mu     sync.Mutex
	output io.Writer
}

func (writer *composeJSONLogWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	message := strings.TrimRight(string(data), "\r\n")
	if message == "" {
		return len(data), nil
	}
	err := json.NewEncoder(writer.output).Encode(ingest.ProgressEvent{Phase: "fetch", Status: "output", Message: message})
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func writeComposePreflight(context Context, stdout io.Writer, loaded ingest.LoadedCompose, destination string) error {
	if context.JSON {
		return writeJSON(stdout, struct {
			Kind        string               `json:"kind"`
			Destination string               `json:"destination"`
			Compose     ingest.LoadedCompose `json:"compose"`
		}{Kind: "waldo-ingest-compose-preflight", Destination: destination, Compose: loaded})
	}
	fmt.Fprintf(stdout, "ingest compose %s\n", loaded.Path)
	fmt.Fprintf(stdout, "  sha256      %s\n", loaded.SHA256)
	fmt.Fprintf(stdout, "  destination %s\n", destination)
	fmt.Fprintf(stdout, "  title       %s\n", loaded.Compose.Title)
	fmt.Fprintf(stdout, "  license     %s\n", loaded.Compose.License)
	fmt.Fprintf(stdout, "  source      %s (%s)\n", loaded.Compose.Source.URL, loaded.Compose.Source.Category)
	for position, executable := range loaded.Executables {
		fmt.Fprintf(stdout, "  step %d      %s -> %s (%s)\n", position+1, executable.Name, executable.Path, executable.SHA256[:12])
	}
	fmt.Fprintln(stdout, "dry run complete; no commands were executed and no files were written")
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func ingestProgressReporter(output io.Writer, jsonOutput bool) ingest.ProgressSink {
	last := map[string]int64{}
	return func(event ingest.ProgressEvent) {
		if jsonOutput {
			_ = json.NewEncoder(output).Encode(event)
			return
		}
		short := event.Shard
		if len(short) > 12 {
			short = short[:12]
		}
		switch {
		case event.Phase == "fetch" && event.Status == "started":
			fmt.Fprintf(output, "fetch %d  %s started\n", event.Sequence, event.Input)
		case event.Phase == "fetch" && event.Status == "completed":
			fmt.Fprintf(output, "fetch %d  %s completed\n", event.Sequence, event.Input)
		case event.Phase == "input" && event.Status == "probing":
			fmt.Fprintf(output, "probing  %s\n", event.Input)
		case event.Phase == "input" && event.Status == "detected":
			fmt.Fprintf(output, "detected %s as %s (%s)\n", event.Input, event.Adapter, humanBytes(event.Bytes))
		case event.Phase == "convert" && event.Status == "started":
			fmt.Fprintf(output, "convert  %s using %s\n", event.Input, event.Adapter)
		case event.Phase == "convert" && event.Status == "completed":
			fmt.Fprintf(output, "converted %s (%s)\n", event.Input, humanBytes(event.Bytes))
		case event.Phase == "shard" && event.Status == "ready":
			fmt.Fprintf(output, "shard %d  %s ready (%s)\n", event.Sequence, short, humanBytes(event.Bytes))
		case event.Phase == "upload" && event.Status == "started":
			fmt.Fprintf(output, "upload %d  %s started on worker %d\n", event.Sequence, short, event.Worker)
		case event.Phase == "upload" && event.Status == "progress" && (event.Bytes == event.TotalBytes || event.Bytes-last[event.Shard] >= 64<<20):
			last[event.Shard] = event.Bytes
			fmt.Fprintf(output, "upload %d  %s %s/%s\n", event.Sequence, short, humanBytes(event.Bytes), humanBytes(event.TotalBytes))
		case event.Phase == "upload" && event.Status == "verified":
			fmt.Fprintf(output, "upload %d  %s verified at %s\n", event.Sequence, short, event.Remote)
		case event.Phase == "staging" && event.Status == "purged":
			fmt.Fprintf(output, "purged %d  %s reclaimed %s\n", event.Sequence, short, humanBytes(event.ReclaimedBytes))
		}
	}
}

type indexIngestOptions struct {
	Request         ingest.PlanRequest
	Inputs          []string
	DryRun          bool
	MetadataOptions []string
}

func parseIndexIngest(args []string) (indexIngestOptions, error) {
	var options indexIngestOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := func(name string) (string, error) {
			if i+1 >= len(args) {
				return "", usageError{message: name + " needs a value"}
			}
			i++
			return args[i], nil
		}
		var err error
		switch arg {
		case "--dry-run":
			options.DryRun = true
		case "--title":
			options.Request.Title, err = value("--title")
			options.MetadataOptions = append(options.MetadataOptions, "--title")
		case "--description":
			options.Request.Description, err = value("--description")
			options.MetadataOptions = append(options.MetadataOptions, "--description")
		case "--license":
			options.Request.License, err = value("--license")
			options.MetadataOptions = append(options.MetadataOptions, "--license")
		case "--source":
			options.Request.Source.URL, err = value("--source")
			options.MetadataOptions = append(options.MetadataOptions, "--source")
		case "--source-name":
			options.Request.Source.Name, err = value("--source-name")
			options.MetadataOptions = append(options.MetadataOptions, "--source-name")
		case "--source-category":
			options.Request.Source.Category, err = value("--source-category")
			options.MetadataOptions = append(options.MetadataOptions, "--source-category")
		case "--text-column":
			options.Request.TextColumn, err = value("--text-column")
			options.MetadataOptions = append(options.MetadataOptions, "--text-column")
		default:
			if strings.HasPrefix(arg, "-") {
				return indexIngestOptions{}, usageError{message: fmt.Sprintf("unknown index ingest option %q", arg)}
			}
			options.Inputs = append(options.Inputs, arg)
		}
		if err != nil {
			return indexIngestOptions{}, err
		}
	}
	if len(options.Inputs) != 2 {
		return indexIngestOptions{}, usageError{message: "index ingest requires exactly two positional arguments: <input> <destination>"}
	}
	options.Request.Destination = options.Inputs[1]
	options.Inputs = options.Inputs[:1]
	return options, nil
}
