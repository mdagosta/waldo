// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/openwaldo/waldo/internal/config"
	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/ingest"
	"github.com/openwaldo/waldo/internal/lookaside"
)

var newIngestPublisher = func(ctx context.Context, publish config.Publish) (lookaside.Publisher, error) {
	return lookaside.NewPublisher(ctx, publish)
}

var ingestRecipeRunner ingest.CommandRunner = ingest.ExecCommandRunner{}

func runIndexIngest(context Context, args []string, stdout, stderr io.Writer) error {
	if boolOption(context, "update") {
		return runIndexIngestUpdate(context, args, stdout, stderr)
	}
	options, err := cobraIndexIngestOptions(context, args)
	if err != nil {
		return err
	}
	loadedRecipe, isRecipe, err := ingest.LoadRecipe(options.Inputs[0])
	if err != nil {
		return err
	}
	loadedCorpus, isCorpusDirectory, err := ingest.LoadCorpusDirectory(options.Inputs[0])
	if err != nil {
		return err
	}
	loadedSource, isSourceDirectory, err := ingest.LoadSourceDirectory(options.Inputs[0])
	if err != nil {
		return err
	}
	requestedDestination := options.Request.Destination
	if isRecipe {
		if requestedDestination == "" {
			return fmt.Errorf("ingest recipes require an explicit destination")
		}
		if len(options.MetadataOptions) > 0 {
			return fmt.Errorf("recipe input owns corpus metadata; remove %s", strings.Join(options.MetadataOptions, ", "))
		}
		options.Request.Title = loadedRecipe.Recipe.Title
		options.Request.Description = loadedRecipe.Recipe.Description
		if loadedRecipe.Recipe.Schema == ingest.RecipeSchema {
			options.Request.License = loadedRecipe.Recipe.License
			options.Request.Source = loadedRecipe.Recipe.Source.AsPlanSource("", loadedRecipe.Recipe.Source.Name)
			options.Request.TextColumn = loadedRecipe.Recipe.TextColumn
			options.Request.RecordMaximumBytes = loadedRecipe.Recipe.RecordMaximumBytes
			options.Request.Profile = loadedRecipe.Recipe.Input
		}
	} else if isCorpusDirectory {
		if requestedDestination == "" {
			return fmt.Errorf("corpus directory ingestion requires an explicit destination")
		}
		if len(options.MetadataOptions) > 0 {
			return fmt.Errorf("corpus directory manifest owns corpus metadata; remove %s", strings.Join(options.MetadataOptions, ", "))
		}
		loadedCorpus.Apply(&options.Request)
		options.Inputs = loadedCorpus.InputPaths()
	} else if isSourceDirectory {
		if requestedDestination == "" {
			return fmt.Errorf("source directory ingestion requires an explicit destination")
		}
		if len(options.MetadataOptions) > 0 {
			return fmt.Errorf("source directory manifest owns corpus metadata; remove %s", strings.Join(options.MetadataOptions, ", "))
		}
		loadedSource.Apply(&options.Request)
		options.Inputs = loadedSource.InputPaths()
	} else if options.Request.Destination == "" {
		return fmt.Errorf("direct index ingest requires a destination")
	} else if options.Request.Title == "" || options.Request.License == "" || options.Request.Source.URL == "" || options.Request.Source.Category == "" {
		return fmt.Errorf("direct index ingest requires --title, --license, --source, and --source-category")
	}
	if !isRecipe && options.InputProfile != "" {
		options.Request.Profile, err = ingest.LoadInputProfile(options.InputProfile)
		if err != nil {
			return fmt.Errorf("load input profile: %w", err)
		}
	}
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	workers := options.Workers
	if workers == 0 && configuration.Lookaside.Publish != nil {
		workers = configuration.Lookaside.Publish.Workers
	}
	configuredRoot, managedDefault, err := config.EffectiveIndexRoot(configuration)
	if err != nil {
		return err
	}
	if managedDefault && !explicitIndexPath(options.Request.Destination) {
		return managedIndexMutationError("ingest into")
	}
	target, err := waldoindex.ResolveDestinationConfigured(configuredRoot, options.Request.Destination)
	if err != nil {
		return err
	}
	managed, err := config.IsManagedIndexPath(target.Root)
	if err != nil {
		return err
	}
	if managed {
		return managedIndexMutationError("ingest into")
	}
	options.Request.Destination = target.Rel
	if options.Request.Source.Name == "" {
		options.Request.Source.Name = path.Base(strings.TrimSuffix(target.Rel, "/"))
	}
	if isRecipe && options.DryRun {
		return writeRecipePreflight(context, stdout, loadedRecipe, target.Rel)
	}
	if !options.DryRun {
		if configuration.Lookaside.Publish == nil {
			return fmt.Errorf("index ingest needs a writable lookaside; run `waldo config set lookaside <s3-or-file-URL>`")
		}
	}
	execution := ingest.WithProgress(context.Execution, ingestProgressReporter(stderr, context.JSON))
	var probe ingest.Probe
	var prepared *ingest.PreparedRecipe
	if isRecipe {
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
		recipeOutput := io.Writer(stderr)
		if context.JSON {
			recipeOutput = &recipeJSONLogWriter{output: stderr}
		}
		result, err := ingest.PrepareRecipeWithWorkers(execution, loadedRecipe, target.Rel, stagingBase, workers, ingestRecipeRunner, recipeOutput, recipeOutput)
		if err != nil {
			return err
		}
		prepared = &result
		probe = result.Probe
		recipeEvidence := result.Loaded.Evidence
		options.Request.RecipeEvidence = &recipeEvidence
		if len(result.Loaded.Recipe.Sources) == 0 {
			options.Request.InputRoot = result.Inputs
		} else {
			options.Request.Sources = result.SourceRequests()
		}
	} else {
		probe, err = ingest.ProbePathsWithWorkers(execution, options.Inputs, workers)
		if err != nil {
			return err
		}
		if isCorpusDirectory {
			if err := loadedCorpus.VerifyProbe(probe); err != nil {
				return err
			}
		} else if isSourceDirectory {
			if err := loadedSource.VerifyProbe(probe); err != nil {
				return err
			}
		}
	}
	plan, err := ingest.NewPlan(probe, options.Request)
	if err != nil {
		return err
	}
	emitIngestForceFormatWarning(stderr, plan, context.JSON)
	emitIngestFallbackWarning(stderr, plan, context.JSON)
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
		assembly, publication, err := ingest.ExecutePublication(execution, plan, staging, publisher, workers)
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
			if err := ingest.PurgePreparedRecipe(*prepared); err != nil {
				return err
			}
		}
		emitIngestExclusionWarning(stderr, assembly, plan)
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
		fmt.Fprintf(stdout, "  records      %s input, %s retained, %s duplicate", humanInteger(assembly.InputDocs), humanInteger(assembly.RetainedDocs), humanInteger(assembly.DuplicateDocs))
		if assembly.RejectedDocs > 0 {
			fmt.Fprintf(stdout, ", %s rejected %s", humanInteger(assembly.RejectedDocs), rejectionLabel(plan))
		}
		fmt.Fprintln(stdout)
		var tokens int64
		for _, object := range assembly.Objects {
			tokens += object.Tokens
		}
		fmt.Fprintf(stdout, "  tokens       %s (%s)\n", humanCount(tokens), manifest.ConvertedBy.Tokenizer)
		fmt.Fprintf(stdout, "  objects      %s published to %s\n", humanInteger(int64(len(publication.Objects))), publication.BaseURL)
		fmt.Fprintf(stdout, "  contribution %s (%s writes, %s removals)\n", contribution.Root, humanInteger(int64(len(contribution.Files))), humanInteger(int64(len(contribution.Removed))))
		for _, file := range contribution.Files {
			fmt.Fprintf(stdout, "    %s\n", file)
		}
		for _, file := range contribution.Removed {
			fmt.Fprintf(stdout, "    remove %s\n", file)
		}
		if strings.HasPrefix(publication.BaseURL, "file://") {
			fmt.Fprintln(stdout, "local publication is for end-to-end testing only; do not commit this overlay to a shared index")
			return nil
		}
		fmt.Fprintln(stdout, "next steps (after reviewing the overlay and confirming the checkout is unchanged):")
		fmt.Fprintf(stdout, "  cp -R -- %s/. %s/\n", shellQuote(contribution.Root), shellQuote(target.Root))
		if len(contribution.Removed) > 0 {
			fmt.Fprintf(stdout, "  rm --")
			for _, file := range contribution.Removed {
				fmt.Fprintf(stdout, " %s", shellQuote(filepath.Join(target.Root, filepath.FromSlash(file))))
			}
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "  waldo index verify %s\n", shellQuote(target.Root))
		fmt.Fprintf(stdout, "  git -C %s add --", shellQuote(target.Root))
		for _, file := range contribution.Files {
			fmt.Fprintf(stdout, " %s", shellQuote(file))
		}
		for _, file := range contribution.Removed {
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
	if len(plan.Sources) == 0 {
		fmt.Fprintf(stdout, "  license      %s\n", plan.License)
		fmt.Fprintf(stdout, "  source       %s (%s)\n", plan.Source.Name, plan.Source.Category)
	} else {
		fmt.Fprintf(stdout, "  sources      %d\n", len(plan.Sources))
		for _, source := range plan.Sources {
			fmt.Fprintf(stdout, "    %-16s %s  %s\n", source.ID, source.License, source.URL)
		}
	}
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

func rejectionLabel(plan ingest.Plan) string {
	for _, input := range plan.Inputs {
		if input.Profile.Type == ingest.ProfileXMLRecord && input.Profile.XML.OnMalformed == "skip" {
			return "malformed XML"
		}
	}
	return "empty"
}

func emitIngestExclusionWarning(output io.Writer, assembly ingest.AssemblyResult, plan ingest.Plan) {
	if assembly.RejectedDocs == 0 {
		return
	}
	detail := strings.ToUpper(rejectionLabel(plan))
	if len(assembly.Rejections) > 0 {
		reasons := make([]string, 0, len(assembly.Rejections))
		for reason := range assembly.Rejections {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		parts := make([]string, 0, len(reasons))
		for _, reason := range reasons {
			parts = append(parts, fmt.Sprintf("%s %s", humanInteger(assembly.Rejections[reason]), strings.ToUpper(reason)))
		}
		detail = strings.Join(parts, ", ")
	}
	fmt.Fprintf(output, "\nWARNING: WALDO EXCLUDED %s RECORDS DURING INGESTION (%s).\n", humanInteger(assembly.RejectedDocs), detail)
	fmt.Fprintln(output, "WARNING: EXCLUDED RECORDS ARE NOT PRESENT IN THE PUBLISHED SHARDS; REVIEW THE SOURCE POLICY AND COUNTS BEFORE COMMITTING.")
	fmt.Fprintln(output)
}

func emitIngestFallbackWarning(output io.Writer, plan ingest.Plan, jsonOutput bool) {
	for _, fallback := range plan.TextFallbacks {
		representation := "RAW TEXT"
		if fallback.Adapter == "opaque-base64" {
			representation = "LOSSLESS BASE64 TEXT"
		}
		message := fmt.Sprintf("WALDO INGESTING %s %s ARTIFACTS (%s) AS %s; CONTENT IS RETAINED", humanInteger(fallback.Artifacts), strings.ToUpper(fallback.DetectedFormat), humanBytes(fallback.Bytes), representation)
		if jsonOutput {
			_ = json.NewEncoder(output).Encode(ingest.ProgressEvent{Phase: "plan", Status: "warning", Message: message})
			continue
		}
		fmt.Fprintf(output, "WARNING: %s.\n", message)
	}
}

func emitIngestForceFormatWarning(output io.Writer, plan ingest.Plan, jsonOutput bool) {
	counts := map[string]int64{}
	for _, input := range plan.Inputs {
		if input.DetectedFormat != "" {
			counts[input.DetectedFormat+"->"+input.Artifact.Format]++
		}
	}
	if len(counts) == 0 {
		return
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		message := fmt.Sprintf("WALDO FORCE-FORMAT OVERRIDE %s FOR %s INPUT FILES; AUTOMATIC FORMAT SELECTION WAS OVERRIDDEN, BUT THE SELECTED ADAPTER WILL STILL PARSE AND VALIDATE CONTENT", strings.ToUpper(key), humanInteger(counts[key]))
		if jsonOutput {
			_ = json.NewEncoder(output).Encode(ingest.ProgressEvent{Phase: "plan", Status: "warning", Message: message})
			continue
		}
		fmt.Fprintf(output, "WARNING: %s.\n", message)
	}
}

type recipeJSONLogWriter struct {
	mu     sync.Mutex
	output io.Writer
}

func (writer *recipeJSONLogWriter) Write(data []byte) (int, error) {
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

func writeRecipePreflight(context Context, stdout io.Writer, loaded ingest.LoadedRecipe, destination string) error {
	if context.JSON {
		return writeJSON(stdout, struct {
			Kind        string              `json:"kind"`
			Destination string              `json:"destination"`
			Recipe      ingest.LoadedRecipe `json:"recipe"`
		}{Kind: "waldo-ingest-recipe-preflight", Destination: destination, Recipe: loaded})
	}
	fmt.Fprintf(stdout, "ingest recipe %s\n", loaded.Path)
	fmt.Fprintf(stdout, "  sha256      %s\n", loaded.SHA256)
	fmt.Fprintf(stdout, "  destination %s\n", destination)
	fmt.Fprintf(stdout, "  title       %s\n", loaded.Recipe.Title)
	if len(loaded.Recipe.Sources) == 0 {
		fmt.Fprintf(stdout, "  license     %s\n", loaded.Recipe.License)
		fmt.Fprintf(stdout, "  source      %s (%s)\n", loaded.Recipe.Source.URL, loaded.Recipe.Source.Category)
	} else {
		fmt.Fprintf(stdout, "  sources     %d\n", len(loaded.Recipe.Sources))
		for _, source := range loaded.Recipe.Sources {
			fmt.Fprintf(stdout, "    %-16s %s  %s\n", source.ID, source.License, source.Source.URL)
		}
	}
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
	var ingestBytes, ingestTotalBytes, ingestFiles, ingestTotalFiles, ingestDocs, ingestTokens int64
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
		case event.Phase == "ingest":
			if event.TotalBytes > 0 {
				ingestTotalBytes = event.TotalBytes
			}
			if event.TotalFiles > 0 {
				ingestTotalFiles = event.TotalFiles
			}
			if event.Bytes > 0 {
				ingestBytes = event.Bytes
			}
			if event.Files > 0 {
				ingestFiles = event.Files
			}
			if event.Docs > 0 || event.Status == "completed" {
				ingestDocs = event.Docs
			}
			if event.Tokens > 0 || event.Status == "completed" {
				ingestTokens = event.Tokens
			}
			label := "ingest  "
			if event.Status == "started" {
				label = "ingest started "
			} else if event.Status == "completed" {
				label = "ingest complete"
			}
			fmt.Fprintf(output, "%s  %s/%s files  %s/%s  %s docs  %s tokens\n",
				label, humanInteger(ingestFiles), humanInteger(ingestTotalFiles),
				humanBytes(ingestBytes), humanBytes(ingestTotalBytes),
				humanInteger(ingestDocs), humanInteger(ingestTokens))
		case event.Phase == "audit" && event.Status == "started":
			fmt.Fprintf(output, "audit %d  %s started on worker %d\n", event.Sequence, short, event.Worker)
		case event.Phase == "audit" && event.Status == "completed":
			fmt.Fprintf(output, "audit %d  %s completed\n", event.Sequence, short)
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
	InputProfile    string
	Workers         int
	MetadataOptions []string
}

func cobraIndexIngestOptions(context Context, args []string) (indexIngestOptions, error) {
	destination := ""
	if len(args) > 1 {
		destination = args[1]
	}
	options := indexIngestOptions{
		Inputs:       []string{args[0]},
		DryRun:       boolOption(context, "dry-run"),
		InputProfile: stringOption(context, "input-profile"),
		Workers:      intOption(context, "workers"),
		Request: ingest.PlanRequest{
			Title:       stringOption(context, "title"),
			Description: stringOption(context, "description"),
			License:     stringOption(context, "license"),
			TextColumn:  stringOption(context, "text-column"),
			ForceFormat: stringOption(context, "force-format"),
			Destination: destination,
			Source: ingest.PlanSource{
				URL:      stringOption(context, "source"),
				Name:     stringOption(context, "source-name"),
				Category: stringOption(context, "source-category"),
			},
		},
	}
	if options.Workers < 0 || options.Workers > 32 {
		return indexIngestOptions{}, fmt.Errorf("--workers must be an integer from 1 to 32, or 0 to use lookaside.workers")
	}
	for _, name := range []string{"title", "description", "license", "source", "source-name", "source-category", "text-column", "input-profile"} {
		if optionChanged(context, name) {
			options.MetadataOptions = append(options.MetadataOptions, "--"+name)
		}
	}
	return options, nil
}
