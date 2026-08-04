package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/openwaldo/waldo-new/internal/config"
	waldoindex "github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/ingest"
	"github.com/openwaldo/waldo-new/internal/lookaside"
)

var newIngestPublisher = func(ctx context.Context, publish config.Publish) (lookaside.Publisher, error) {
	return lookaside.NewPublisher(ctx, publish)
}

func runIndexIngest(context Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseIndexIngest(args)
	if err != nil {
		return err
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
	probe, err := ingest.ProbePaths(execution, options.Inputs)
	if err != nil {
		return err
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
		target, err := waldoindex.Resolve(context.IndexPath, "")
		if err != nil {
			return err
		}
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
		fmt.Fprintf(stdout, "  waldo --index %s index verify\n", shellQuote(target.Root))
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
	Request ingest.PlanRequest
	Inputs  []string
	DryRun  bool
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
		case "--description":
			options.Request.Description, err = value("--description")
		case "--license":
			options.Request.License, err = value("--license")
		case "--source":
			options.Request.Source.URL, err = value("--source")
		case "--source-name":
			options.Request.Source.Name, err = value("--source-name")
		case "--source-category":
			options.Request.Source.Category, err = value("--source-category")
		case "--text-column":
			options.Request.TextColumn, err = value("--text-column")
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
	request := &options.Request
	if request.Title == "" || request.License == "" || request.Source.URL == "" || request.Source.Category == "" {
		return indexIngestOptions{}, usageError{message: "index ingest requires --title, --license, --source, and --source-category"}
	}
	if request.Source.Name == "" {
		request.Source.Name = path.Base(strings.TrimSuffix(request.Destination, "/"))
	}
	return options, nil
}
