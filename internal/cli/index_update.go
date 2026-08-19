// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/openwaldo/waldo/internal/config"
	"github.com/openwaldo/waldo/internal/corpus"
	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/ingest"
	"github.com/openwaldo/waldo/internal/lookaside"
	"github.com/openwaldo/waldo/internal/shard"
)

func runIndexUpdate(commandContext Context, args []string, stdout, stderr io.Writer) error {
	rebuild := boolOption(commandContext, "rebuild-shards")
	options, err := cobraIndexIngestOptions(commandContext, args)
	if err != nil {
		return err
	}
	pathConfiguration, err := config.Load()
	if err != nil {
		return err
	}
	configuredRoot, managedDefault, err := config.EffectiveIndexRoot(pathConfiguration)
	if err != nil {
		return err
	}
	if managedDefault && !explicitIndexPath(options.Request.Destination) {
		return managedIndexMutationError("update")
	}
	target, err := waldoindex.ResolveConfigured(configuredRoot, options.Request.Destination)
	if err != nil {
		return err
	}
	managed, err := config.IsManagedIndexPath(target.Root)
	if err != nil {
		return err
	}
	if managed {
		return managedIndexMutationError("update")
	}
	corpusTarget, err := resolveSingleUpdateCorpus(target)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(target.Root, filepath.FromSlash(corpusTarget.Path))
	manifestHash, err := ingest.ManifestFileSHA256(manifestPath)
	if err != nil {
		return err
	}
	mode := "append"
	if rebuild {
		mode = "rebuild-shards"
	}
	logicalDestination := strings.TrimSuffix(corpusTarget.Path, filepath.Ext(corpusTarget.Path))
	options.Request.Destination = logicalDestination
	options.Request.Update = &ingest.UpdatePlan{Manifest: corpusTarget.Path, ManifestSHA256: manifestHash, Mode: mode}
	loadedRecipe, isRecipe, err := ingest.LoadRecipe(options.Inputs[0])
	if err != nil {
		return err
	}
	if isRecipe {
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
	} else if options.Request.Title == "" || options.Request.License == "" || options.Request.Source.URL == "" || options.Request.Source.Category == "" {
		return fmt.Errorf("direct index update requires --title, --license, --source, and --source-category")
	}
	if options.Request.Source.Name == "" {
		options.Request.Source.Name = corpusTarget.Manifest.Name
	}
	if isRecipe && options.DryRun {
		if commandContext.JSON {
			return writeJSON(stdout, struct {
				Mode           string              `json:"mode"`
				Manifest       string              `json:"manifest"`
				ManifestSHA256 string              `json:"manifest_sha256"`
				Recipe         ingest.LoadedRecipe `json:"recipe"`
			}{mode, corpusTarget.Path, manifestHash, loadedRecipe})
		}
		fmt.Fprintf(stdout, "index update preflight\n  mode      %s\n  manifest  %s (%s)\n", mode, corpusTarget.Path, manifestHash[:12])
		return writeRecipePreflight(commandContext, stdout, loadedRecipe, logicalDestination)
	}
	var configuration config.Config
	if !options.DryRun {
		configuration, err = config.Load()
		if err != nil {
			return err
		}
		if configuration.Lookaside.Publish == nil {
			return fmt.Errorf("index update needs a writable lookaside; run `waldo config set lookaside <s3-or-file-URL>`")
		}
	}
	workers := options.Workers
	if workers == 0 && configuration.Lookaside.Publish != nil {
		workers = configuration.Lookaside.Publish.Workers
	}
	execution := ingest.WithProgress(commandContext.Execution, ingestProgressReporter(stderr, commandContext.JSON))
	var probe ingest.Probe
	var prepared *ingest.PreparedRecipe
	if isRecipe {
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
		if commandContext.JSON {
			recipeOutput = &recipeJSONLogWriter{output: stderr}
		}
		state := recipeUpdateState(mode, corpusTarget.Path, manifestHash, corpusTarget.Manifest)
		result, err := ingest.PrepareRecipeUpdateWithWorkers(execution, loadedRecipe, logicalDestination, stagingBase, state, workers, ingestRecipeRunner, recipeOutput, recipeOutput)
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
	}
	plan, err := ingest.NewPlan(probe, options.Request)
	if err != nil {
		return err
	}
	emitIngestForceFormatWarning(stderr, plan, commandContext.JSON)
	emitIngestFallbackWarning(stderr, plan, commandContext.JSON)
	identity, err := plan.Identity()
	if err != nil {
		return err
	}
	if options.DryRun {
		if commandContext.JSON {
			return writeJSON(stdout, struct {
				Identity string      `json:"identity"`
				Plan     ingest.Plan `json:"plan"`
			}{identity, plan})
		}
		fmt.Fprintf(stdout, "index update plan %s\n  mode      %s\n  manifest  %s (%s)\n  input     %s files, %s\n", identity[:12], mode, corpusTarget.Path, manifestHash[:12], humanInteger(int64(len(plan.Inputs))), humanBytes(probe.Totals.Bytes))
		return nil
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
	publisher, err := newIngestPublisher(execution, *configuration.Lookaside.Publish)
	if err != nil {
		return err
	}
	var seed ingest.DedupSeed
	if !rebuild {
		seed, err = updateDedupSeed(commandContext.Execution, target)
		if err != nil {
			return err
		}
	}
	assembly, publication, err := ingest.ExecutePublicationWithSeed(execution, plan, staging, publisher, workers, seed)
	if err != nil {
		return err
	}
	updated, err := ingest.BuildUpdatedManifest(plan, corpusTarget.Manifest, assembly, publication.BaseURL, manifestPath)
	if err != nil {
		return err
	}
	contribution, err := ingest.StageUpdateContribution(target.Root, staging, plan, updated)
	if err != nil {
		return err
	}
	if prepared != nil {
		if err := ingest.PurgePreparedRecipe(*prepared); err != nil {
			return err
		}
	}
	if commandContext.JSON {
		emitIngestExclusionWarning(stderr, assembly, plan)
		return writeJSON(stdout, struct {
			Identity     string                    `json:"identity"`
			Plan         ingest.Plan               `json:"plan"`
			Assembly     ingest.AssemblyResult     `json:"assembly"`
			Publication  ingest.PublicationResult  `json:"publication"`
			Contribution ingest.ContributionResult `json:"contribution"`
		}{identity, plan, assembly, publication, contribution})
	}
	emitIngestExclusionWarning(stderr, assembly, plan)
	verb := "appended"
	if rebuild {
		verb = "rebuilt"
	} else if assembly.RetainedDocs == 0 {
		verb = "unchanged"
	}
	fmt.Fprintf(stdout, "updated %s (%s)\n", corpusTarget.Path, verb)
	fmt.Fprintf(stdout, "  records       %s input, %s retained, %s duplicate", humanInteger(assembly.InputDocs), humanInteger(assembly.RetainedDocs), humanInteger(assembly.DuplicateDocs))
	if assembly.RejectedDocs > 0 {
		fmt.Fprintf(stdout, ", %s rejected %s", humanInteger(assembly.RejectedDocs), rejectionLabel(plan))
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "  shards        %s new at %s\n", humanInteger(int64(len(assembly.Objects))), publication.BaseURL)
	fmt.Fprintf(stdout, "  contribution  %s\n", contribution.Root)
	for _, file := range contribution.Files {
		fmt.Fprintf(stdout, "    write  %s\n", file)
	}
	for _, file := range contribution.Removed {
		fmt.Fprintf(stdout, "    remove %s\n", file)
	}
	return nil
}

func resolveSingleUpdateCorpus(target waldoindex.Target) (waldoindex.Corpus, error) {
	var corpora []waldoindex.Corpus
	if err := waldoindex.WalkCorpora(target, func(value waldoindex.Corpus) error {
		corpora = append(corpora, value)
		return nil
	}); err != nil {
		return waldoindex.Corpus{}, err
	}
	if len(corpora) != 1 {
		paths := make([]string, 0, len(corpora))
		for _, value := range corpora {
			paths = append(paths, value.Path)
		}
		return waldoindex.Corpus{}, fmt.Errorf("index update target must resolve exactly one manifest; found %d: %s", len(corpora), strings.Join(paths, ", "))
	}
	return corpora[0], nil
}

func recipeUpdateState(mode, manifest, digest string, existing waldoindex.Manifest) ingest.RecipeUpdateState {
	state := ingest.RecipeUpdateState{Kind: "waldo-ingest-update-state", Schema: 1, Mode: mode, Manifest: manifest, ManifestSHA256: digest, Sources: existing.Sources}
	if existing.Rollup != nil {
		state.Shards, state.Docs, state.Tokens, state.Bytes = int(existing.Rollup.Count), existing.Rollup.Docs, existing.Rollup.Tokens, existing.Rollup.Bytes
		return state
	}
	state.Shards = len(existing.Shards)
	for _, object := range existing.Shards {
		state.Docs += object.Docs
		state.Tokens += object.Tokens
		state.Bytes += object.Bytes
	}
	return state
}

func updateDedupSeed(ctx context.Context, target waldoindex.Target) (ingest.DedupSeed, error) {
	policy, err := corpus.NewLicensePolicy(nil, nil)
	if err != nil {
		return nil, err
	}
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return nil, err
	}
	bom, err := corpus.BuildBOM(ctx, []waldoindex.Target{target}, policy, cache)
	if err != nil {
		return nil, err
	}
	materialized, err := corpus.Materialize(ctx, bom, cache, nil)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(materialized.Objects))
	seen := map[string]bool{}
	for _, object := range materialized.Objects {
		if !seen[object.Path] {
			seen[object.Path] = true
			paths = append(paths, object.Path)
		}
	}
	if _, err := shard.Audit(ctx, paths); err != nil {
		return nil, fmt.Errorf("audit existing update corpus: %w", err)
	}
	return func(add func([]ingest.DedupIdentity) error) error {
		const batchSize = 8192
		batch := make([]ingest.DedupIdentity, 0, batchSize)
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			if err := add(batch); err != nil {
				return err
			}
			batch = batch[:0]
			return nil
		}
		for _, path := range paths {
			if err := shard.WalkRecords(path, func(_ int64, record shard.RecordView) error {
				batch = append(batch, ingest.DedupIdentity{SHA256: record.ID, License: record.License})
				if len(batch) == batchSize {
					return flush()
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return flush()
	}, nil
}
