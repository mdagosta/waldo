// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/openwaldo/waldo/internal/config"
	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/ingest"
)

func runIndexIngestUpdate(commandContext Context, args []string, stdout, stderr io.Writer) error {
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
	const mode = "rebuild-shards"
	logicalDestination := strings.TrimSuffix(corpusTarget.Path, filepath.Ext(corpusTarget.Path))
	options.Request.Destination = logicalDestination
	options.Request.Update = &ingest.UpdatePlan{Manifest: corpusTarget.Path, ManifestSHA256: manifestHash, Mode: mode}
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
	} else if isCorpusDirectory {
		if len(options.MetadataOptions) > 0 {
			return fmt.Errorf("corpus directory manifest owns corpus metadata; remove %s", strings.Join(options.MetadataOptions, ", "))
		}
		loadedCorpus.Apply(&options.Request)
		options.Inputs = loadedCorpus.InputPaths()
	} else if isSourceDirectory {
		if len(options.MetadataOptions) > 0 {
			return fmt.Errorf("source directory manifest owns corpus metadata; remove %s", strings.Join(options.MetadataOptions, ", "))
		}
		loadedSource.Apply(&options.Request)
		options.Inputs = loadedSource.InputPaths()
	} else if options.Request.Title == "" || options.Request.License == "" || options.Request.Source.URL == "" || options.Request.Source.Category == "" {
		return fmt.Errorf("direct index ingest --update requires --title, --license, --source, and --source-category")
	}
	if !isRecipe && options.InputProfile != "" {
		options.Request.Profile, err = ingest.LoadInputProfile(options.InputProfile)
		if err != nil {
			return fmt.Errorf("load input profile: %w", err)
		}
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
		fmt.Fprintf(stdout, "index ingest --update preflight\n  mode      %s\n  manifest  %s (%s)\n", mode, corpusTarget.Path, manifestHash[:12])
		return writeRecipePreflight(commandContext, stdout, loadedRecipe, logicalDestination)
	}
	var configuration config.Config
	if !options.DryRun {
		configuration, err = config.Load()
		if err != nil {
			return err
		}
		if configuration.Lookaside.Publish == nil {
			return fmt.Errorf("index ingest --update needs a writable lookaside; run `waldo config set lookaside <s3-or-file-URL>`")
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
		fmt.Fprintf(stdout, "index ingest --update plan %s\n  mode      %s\n  manifest  %s (%s)\n  input     %s files, %s\n", identity[:12], mode, corpusTarget.Path, manifestHash[:12], humanInteger(int64(len(plan.Inputs))), humanBytes(probe.Totals.Bytes))
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
	assembly, publication, err := ingest.ExecutePublication(execution, plan, staging, publisher, workers)
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
	contribution, err = ingest.ApplyContribution(target.Root, contribution)
	if err != nil {
		return fmt.Errorf("apply verified contribution %s: %w", contribution.Root, err)
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
	fmt.Fprintf(stdout, "updated %s (rebuilt)\n", corpusTarget.Path)
	fmt.Fprintf(stdout, "  records       %s input, %s retained, %s duplicate", humanInteger(assembly.InputDocs), humanInteger(assembly.RetainedDocs), humanInteger(assembly.DuplicateDocs))
	if assembly.RejectedDocs > 0 {
		fmt.Fprintf(stdout, ", %s rejected %s", humanInteger(assembly.RejectedDocs), rejectionLabel(plan))
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "  shards        %s new at %s\n", humanInteger(int64(len(assembly.Objects))), publication.BaseURL)
	fmt.Fprintf(stdout, "  index         applied %s writes, %s removals to %s\n", humanInteger(int64(len(contribution.Files))), humanInteger(int64(len(contribution.Removed))), contribution.IndexRoot)
	fmt.Fprintf(stdout, "  contribution  %s (retained)\n", contribution.Root)
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
		return waldoindex.Corpus{}, fmt.Errorf("index ingest --update target must resolve exactly one manifest; found %d: %s", len(corpora), strings.Join(paths, ", "))
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
