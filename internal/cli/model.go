// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	waldoai "github.com/openwaldo/waldo/internal/ai"
	"github.com/openwaldo/waldo/internal/calibration"
	"github.com/openwaldo/waldo/internal/config"
	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/disclosure"
	"github.com/openwaldo/waldo/internal/inference"
	"github.com/openwaldo/waldo/internal/lookaside"
	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/modelexport"
	"github.com/openwaldo/waldo/internal/modelquant"
	"github.com/openwaldo/waldo/internal/shard"
	"github.com/openwaldo/waldo/internal/signing"
	"github.com/openwaldo/waldo/internal/training"
	"golang.org/x/term"
)

func runModelForecast(context Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 {
		isCompose, err := model.IsComposeFile(args[0])
		if err != nil {
			return err
		}
		if isCompose {
			return runModelComposeForecast(context, args[0], stdout)
		}
	}
	return runModelIndexForecast(context, args, stdout, stderr)
}

func runModelComposeForecast(context Context, path string, stdout io.Writer) error {
	compose, composePath, err := model.LoadCompose(path)
	if err != nil {
		return err
	}
	calibrations, err := configuredForecastCalibration()
	if err != nil {
		return err
	}
	report, err := model.ForecastComposeWithCalibration(compose, calibrations)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Compose  string                 `json:"compose"`
			Forecast model.ResourceForecast `json:"forecast"`
		}{Compose: composePath, Forecast: report})
	}
	writeModelForecast(stdout, report)
	return nil
}

func runModelIndexForecast(context Context, paths []string, stdout, warnings io.Writer) error {
	targets, err := resolveIndexArgumentsWithWarning(context.Execution, paths, warnings)
	if err != nil {
		return err
	}
	policy, err := corpus.NewLicensePolicy(nil, nil)
	if err != nil {
		return err
	}
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return err
	}
	bom, err := corpus.BuildBOM(context.Execution, targets, policy, cache)
	if err != nil {
		return err
	}
	calibrations, err := configuredForecastCalibration()
	if err != nil {
		return err
	}
	preset, report, err := model.ForecastIndexSelectionWithCalibration(bom.Totals.Tokens, calibrations)
	if err != nil {
		return err
	}
	parameters, err := preset.Architecture.Forecast()
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Index      any                    `json:"index"`
			Paths      []string               `json:"paths"`
			Preset     string                 `json:"preset"`
			Parameters uint64                 `json:"approximate_parameters"`
			Tokens     int64                  `json:"tokens"`
			Budget     string                 `json:"budget"`
			Forecast   model.ResourceForecast `json:"forecast"`
		}{Index: bom.Index, Paths: bom.Paths, Preset: preset.Name, Parameters: parameters.ApproximateParameters, Tokens: bom.Totals.Tokens, Budget: "one-pass", Forecast: report})
	}
	fmt.Fprintf(stdout, "MODEL:       %s (%s parameters)\n", preset.Name, humanCount(int64(parameters.ApproximateParameters)))
	fmt.Fprintf(stdout, "TOKENS:      %s\n", humanCount(bom.Totals.Tokens))
	fmt.Fprintln(stdout, "BUDGET:      one pass")
	fmt.Fprintln(stdout)
	writeModelForecast(stdout, report)
	return nil
}

func writeModelForecast(stdout io.Writer, report model.ResourceForecast) {
	type row struct {
		manufacturer string
		accelerator  string
		GPUs         string
		memory       string
		duration     string
	}
	rows := make([]row, 0, len(report.Configurations))
	observedRuns := 0
	manufacturerWidth, acceleratorWidth := len("MFR"), len("ACCELERATOR")
	GPUsWidth, memoryWidth, durationWidth := len("GPUS"), len("MEMORY/GPU"), len("APPROX. TIME")
	for _, configuration := range report.Configurations {
		if configuration.EstimateSource == "observed-runs" {
			observedRuns += configuration.ObservedRuns
		}
		candidate := row{
			manufacturer: configuration.Manufacturer,
			accelerator:  configuration.Accelerator,
			GPUs:         fmt.Sprintf("%d", configuration.GPUs),
			memory:       hardwareMemory(configuration.MemoryPerGPUBytes),
			duration:     approximateDuration(configuration.ApproximateSeconds),
		}
		rows = append(rows, candidate)
		manufacturerWidth = max(manufacturerWidth, len(candidate.manufacturer))
		acceleratorWidth = max(acceleratorWidth, len(candidate.accelerator))
		GPUsWidth = max(GPUsWidth, len(candidate.GPUs))
		memoryWidth = max(memoryWidth, len(candidate.memory))
		durationWidth = max(durationWidth, len(candidate.duration))
	}
	if observedRuns > 0 {
		label := "runs"
		if observedRuns == 1 {
			label = "run"
		}
		fmt.Fprintf(stdout, "CALIBRATION: %d completed local %s applied\n\n", observedRuns, label)
	}
	fmt.Fprintf(stdout, "%*s  %-*s  %-*s  %*s  %*s\n", GPUsWidth, "GPUS", manufacturerWidth, "MFR", acceleratorWidth, "ACCELERATOR", memoryWidth, "MEMORY/GPU", durationWidth, "APPROX. TIME")
	for _, candidate := range rows {
		fmt.Fprintf(stdout, "%*s  %-*s  %-*s  %*s  %*s\n", GPUsWidth, candidate.GPUs, manufacturerWidth, candidate.manufacturer, acceleratorWidth, candidate.accelerator, memoryWidth, candidate.memory, durationWidth, candidate.duration)
	}
}

func configuredForecastCalibration() ([]model.ForecastCalibration, error) {
	root, err := configuredModelRoot()
	if err != nil {
		return nil, err
	}
	return model.LoadForecastCalibration(root)
}

func hardwareMemory(bytes uint64) string {
	const gibibyte = uint64(1 << 30)
	if bytes%gibibyte == 0 {
		return fmt.Sprintf("%d GB", bytes/gibibyte)
	}
	return humanBytesUint(bytes)
}

func approximateDuration(seconds int64) string {
	hours := float64(seconds) / float64(time.Hour/time.Second)
	if hours < 1 {
		return "under 1 hour"
	}
	if hours < 100 {
		value := int64(math.Round(hours))
		if value < 1 {
			value = 1
		}
		if value == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", value)
	}
	days := int64(math.Round(hours / 24))
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func runModelInit(context Context, args []string, stdout, stderr io.Writer) error {
	name, presetName := args[0], stringOption(context, "preset")
	preset, err := model.PresetByName(presetName)
	if err != nil {
		return err
	}
	builder, err := configuredModelBuilder(context, stderr)
	if err != nil {
		return err
	}
	inspection, err := builder.Initialize(name, preset.Architecture)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, inspection)
	}
	fmt.Fprintf(stdout, "initialized model %s\n", name)
	fmt.Fprintf(stdout, "  preset        %s\n", preset.Name)
	fmt.Fprintf(stdout, "  location      %s\n", inspection.Path)
	fmt.Fprintf(stdout, "  model id      %s\n", shortModelHash(inspection.Model.ID))
	fmt.Fprintf(stdout, "  estimate      %s parameters, %s weights\n", humanIntegerUint(inspection.Model.Forecast.ApproximateParameters), humanBytesUint(inspection.Model.Forecast.ParameterBytes))
	return nil
}

func runModelPull(context Context, args []string, stdout, stderr io.Writer) error {
	root, err := configuredModelRoot()
	if err != nil {
		return err
	}
	puller := model.Puller{Root: root, Client: &http.Client{}, Progress: func(progress model.PullProgress) {
		fmt.Fprintln(stderr, progress.Message)
	}}
	inspection, err := puller.Pull(context.Execution, args[0], args[1])
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, inspection)
	}
	fmt.Fprintf(stdout, "pulled model %s\n", inspection.Model.Name)
	fmt.Fprintf(stdout, "  location      %s\n", inspection.Path)
	fmt.Fprintf(stdout, "  model id      %s\n", shortModelHash(inspection.Model.ID))
	fmt.Fprintf(stdout, "  origin        %s@%s\n", inspection.Origin.Source.Repository, shortModelHash(inspection.Origin.Source.Revision))
	fmt.Fprintf(stdout, "  weights       %s\n", humanBytesUint(inspection.Model.Forecast.ParameterBytes))
	return nil
}

func runModelList(context Context, args []string, stdout, _ io.Writer) error {
	root, err := configuredModelRoot()
	if err != nil {
		return err
	}
	models, err := model.List(root, args)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, models)
	}
	if len(models) == 0 {
		return nil
	}
	table := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "NAME\tSTATE\tPARAMETERS\tRUNS\tUPDATED (UTC)")
	for _, item := range models {
		state := "untrained"
		if item.State != "" {
			state = string(item.State)
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", item.Name, state, humanCount(int64(item.Parameters)), humanInteger(int64(item.Runs)), item.Updated)
	}
	return table.Flush()
}

func runModelSummary(context Context, args []string, stdout, _ io.Writer) error {
	root, err := configuredModelRoot()
	if err != nil {
		return err
	}
	inspection, err := model.Inspect(root, args[0])
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, inspection)
	}
	state := "untrained"
	if inspection.Origin != nil {
		state = "downloaded"
	}
	if len(inspection.Model.Runs) > 0 {
		state = string(inspection.Model.Runs[len(inspection.Model.Runs)-1].State)
	}
	var consumed int64
	for _, run := range inspection.Runs {
		if run.Observation != nil {
			consumed += run.Observation.ConsumedTokens
		} else if run.Progress != nil {
			consumed += run.Progress.ConsumedTokens
		}
	}
	fmt.Fprintf(stdout, "NAME:          %s\n", inspection.Model.Name)
	fmt.Fprintf(stdout, "STATE:         %s\n", state)
	fmt.Fprintf(stdout, "MODEL ID:      %s\n", shortModelHash(inspection.Model.ID))
	fmt.Fprintf(stdout, "CREATED:       %s\n", inspection.Model.Created)
	fmt.Fprintf(stdout, "PARAMETERS:    %s\n", humanIntegerUint(inspection.Model.Forecast.ApproximateParameters))
	fmt.Fprintf(stdout, "WEIGHTS:       %s\n", humanBytesUint(inspection.Model.Forecast.ParameterBytes))
	fmt.Fprintf(stdout, "RUNS:          %s\n", humanInteger(int64(len(inspection.Model.Runs))))
	fmt.Fprintf(stdout, "TOKENS:        %s\n", humanCount(consumed))
	fmt.Fprintf(stdout, "ARCHITECTURE:  %s %s, %s layers, width %s, %s/%s heads\n",
		shortModelHash(inspection.Model.ArchitectureSHA256), inspection.Model.Architecture.Family,
		humanIntegerUint(inspection.Model.Architecture.Layers), humanIntegerUint(inspection.Model.Architecture.HiddenSize),
		humanIntegerUint(inspection.Model.Architecture.AttentionHeads), humanIntegerUint(inspection.Model.Architecture.KeyValueHeads))
	fmt.Fprintf(stdout, "TOKENIZER:     %s@%s\n", inspection.Model.Architecture.Tokenizer.Name, inspection.Model.Architecture.Tokenizer.Revision)
	if inspection.Origin != nil {
		fmt.Fprintf(stdout, "ORIGIN:        %s@%s (%s)\n", inspection.Origin.Source.Repository, shortModelHash(inspection.Origin.Source.Revision), inspection.Origin.Source.Provider)
	}
	for position, pin := range inspection.Model.Runs {
		tokens := int64(0)
		simulated := ""
		detail := ""
		if position < len(inspection.Runs) && inspection.Runs[position].Observation != nil {
			tokens = inspection.Runs[position].Observation.ConsumedTokens
			if inspection.Runs[position].Observation.Simulated {
				simulated = ", simulated"
			}
		} else if position < len(inspection.Runs) && inspection.Runs[position].Progress != nil {
			run := inspection.Runs[position]
			tokens = run.Progress.ConsumedTokens
			if len(run.Progress.Checkpoints) > 0 {
				detail = fmt.Sprintf(", checkpoint step %s", humanInteger(run.Progress.Checkpoints[len(run.Progress.Checkpoints)-1].Step))
			}
			if len(run.Attempts) > 1 {
				detail += fmt.Sprintf(", %s attempts", humanInteger(int64(len(run.Attempts))))
			}
		}
		fmt.Fprintf(stdout, "RUN %04d:      %-16s %-11s %s tokens%s%s\n", pin.Ordinal, pin.Stage, pin.State, humanCount(tokens), simulated, detail)
		if position < len(inspection.BOM.Runs) {
			runDirectory := filepath.Dir(filepath.FromSlash(inspection.BOM.Runs[position].RunBOM))
			fmt.Fprintf(stdout, "  TELEMETRY:   %s\n", filepath.Join(inspection.Path, runDirectory, model.TelemetryFilename))
		}
	}
	return nil
}

type modelAdviceOutput struct {
	Advice   model.Advice `json:"advice"`
	Provider string       `json:"provider,omitempty"`
	AIModel  string       `json:"ai_model,omitempty"`
	Response string       `json:"response,omitempty"`
}

func runModelAdvise(context Context, args []string, stdout, stderr io.Writer) error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	root, err := config.EffectiveModelRoot(configuration)
	if err != nil {
		return err
	}
	inspection, err := model.Inspect(root, args[0])
	if err != nil {
		return err
	}
	report, err := model.BuildAdvice(inspection, time.Now())
	if err != nil {
		return err
	}
	provider := stringOption(context, "provider")
	if provider == "" {
		provider = configuration.AI.Provider
	}
	aiModel := stringOption(context, "model")
	if aiModel == "" {
		aiModel = configuration.AI.Model
	}
	selected, err := waldoai.Select(provider, aiModel, waldoai.Credentials{APIKey: configuration.AI.APIKey}, nil)
	if err != nil {
		return err
	}
	output := modelAdviceOutput{Advice: report}
	if selected.Provider != waldoai.ProviderNone {
		output.Provider, output.AIModel = selected.Provider, selected.Model
		if !context.JSON {
			writeModelAdvice(stdout, output)
			fmt.Fprintf(stderr, "ai/advice              contacting %s/%s\n", selected.Provider, selected.Model)
		}
		question := ""
		if len(args) == 2 {
			question = args[1]
		}
		response, err := (waldoai.Client{}).Ask(context.Execution, selected, modelAdvicePrompt(report, question))
		if err != nil {
			return err
		}
		output.Response = response
		if !context.JSON {
			fmt.Fprintf(stdout, "\nAI ADVICE (%s/%s):\n%s\n", output.Provider, output.AIModel, output.Response)
			return nil
		}
	}
	if context.JSON {
		return writeJSON(stdout, output)
	}
	writeModelAdvice(stdout, output)
	return nil
}

func modelAdvicePrompt(report model.Advice, question string) string {
	data, _ := json.MarshalIndent(report, "", "  ")
	prompt := "You are advising an operator training an auditable language model with WALDO. Analyze only the supplied evidence. Distinguish observations from hypotheses. Recommend whether to let the run continue, inspect or fix it, or stop it; explain concrete compose changes only when supported. Never claim to have stopped or changed the run. Answer directly and concisely without extended deliberation.\n\nWALDO evidence:\n" + string(data)
	if question != "" {
		prompt += "\n\nOperator question:\n" + question
	}
	return prompt
}

func writeModelAdvice(stdout io.Writer, output modelAdviceOutput) {
	report := output.Advice
	fmt.Fprintf(stdout, "MODEL:         %s\n", report.Model)
	fmt.Fprintf(stdout, "STATE:         %s\n", report.State)
	fmt.Fprintf(stdout, "ACTION:        %s\n", report.Action)
	fmt.Fprintf(stdout, "ASSESSMENT:    %s\n", report.Summary)
	if report.Run != nil {
		run := report.Run
		fmt.Fprintf(stdout, "RUN:           %04d %s (%s)\n", run.Ordinal, run.Stage, run.ID)
		if run.PlannedSteps > 0 {
			fmt.Fprintf(stdout, "PROGRESS:      %.1f%% (%d/%d steps)\n", run.ProgressPercent, run.Step, run.PlannedSteps)
		}
		if run.Loss != nil {
			fmt.Fprintf(stdout, "LOSS:          %.6g\n", *run.Loss)
		}
		if run.HeldoutLoss != nil {
			fmt.Fprintf(stdout, "HELD-OUT LOSS: %.6g\n", *run.HeldoutLoss)
		}
		if run.TokensPerSecond > 0 {
			fmt.Fprintf(stdout, "THROUGHPUT:    %s tokens/s\n", humanCount(int64(run.TokensPerSecond)))
		}
		if run.ETASeconds > 0 {
			fmt.Fprintf(stdout, "ETA:           %s\n", compactDuration(run.ETASeconds))
		}
	}
	if len(report.Findings) > 0 {
		fmt.Fprintln(stdout, "\nFINDINGS:")
		for _, finding := range report.Findings {
			fmt.Fprintf(stdout, "  - %s\n", finding)
		}
	}
}

func runModelBOM(context Context, args []string, stdout, stderr io.Writer) error {
	options, err := cobraModelBOMOptions(context, args)
	if err != nil {
		return err
	}
	if options.Format == "eu-gpai" {
		return runModelEUGPAIBOM(context, options, stdout, stderr)
	}
	root, err := configuredModelRoot()
	if err != nil {
		return err
	}
	inspection, err := model.Inspect(root, options.Model)
	if err != nil {
		return err
	}
	if options.Output == "" {
		return writeJSON(stdout, inspection.BOM)
	}
	output, err := filepath.Abs(options.Output)
	if err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	if options.Force {
		flags = os.O_CREATE | os.O_TRUNC | os.O_WRONLY
	}
	file, err := os.OpenFile(output, flags, 0o644)
	if err != nil {
		return err
	}
	if err := writeJSON(file, inspection.BOM); err != nil {
		_ = file.Close()
		_ = os.Remove(output)
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Output string `json:"output"`
		}{output})
	}
	fmt.Fprintf(stdout, "wrote model OpenWALDO BOM to %s\n", output)
	return nil
}

func runModelTrain(context Context, args []string, stdout, stderr io.Writer) error {
	epochs := int64Option(context, "epochs")
	if epochs < 1 || epochs > 1_000_000 {
		return fmt.Errorf("--epochs must be an integer in 1..1000000")
	}
	name, paths := args[0], args[1:]
	root, err := configuredModelRoot()
	if err != nil {
		return err
	}
	inspection, err := model.Inspect(root, name)
	if err != nil {
		return err
	}
	builder, err := configuredModelBuilder(context, stderr)
	if err != nil {
		return err
	}
	if err := builder.CheckBackend(context.Execution, inspection.Model.Architecture, []string{"causal-language-modeling"}); err != nil {
		return err
	}
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return err
	}
	stage, err := prepareDefaultTrainingStage(context, inspection, paths, epochs, cache, stderr, boolOption(context, "audit"))
	if err != nil {
		return err
	}
	result, err := builder.Train(context.Execution, name, stage)
	if err != nil {
		return err
	}
	if _, err := cache.PurgeUsed(); err != nil {
		return fmt.Errorf("purge successful training scratch: %w", err)
	}
	return writeModelMutationResult(context, stdout, result, "trained")
}

func looksLikeIndexPath(value string) bool {
	return value == "." || value == ".." || value == "~" || strings.ContainsAny(value, `/\\`)
}

func runModelCompose(context Context, args []string, stdout, stderr io.Writer) error {
	name, path, replace := args[0], args[1], boolOption(context, "replace")
	compose, composePath, err := model.LoadCompose(path)
	if err != nil {
		return err
	}
	root, err := configuredModelRoot()
	if err != nil {
		return err
	}
	if !replace {
		exists, err := model.Exists(root, name)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("model %q already exists; use --replace to recreate it", name)
		}
	}
	builder, err := configuredModelBuilder(context, stderr)
	if err != nil {
		return err
	}
	objectives := make([]string, 0, len(compose.Stages))
	for _, stage := range compose.Stages {
		if !slices.Contains(objectives, stage.Objective) {
			objectives = append(objectives, stage.Objective)
		}
	}
	if err := builder.CheckBackend(context.Execution, compose.Architecture, objectives); err != nil {
		return err
	}
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return err
	}
	prepared := make([]model.PreparedStage, 0, len(compose.Stages))
	for _, stage := range compose.Stages {
		resolved, err := prepareModelStage(context, stage, cache, stderr, boolOption(context, "audit"))
		if err != nil {
			return err
		}
		prepared = append(prepared, resolved)
	}
	result, err := builder.Compose(context.Execution, name, compose, prepared, replace)
	if err != nil {
		return err
	}
	if _, err := cache.PurgeUsed(); err != nil {
		return fmt.Errorf("purge successful compose scratch: %w", err)
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Compose string           `json:"compose"`
			Result  model.Inspection `json:"result"`
		}{Compose: composePath, Result: result})
	}
	return writeModelMutationResult(context, stdout, result, "composed")
}

func runModelExport(context Context, args []string, stdout, stderr io.Writer) error {
	parsed, err := cobraModelExportOptions(context, args)
	if err != nil {
		return err
	}
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	root, err := config.EffectiveModelRoot(configuration)
	if err != nil {
		return err
	}
	inspection, err := model.Inspect(root, parsed.Name)
	if err != nil {
		return err
	}
	if configuration.Disclosure.Provider == "" {
		return fmt.Errorf("model export requires provider information; run waldo config set disclosure.provider <provider.json>")
	}
	provider, err := disclosure.LoadProvider(configuration.Disclosure.Provider)
	if err != nil {
		return fmt.Errorf("load configured disclosure.provider: %w", err)
	}
	report, err := disclosure.BuildEUGPAIReport(inspection, &provider, disclosure.ReleaseFromModel(inspection), time.Now())
	if err != nil {
		return err
	}
	if err := requireCompleteDisclosure(report, parsed.AllowIncomplete, stderr); err != nil {
		return err
	}
	euBOM, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	euBOM = append(euBOM, '\n')
	signed := signing.Configured(configuration.Signing)
	finalize := func(string) error { return nil }
	if signed {
		finalize = func(directory string) error {
			return signing.SignExport(context.Execution, configuration.Signing, directory, stderr)
		}
	}
	var output string
	var quantization *modelexport.Quantization
	var preparedCalibration *calibration.Prepared
	var cache *lookaside.Cache
	if parsed.Quant != "" {
		resolved, err := modelquant.ResolveProfile(parsed.Quant)
		if err != nil {
			return err
		}
		runtime, err := modelquant.ResolveRuntime(context.Execution, parsed.Calibration != "")
		if err != nil {
			return err
		}
		quantization = &modelexport.Quantization{Requested: parsed.Quant, Resolved: resolved, Quantizer: runtime}
		if parsed.Calibration != "" {
			cache, err = lookaside.DefaultCache()
			if err != nil {
				return err
			}
			targets, err := resolveIndexArguments(context.Execution, []string{parsed.Calibration}, stderr)
			if err != nil {
				return fmt.Errorf("calibration: %w", err)
			}
			policy, err := corpus.NewLicensePolicy(nil, nil)
			if err != nil {
				return err
			}
			bom, err := corpus.BuildBOM(context.Execution, targets, policy, cache)
			if err != nil {
				return fmt.Errorf("calibration: %w", err)
			}
			fmt.Fprintf(stderr, "calibration        resolved %s: %s shards, %s reference tokens\n", strings.Join(bom.Paths, ", "), humanInteger(bom.Totals.Shards), humanCount(bom.Totals.Tokens))
			prepared, err := calibration.Prepare(context.Execution, bom, cache, calibration.DefaultTokens, calibration.DefaultSeed, func(event calibration.Progress) {
				if event.Current == 1 || event.Current%25 == 0 || event.Current == event.Total {
					fmt.Fprintf(stderr, "calibration        shard %s/%s  %s\n", humanInteger(int64(event.Current)), humanInteger(int64(event.Total)), event.Shard[:12])
				}
			})
			if err != nil {
				return err
			}
			preparedCalibration = &prepared
			defer prepared.Cleanup()
			quantization.Calibration = &modelexport.Calibration{TextPath: prepared.TextPath, Profile: prepared.BOM.Profile, ReferenceTokens: prepared.BOM.Corpus.Tokens, SampledTokens: prepared.BOM.SampledTokens, Records: prepared.BOM.Records, Shards: len(prepared.BOM.Shards), SelectionSHA256: prepared.BOM.SelectionSHA256, Seed: prepared.BOM.Seed, Evidence: json.RawMessage(prepared.JSON)}
			fmt.Fprintf(stderr, "calibration        selected %s byte tokens from %s records in %s shards\n", humanCount(prepared.BOM.SampledTokens), humanCount(prepared.BOM.Records), humanInteger(int64(len(prepared.BOM.Shards))))
		}
	}
	switch parsed.Format {
	case "waldo":
		options := model.ExportOptions{Files: map[string][]byte{signing.EUBOM: euBOM}}
		if signed {
			options.Finalize = finalize
		}
		output, err = model.ExportPackage(root, parsed.Name, parsed.Destination, options)
	case "huggingface":
		options := modelexport.Options{EUBOM: euBOM}
		if signed {
			options.Finalize = finalize
		}
		output, err = modelexport.ExportHuggingFace(context.Execution, inspection, parsed.Destination, options)
	case "mlx":
		options := modelexport.Options{EUBOM: euBOM}
		if signed {
			options.Finalize = finalize
		}
		output, err = modelexport.ExportMLX(context.Execution, inspection, parsed.Destination, options)
	case "gguf":
		options := modelexport.Options{EUBOM: euBOM, Quantization: quantization, Report: func(message string) { fmt.Fprintln(stderr, "quantization      "+message) }}
		if signed {
			options.Finalize = finalize
		}
		output, err = modelexport.ExportGGUF(context.Execution, inspection, parsed.Destination, options)
	case "ollama":
		options := modelexport.Options{EUBOM: euBOM, Quantization: quantization, Report: func(message string) { fmt.Fprintln(stderr, "quantization      "+message) }}
		if signed {
			options.Finalize = finalize
		}
		output, err = modelexport.ExportOllama(context.Execution, inspection, parsed.Destination, options)
	}
	if err != nil {
		return err
	}
	if preparedCalibration != nil {
		if _, err := cache.PurgeUsed(); err != nil {
			return fmt.Errorf("purge successful calibration scratch: %w", err)
		}
	}
	if !signed {
		fmt.Fprintln(stderr, "warning: model export is unsigned; configure signing.* to sign exports automatically")
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Name   string `json:"name"`
			Format string `json:"format"`
			Output string `json:"output"`
			Signed bool   `json:"signed"`
		}{parsed.Name, parsed.Format, output, signed})
	}
	fmt.Fprintf(stdout, "exported %s model %s to %s\n", parsed.Format, parsed.Name, output)
	return nil
}

type modelExportOptions struct {
	Name            string
	Destination     string
	Format          string
	Quant           string
	Calibration     string
	AllowIncomplete bool
}

func cobraModelExportOptions(context Context, args []string) (modelExportOptions, error) {
	result := modelExportOptions{
		Name: args[0], Destination: args[1],
		Format: stringOption(context, "format"), Quant: stringOption(context, "quant"),
		Calibration: stringOption(context, "calibration"), AllowIncomplete: boolOption(context, "allow-incomplete"),
	}
	if result.Format != "waldo" && result.Format != "huggingface" && result.Format != "mlx" && result.Format != "gguf" && result.Format != "ollama" {
		return modelExportOptions{}, fmt.Errorf("model export format %q is not implemented; use waldo, huggingface, mlx, gguf, or ollama", result.Format)
	}
	if result.Quant != "" && result.Format != "gguf" && result.Format != "ollama" {
		return modelExportOptions{}, fmt.Errorf("--quant is supported only with --format gguf or ollama")
	}
	if result.Quant != "" {
		if _, err := modelquant.ResolveProfile(result.Quant); err != nil {
			return modelExportOptions{}, err
		}
	}
	if result.Calibration != "" && result.Quant == "" {
		return modelExportOptions{}, fmt.Errorf("--calibration requires --quant")
	}
	return result, nil
}

func runModelRemove(context Context, args []string, stdout, _ io.Writer) error {
	root, err := configuredModelRoot()
	if err != nil {
		return err
	}
	removed, err := model.Remove(root, args)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Removed []string `json:"removed"`
		}{removed})
	}
	for _, name := range removed {
		fmt.Fprintf(stdout, "removed model %s\n", name)
	}
	return nil
}

var openModelChat = inference.Open
var modelChatInput io.Reader = os.Stdin
var modelChatTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

func runModelChat(context Context, args []string, stdout, stderr io.Writer) error {
	name, prompt, options, err := cobraModelChatOptions(context, args)
	if err != nil {
		return err
	}
	root, err := configuredModelRoot()
	if err != nil {
		return err
	}
	inspection, err := model.Inspect(root, name)
	if err != nil {
		return err
	}
	if len(inspection.Model.Runs) == 0 && inspection.Origin == nil {
		return fmt.Errorf("model %q is untrained", name)
	}
	interactive := prompt == nil && modelChatTerminal()
	if context.JSON && interactive {
		return fmt.Errorf("--json requires a positional prompt or piped standard input")
	}
	if !interactive && prompt == nil {
		data, err := io.ReadAll(modelChatInput)
		if err != nil {
			return fmt.Errorf("read chat prompt from standard input: %w", err)
		}
		value := string(data)
		prompt = &value
	}
	if interactive {
		fmt.Fprintf(stderr, "loading model %s...\n", name)
	}
	opened, err := openModelChat(context.Execution, inspection)
	if err != nil {
		fmt.Fprintf(stderr, "warning: model chat unavailable: %v\n", err)
		return err
	}
	var chatErr error
	if interactive {
		chatErr = runInteractiveChat(context.Execution, opened, options, stdout)
	} else {
		chatErr = runOneShotChat(context, opened, *prompt, options, stdout)
	}
	return errors.Join(chatErr, opened.Session.Close())
}

func cobraModelChatOptions(context Context, args []string) (string, *string, inference.Options, error) {
	options := inference.Options{MaxTokens: intOption(context, "max-tokens"), Temperature: float64Option(context, "temperature"), TopP: float64Option(context, "top-p")}
	if optionChanged(context, "seed") {
		seed := uint64Option(context, "seed")
		options.Seed = &seed
	}
	if err := options.Validate(); err != nil {
		return "", nil, options, err
	}
	if len(args) == 2 {
		return args[0], &args[1], options, nil
	}
	return args[0], nil, options, nil
}

func runOneShotChat(context Context, opened inference.Opened, prompt string, options inference.Options, stdout io.Writer) error {
	var renderer safeTokenWriter
	if !context.JSON {
		renderer.writer = stdout
	}
	result, err := opened.Session.Generate(context.Execution, prompt, options, func(token inference.Token) error {
		if context.JSON {
			return nil
		}
		return renderer.Write(token.Bytes)
	})
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Model      string           `json:"model"`
			SourceType string           `json:"source_type"`
			SourceID   string           `json:"source_id"`
			RunID      string           `json:"run_id,omitempty"`
			Prompt     string           `json:"prompt"`
			Result     inference.Result `json:"result"`
		}{opened.Description.Model, opened.Description.SourceType, opened.Description.SourceID, opened.Description.RunID, prompt, result})
	}
	if err := renderer.Flush(); err != nil {
		return err
	}
	if !strings.HasSuffix(result.Text, "\n") {
		_, err = fmt.Fprintln(stdout)
	}
	return err
}

func runInteractiveChat(ctx context.Context, opened inference.Opened, options inference.Options, stdout io.Writer) error {
	fmt.Fprintf(stdout, "OpenWALDO model %s\n", opened.Description.Model)
	fmt.Fprintf(stdout, "Backend: %s\n", strings.ToUpper(opened.Description.Backend))
	fmt.Fprintf(stdout, "Context: %d tokens\n", opened.Description.ContextTokens)
	fmt.Fprintln(stdout, "Mode: raw causal continuation (this model has no chat template)")
	fmt.Fprintln(stdout, "Commands: /clear, /help, /exit")
	reader := bufio.NewReader(modelChatInput)
	history := ""
	for {
		fmt.Fprint(stdout, "\nyou> ")
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" && errors.Is(err, io.EOF) {
			fmt.Fprintln(stdout)
			return nil
		}
		switch line {
		case "/exit":
			return nil
		case "/clear":
			history = ""
			fmt.Fprintln(stdout, "context cleared")
			continue
		case "/help":
			fmt.Fprintln(stdout, "/clear resets context; /exit or Ctrl-D closes the session")
			continue
		}
		prompt := line
		if history != "" {
			prompt = history + "\n" + line
		}
		fmt.Fprintf(stdout, "%s> ", opened.Description.Model)
		renderer := safeTokenWriter{writer: stdout}
		result, generateErr := opened.Session.Generate(ctx, prompt, options, func(token inference.Token) error {
			return renderer.Write(token.Bytes)
		})
		if generateErr != nil {
			return generateErr
		}
		if err := renderer.Flush(); err != nil {
			return err
		}
		if !strings.HasSuffix(result.Text, "\n") {
			fmt.Fprintln(stdout)
		}
		history = boundChatHistory(prompt+result.Text, opened.Description.ContextTokens)
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func boundChatHistory(history string, contextTokens int) string {
	if contextTokens < 1 || len(history) <= contextTokens {
		return history
	}
	return strings.ToValidUTF8(history[len(history)-contextTokens:], "�")
}

func configuredModelRoot() (string, error) {
	configuration, err := config.Load()
	if err != nil {
		return "", err
	}
	return config.EffectiveModelRoot(configuration)
}

func configuredModelBuilder(commandContext Context, progress io.Writer) (model.Builder, error) {
	configuration, err := config.Load()
	if err != nil {
		return model.Builder{}, err
	}
	root, err := config.EffectiveModelRoot(configuration)
	if err != nil {
		return model.Builder{}, err
	}
	builder := model.Builder{Root: root, Progress: func(event model.Progress) {
		if commandContext.JSON {
			_ = json.NewEncoder(progress).Encode(event)
			return
		}
		label := event.Phase
		if event.Stage != "" {
			label += "/" + event.Stage
		}
		message := modelProgressMessage(event)
		if event.State != "" {
			fmt.Fprintf(progress, "%-22s %-11s %s\n", label, event.State, message)
		} else {
			fmt.Fprintf(progress, "%-22s %s\n", label, message)
		}
	}}
	backend := config.EffectiveModelBackend(configuration)
	resolver := training.NewEnvironmentResolver(backend)
	builder.Resolver = training.ResolverFunc(func(execution context.Context, request training.ResolveRequest) (training.Selection, error) {
		selection, err := resolver.Resolve(execution, request)
		if err != nil {
			if commandContext.JSON {
				_ = json.NewEncoder(progress).Encode(model.Progress{Phase: "backend", State: "unavailable", Message: err.Error()})
			}
		}
		return selection, err
	})
	return builder, nil
}

func modelProgressMessage(event model.Progress) string {
	if event.Training == nil || event.Training.Kind != "progress" || event.Training.Step <= 1 || event.Training.ETASeconds <= 0 {
		return event.Message
	}
	return fmt.Sprintf("%s, ETA %s", event.Message, compactDuration(event.Training.ETASeconds))
}

func compactDuration(seconds int64) string {
	days := seconds / (24 * 60 * 60)
	seconds %= 24 * 60 * 60
	hours := seconds / (60 * 60)
	seconds %= 60 * 60
	minutes := seconds / 60
	seconds %= 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

func prepareDefaultTrainingStage(context Context, inspection model.Inspection, paths []string, epochs int64, cache *lookaside.Cache, progress io.Writer, audit bool) (model.PreparedStage, error) {
	architecture := inspection.Model.Architecture
	if architecture.Tokenizer.Name != "byte" || architecture.Tokenizer.Revision != "builtin-byte-schema-1" || architecture.VocabularySize != 259 {
		return model.PreparedStage{}, fmt.Errorf("automatic one-pass training currently requires byte@builtin-byte-schema-1 with vocabulary_size 259")
	}
	targets, err := resolveIndexArgumentsWithWarning(context.Execution, paths, progress)
	if err != nil {
		return model.PreparedStage{}, err
	}
	policy, err := corpus.NewLicensePolicy(nil, nil)
	if err != nil {
		return model.PreparedStage{}, err
	}
	bom, err := corpus.BuildBOM(context.Execution, targets, policy, cache)
	if err != nil {
		return model.PreparedStage{}, err
	}
	batch := int64(8)
	sequence := int64(inspection.Model.Architecture.ContextTokens)
	stageName := fmt.Sprintf("train-%04d", len(inspection.Model.Runs)+1)
	if len(inspection.Model.Runs) > 0 {
		last := inspection.Model.Runs[len(inspection.Model.Runs)-1]
		if last.State == model.RunInterrupted && strings.HasPrefix(last.Stage, "train-") {
			stageName = last.Stage
		}
	}
	stage := model.Stage{
		Name: stageName, Type: "pre-training",
		Objective: "causal-language-modeling", Corpora: append([]string(nil), paths...),
		Parameters: training.Parameters{Epochs: epochs, Steps: 1, BatchSize: batch, SequenceLength: sequence, LearningRate: 0.0003, Seed: 42},
	}
	prepared, err := materializeModelStage(context, stage, bom, cache, progress, audit)
	if err != nil {
		return model.PreparedStage{}, err
	}
	resolved, err := training.ResolveParameters(prepared.Stage.Parameters)
	if err != nil {
		return model.PreparedStage{}, err
	}
	partition, err := training.NewRecordPartitionContext(context.Execution, prepared.Inputs, resolved, nil)
	if err != nil {
		return model.PreparedStage{}, err
	}
	tokenTargets, err := partition.TrainingByteTargets(context.Execution)
	if err != nil {
		return model.PreparedStage{}, err
	}
	capacity := batch * sequence
	steps := tokenTargets / capacity
	if tokenTargets%capacity != 0 {
		steps++
	}
	prepared.Stage.Parameters.Steps = steps
	epochLabel := "epochs"
	if epochs == 1 {
		epochLabel = "epoch"
	}
	fmt.Fprintf(progress, "preflight/%s          training %s %s, %s byte targets, %s optimizer steps (%s × %s tokens); held out %s records\n", stage.Name, humanInteger(epochs), epochLabel, humanCount(tokenTargets), humanInteger(steps), humanInteger(batch), humanInteger(sequence), humanInteger(partition.Evaluation.Records))
	return model.PrepareStage(prepared.Stage, prepared.BOM, prepared.Inputs)
}

func prepareModelStage(context Context, stage model.Stage, cache *lookaside.Cache, progress io.Writer, audit bool) (model.PreparedStage, error) {
	targets, err := resolveIndexArguments(context.Execution, stage.Corpora, progress)
	if err != nil {
		return model.PreparedStage{}, fmt.Errorf("stage %s: %w", stage.Name, err)
	}
	policy, err := corpus.NewLicensePolicy(nil, nil)
	if err != nil {
		return model.PreparedStage{}, err
	}
	bom, err := corpus.BuildBOM(context.Execution, targets, policy, cache)
	if err != nil {
		return model.PreparedStage{}, fmt.Errorf("stage %s: %w", stage.Name, err)
	}
	return materializeModelStage(context, stage, bom, cache, progress, audit)
}

func materializeModelStage(context Context, stage model.Stage, bom corpus.BOM, cache *lookaside.Cache, progress io.Writer, audit bool) (model.PreparedStage, error) {
	fmt.Fprintf(progress, "preflight/%s          resolving %s shards, %s records, %s reference tokens\n", stage.Name, humanInteger(bom.Totals.Shards), humanCount(bom.Totals.Docs), humanCount(bom.Totals.Tokens))
	materialized, err := corpus.Materialize(context.Execution, bom, cache, modelMaterializeProgressPrinter(progress))
	if err != nil {
		return model.PreparedStage{}, err
	}
	seen := map[string]bool{}
	var paths []string
	var inputs []training.Input
	for _, object := range materialized.Objects {
		if seen[object.Shard.SHA256] {
			continue
		}
		seen[object.Shard.SHA256] = true
		paths = append(paths, object.Path)
		inputs = append(inputs, training.Input{Path: object.Path, SHA256: object.Shard.SHA256, Bytes: object.Shard.Bytes})
	}
	if audit {
		fmt.Fprintf(progress, "preflight/%s          auditing %s materialized shards\n", stage.Name, humanInteger(int64(len(paths))))
		audited, err := shard.VerifyWithOptions(context.Execution, paths, shard.AuditOptions{Progress: auditProgressPrinter(progress)})
		if err != nil {
			return model.PreparedStage{}, fmt.Errorf("stage %s shard audit: %w", stage.Name, err)
		}
		if audited.Records != bom.Totals.Docs || audited.Tokens != bom.Totals.Tokens || audited.EncodedBytes != bom.Totals.Bytes {
			return model.PreparedStage{}, fmt.Errorf("stage %s audited totals differ from index manifests", stage.Name)
		}
		if err := corpus.AttachShardAttestations(&bom, materialized.Objects); err != nil {
			return model.PreparedStage{}, fmt.Errorf("stage %s shard BOM evidence: %w", stage.Name, err)
		}
	}
	return model.PrepareStage(stage, bom, inputs)
}

func modelMaterializeProgressPrinter(output io.Writer) func(corpus.MaterializeProgress) {
	type terminalWriter interface{ Fd() uintptr }
	terminal := false
	if writer, ok := output.(terminalWriter); ok {
		terminal = term.IsTerminal(int(writer.Fd()))
	}
	lastUpdate := time.Time{}
	return func(event corpus.MaterializeProgress) {
		if !terminal {
			if event.Phase == "complete" {
				fmt.Fprintf(output, "  materialized %s/%s  %s/%s  %s\n",
					humanInteger(int64(event.Current)), humanInteger(int64(event.Total)),
					humanBytes(event.Bytes), humanBytes(event.TotalBytes), event.Shard.SHA256[:12])
			}
			return
		}
		now := time.Now()
		if event.Phase != "complete" && !lastUpdate.IsZero() && now.Sub(lastUpdate) < 100*time.Millisecond {
			return
		}
		lastUpdate = now
		const width = 24
		filled := 0
		if event.TotalBytes > 0 {
			filled = int(event.Bytes * width / event.TotalBytes)
			if filled > width {
				filled = width
			}
		}
		phase := event.Phase
		if phase == "complete" {
			phase = "verified"
		}
		fmt.Fprintf(output, "\r\x1b[K  materialize [%-24s] %3d%%  %s/%s  %s/%s  %-8s %s",
			strings.Repeat("=", filled), percentage(event.Bytes, event.TotalBytes),
			humanInteger(int64(event.Current)), humanInteger(int64(event.Total)),
			humanBytes(event.Bytes), humanBytes(event.TotalBytes), phase, event.Shard.SHA256[:12])
		if event.Phase == "complete" && event.Current == event.Total {
			fmt.Fprintln(output)
		}
	}
}

func percentage(current, total int64) int64 {
	if total <= 0 {
		return 0
	}
	value := current * 100 / total
	if value > 100 {
		return 100
	}
	return value
}

func writeModelMutationResult(context Context, stdout io.Writer, inspection model.Inspection, verb string) error {
	if context.JSON {
		return writeJSON(stdout, inspection)
	}
	fmt.Fprintf(stdout, "%s model %s\n", verb, inspection.Model.Name)
	fmt.Fprintf(stdout, "  location      %s\n", inspection.Path)
	fmt.Fprintf(stdout, "  model id      %s\n", shortModelHash(inspection.Model.ID))
	fmt.Fprintf(stdout, "  runs          %s\n", humanInteger(int64(len(inspection.Model.Runs))))
	if len(inspection.RunBOMs) > 0 {
		backend := inspection.RunBOMs[len(inspection.RunBOMs)-1].Execution.Backend
		fmt.Fprintf(stdout, "  backend       %s@%s\n", backend.Name, backend.Revision)
		if backend.Name == "fake" {
			fmt.Fprintln(stdout, "  warning       explicitly simulated; artifacts are not trained model weights")
		}
	}
	return nil
}

func shortModelHash(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func humanIntegerUint(value uint64) string {
	if value <= uint64(^uint64(0)>>1) {
		return humanInteger(int64(value))
	}
	return fmt.Sprintf("%d", value)
}

func humanBytesUint(value uint64) string {
	if value <= uint64(^uint64(0)>>1) {
		return humanBytes(int64(value))
	}
	return fmt.Sprintf("%d B", value)
}
