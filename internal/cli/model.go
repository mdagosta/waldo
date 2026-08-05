package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/openwaldo/waldo/internal/config"
	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/disclosure"
	"github.com/openwaldo/waldo/internal/inference"
	"github.com/openwaldo/waldo/internal/lookaside"
	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/modelexport"
	"github.com/openwaldo/waldo/internal/shard"
	"github.com/openwaldo/waldo/internal/signing"
	"github.com/openwaldo/waldo/internal/training"
	"golang.org/x/term"
)

func runModelForecast(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return usageError{message: "usage: waldo model forecast <compose.yaml> | <index-path...> [--json]"}
	}
	if len(args) == 1 {
		isCompose, err := model.IsComposeFile(args[0])
		if err != nil {
			return err
		}
		if isCompose {
			return runModelComposeForecast(context, args[0], stdout)
		}
	}
	return runModelIndexForecast(context, args, stdout)
}

func runModelComposeForecast(context Context, path string, stdout io.Writer) error {
	compose, composePath, err := model.LoadCompose(path)
	if err != nil {
		return err
	}
	report, err := model.ForecastCompose(compose)
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

func runModelIndexForecast(context Context, paths []string, stdout io.Writer) error {
	targets, err := resolveIndexArguments(paths)
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
	preset, report, err := model.ForecastIndexSelection(bom.Totals.Tokens)
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
	manufacturerWidth, acceleratorWidth := len("MFR"), len("ACCELERATOR")
	GPUsWidth, memoryWidth, durationWidth := len("GPUS"), len("MEMORY/GPU"), len("APPROX. TIME")
	for _, configuration := range report.Configurations {
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
	fmt.Fprintf(stdout, "%*s  %-*s  %-*s  %*s  %*s\n", GPUsWidth, "GPUS", manufacturerWidth, "MFR", acceleratorWidth, "ACCELERATOR", memoryWidth, "MEMORY/GPU", durationWidth, "APPROX. TIME")
	for _, candidate := range rows {
		fmt.Fprintf(stdout, "%*s  %-*s  %-*s  %*s  %*s\n", GPUsWidth, candidate.GPUs, manufacturerWidth, candidate.manufacturer, acceleratorWidth, candidate.accelerator, memoryWidth, candidate.memory, durationWidth, candidate.duration)
	}
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
	name, presetName, err := parseModelInit(args)
	if err != nil {
		return err
	}
	preset, err := model.PresetByName(presetName)
	if err != nil {
		return usageError{message: err.Error()}
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

func parseModelInit(args []string) (string, string, error) {
	var positionals []string
	preset := ""
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--preset":
			value, next, err := optionValue(args, index, "--preset")
			if err != nil {
				return "", "", err
			}
			preset, index = value, next
		case strings.HasPrefix(argument, "--preset="):
			preset = strings.TrimPrefix(argument, "--preset=")
		case strings.HasPrefix(argument, "-"):
			return "", "", usageError{message: fmt.Sprintf("unknown model init option %q", argument)}
		default:
			positionals = append(positionals, argument)
		}
	}
	if len(positionals) != 1 || preset == "" {
		return "", "", usageError{message: "usage: waldo model init <name> --preset <preset>"}
	}
	return positionals[0], preset, nil
}

func runModelList(context Context, args []string, stdout, _ io.Writer) error {
	for _, argument := range args {
		if strings.HasPrefix(argument, "-") {
			return usageError{message: fmt.Sprintf("unknown model list option %q", argument)}
		}
	}
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
	if len(args) != 1 {
		return usageError{message: "usage: waldo model summary <name> [--json]"}
	}
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
	if len(inspection.Model.Runs) > 0 {
		state = string(inspection.Model.Runs[len(inspection.Model.Runs)-1].State)
	}
	var consumed int64
	for _, run := range inspection.Runs {
		if run.Observation != nil {
			consumed += run.Observation.ConsumedTokens
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
	for position, pin := range inspection.Model.Runs {
		tokens := int64(0)
		simulated := ""
		if position < len(inspection.Runs) && inspection.Runs[position].Observation != nil {
			tokens = inspection.Runs[position].Observation.ConsumedTokens
			if inspection.Runs[position].Observation.Simulated {
				simulated = ", simulated"
			}
		}
		fmt.Fprintf(stdout, "RUN %04d:      %-16s %-11s %s tokens%s\n", pin.Ordinal, pin.Stage, pin.State, humanCount(tokens), simulated)
	}
	return nil
}

func runModelBOM(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return usageError{message: "usage: waldo model bom <name> [output.json] [--json]"}
	}
	root, err := configuredModelRoot()
	if err != nil {
		return err
	}
	inspection, err := model.Inspect(root, args[0])
	if err != nil {
		return err
	}
	if len(args) == 1 {
		return writeJSON(stdout, inspection.BOM)
	}
	output, err := filepath.Abs(args[1])
	if err != nil {
		return err
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
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
	name, paths, epochs, err := parseModelTrain(args)
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
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return err
	}
	stage, err := prepareDefaultTrainingStage(context, inspection, paths, epochs, cache, stderr)
	if err != nil {
		return err
	}
	builder, err := configuredModelBuilder(context, stderr)
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

func parseModelTrain(args []string) (string, []string, int64, error) {
	epochs := int64(1)
	var positionals []string
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--epochs":
			value, next, err := optionValue(args, index, "--epochs")
			if err != nil {
				return "", nil, 0, err
			}
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil || parsed < 1 || parsed > 1_000_000 {
				return "", nil, 0, usageError{message: "--epochs must be an integer in 1..1000000"}
			}
			epochs = parsed
			index = next
		case strings.HasPrefix(argument, "-"):
			return "", nil, 0, usageError{message: fmt.Sprintf("unknown model train option %q", argument)}
		default:
			positionals = append(positionals, argument)
		}
	}
	if len(positionals) < 2 {
		return "", nil, 0, usageError{message: "usage: waldo model train <name> <index-path...> [--epochs <n>] [--json]"}
	}
	return positionals[0], positionals[1:], epochs, nil
}

func runModelCompose(context Context, args []string, stdout, stderr io.Writer) error {
	name, path, replace, err := parseModelCompose(args)
	if err != nil {
		return err
	}
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
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return err
	}
	prepared := make([]model.PreparedStage, 0, len(compose.Stages))
	for _, stage := range compose.Stages {
		resolved, err := prepareModelStage(context, stage, cache, stderr)
		if err != nil {
			return err
		}
		prepared = append(prepared, resolved)
	}
	builder, err := configuredModelBuilder(context, stderr)
	if err != nil {
		return err
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

func parseModelCompose(args []string) (string, string, bool, error) {
	var positionals []string
	replace := false
	for _, argument := range args {
		switch {
		case argument == "--replace":
			replace = true
		case strings.HasPrefix(argument, "-"):
			return "", "", false, usageError{message: fmt.Sprintf("unknown model compose option %q", argument)}
		default:
			positionals = append(positionals, argument)
		}
	}
	if len(positionals) != 2 {
		return "", "", false, usageError{message: "usage: waldo model compose <name> <compose-file> [--replace] [--json]"}
	}
	return positionals[0], positionals[1], replace, nil
}

func runModelExport(context Context, args []string, stdout, stderr io.Writer) error {
	name, destination, format, allowIncomplete, err := parseModelExport(args)
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
	inspection, err := model.Inspect(root, name)
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
	if err := requireCompleteDisclosure(report, allowIncomplete, stderr); err != nil {
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
	switch format {
	case "waldo":
		options := model.ExportOptions{Files: map[string][]byte{signing.EUBOM: euBOM}}
		if signed {
			options.Finalize = finalize
		}
		output, err = model.ExportPackage(root, name, destination, options)
	case "huggingface":
		options := modelexport.Options{EUBOM: euBOM}
		if signed {
			options.Finalize = finalize
		}
		output, err = modelexport.ExportHuggingFace(context.Execution, inspection, destination, options)
	case "mlx":
		options := modelexport.Options{EUBOM: euBOM}
		if signed {
			options.Finalize = finalize
		}
		output, err = modelexport.ExportMLX(context.Execution, inspection, destination, options)
	case "gguf":
		options := modelexport.Options{EUBOM: euBOM}
		if signed {
			options.Finalize = finalize
		}
		output, err = modelexport.ExportGGUF(context.Execution, inspection, destination, options)
	case "ollama":
		options := modelexport.Options{EUBOM: euBOM}
		if signed {
			options.Finalize = finalize
		}
		output, err = modelexport.ExportOllama(context.Execution, inspection, destination, options)
	}
	if err != nil {
		return err
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
		}{name, format, output, signed})
	}
	fmt.Fprintf(stdout, "exported %s model %s to %s\n", format, name, output)
	return nil
}

func parseModelExport(args []string) (string, string, string, bool, error) {
	format := "waldo"
	allowIncomplete := false
	var positionals []string
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--format":
			value, next, err := optionValue(args, index, "--format")
			if err != nil {
				return "", "", "", false, err
			}
			format, index = value, next
		case strings.HasPrefix(argument, "--format="):
			format = strings.TrimPrefix(argument, "--format=")
		case argument == "--allow-incomplete":
			allowIncomplete = true
		case strings.HasPrefix(argument, "-"):
			return "", "", "", false, usageError{message: fmt.Sprintf("unknown model export option %q", argument)}
		default:
			positionals = append(positionals, argument)
		}
	}
	if len(positionals) != 2 {
		return "", "", "", false, usageError{message: "usage: waldo model export <name> <directory> [--format waldo|huggingface|mlx|gguf|ollama] [--allow-incomplete] [--json]"}
	}
	if format != "waldo" && format != "huggingface" && format != "mlx" && format != "gguf" && format != "ollama" {
		return "", "", "", false, usageError{message: fmt.Sprintf("model export format %q is not implemented; use waldo, huggingface, mlx, gguf, or ollama", format)}
	}
	return positionals[0], positionals[1], format, allowIncomplete, nil
}

func runModelRemove(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return usageError{message: "usage: waldo model rm <name...> [--json]"}
	}
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
	name, prompt, options, err := parseModelChat(args)
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
	if len(inspection.Model.Runs) == 0 {
		return fmt.Errorf("model %q is untrained", name)
	}
	interactive := prompt == nil && modelChatTerminal()
	if context.JSON && interactive {
		return usageError{message: "--json requires a positional prompt or piped standard input"}
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

func parseModelChat(args []string) (string, *string, inference.Options, error) {
	options := inference.Options{MaxTokens: 256, Temperature: 0.8, TopP: 0.95}
	var positionals []string
	for index := 0; index < len(args); index++ {
		argument := args[index]
		var value string
		var err error
		switch argument {
		case "--max-tokens", "--temperature", "--top-p", "--seed":
			value, index, err = optionValue(args, index, argument)
			if err != nil {
				return "", nil, options, err
			}
		default:
			if strings.HasPrefix(argument, "-") {
				return "", nil, options, usageError{message: fmt.Sprintf("unknown model chat option %q", argument)}
			}
			positionals = append(positionals, argument)
			continue
		}
		switch argument {
		case "--max-tokens":
			options.MaxTokens, err = strconv.Atoi(value)
		case "--temperature":
			options.Temperature, err = strconv.ParseFloat(value, 64)
		case "--top-p":
			options.TopP, err = strconv.ParseFloat(value, 64)
		case "--seed":
			seed, parseErr := strconv.ParseUint(value, 10, 64)
			err = parseErr
			options.Seed = &seed
		}
		if err != nil {
			return "", nil, options, usageError{message: fmt.Sprintf("invalid %s value %q", argument, value)}
		}
	}
	if len(positionals) < 1 || len(positionals) > 2 {
		return "", nil, options, usageError{message: "usage: waldo model chat <name> [prompt] [--max-tokens <n>] [--temperature <n>] [--top-p <n>] [--seed <n>] [--json]"}
	}
	if err := options.Validate(); err != nil {
		return "", nil, options, usageError{message: err.Error()}
	}
	if len(positionals) == 2 {
		return positionals[0], &positionals[1], options, nil
	}
	return positionals[0], nil, options, nil
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
			Model  string           `json:"model"`
			RunID  string           `json:"run_id"`
			Prompt string           `json:"prompt"`
			Result inference.Result `json:"result"`
		}{opened.Description.Model, opened.Description.RunID, prompt, result})
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
		if event.State != "" {
			fmt.Fprintf(progress, "%-22s %-11s %s\n", label, event.State, event.Message)
		} else {
			fmt.Fprintf(progress, "%-22s %s\n", label, event.Message)
		}
	}}
	backend := config.EffectiveModelBackend(configuration)
	resolver := training.NewEnvironmentResolver(backend)
	builder.Resolver = training.ResolverFunc(func(execution context.Context, request training.ResolveRequest) (training.Selection, error) {
		selection, err := resolver.Resolve(execution, request)
		if err != nil {
			if commandContext.JSON {
				_ = json.NewEncoder(progress).Encode(model.Progress{Phase: "backend", State: "unavailable", Message: err.Error()})
			} else {
				fmt.Fprintf(progress, "warning: training backend unavailable: %v\n", err)
			}
		}
		return selection, err
	})
	return builder, nil
}

func prepareDefaultTrainingStage(context Context, inspection model.Inspection, paths []string, epochs int64, cache *lookaside.Cache, progress io.Writer) (model.PreparedStage, error) {
	architecture := inspection.Model.Architecture
	if architecture.Tokenizer.Name != "byte" || architecture.Tokenizer.Revision != "builtin-byte-schema-1" || architecture.VocabularySize != 259 {
		return model.PreparedStage{}, fmt.Errorf("automatic one-pass training currently requires byte@builtin-byte-schema-1 with vocabulary_size 259")
	}
	targets, err := resolveIndexArguments(paths)
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
	stage := model.Stage{
		Name: fmt.Sprintf("train-%04d", len(inspection.Model.Runs)+1), Type: "pre-training",
		Objective: "causal-language-modeling", Corpora: append([]string(nil), paths...),
		Parameters: training.Parameters{Epochs: epochs, Steps: 1, BatchSize: batch, SequenceLength: sequence, LearningRate: 0.0003, Seed: 42},
	}
	prepared, err := materializeModelStage(context, stage, bom, cache, progress)
	if err != nil {
		return model.PreparedStage{}, err
	}
	oneEpochTargets, err := training.CountByteTargets(context.Execution, prepared.Inputs)
	if err != nil {
		return model.PreparedStage{}, err
	}
	tokenTargets, err := training.ByteTargetsForEpochs(oneEpochTargets, epochs)
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
	fmt.Fprintf(progress, "preflight/%s          training %s %s, %s byte targets, %s optimizer steps (%s × %s tokens)\n", stage.Name, humanInteger(epochs), epochLabel, humanCount(tokenTargets), humanInteger(steps), humanInteger(batch), humanInteger(sequence))
	return model.PrepareStage(prepared.Stage, prepared.BOM, prepared.Inputs)
}

func prepareModelStage(context Context, stage model.Stage, cache *lookaside.Cache, progress io.Writer) (model.PreparedStage, error) {
	targets, err := resolveIndexArguments(stage.Corpora)
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
	return materializeModelStage(context, stage, bom, cache, progress)
}

func materializeModelStage(context Context, stage model.Stage, bom corpus.BOM, cache *lookaside.Cache, progress io.Writer) (model.PreparedStage, error) {
	fmt.Fprintf(progress, "preflight/%s          resolving %s shards, %s records, %s reference tokens\n", stage.Name, humanInteger(bom.Totals.Shards), humanCount(bom.Totals.Docs), humanCount(bom.Totals.Tokens))
	materialized, err := corpus.Materialize(context.Execution, bom, cache, func(event corpus.MaterializeProgress) {
		if event.Current == 1 || event.Current == event.Total || event.Current%25 == 0 {
			fmt.Fprintf(progress, "  shard %s/%s  %s\n", humanInteger(int64(event.Current)), humanInteger(int64(event.Total)), event.Shard.SHA256[:12])
		}
	})
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
	audited, err := shard.Audit(context.Execution, paths)
	if err != nil {
		return model.PreparedStage{}, fmt.Errorf("stage %s shard audit: %w", stage.Name, err)
	}
	if audited.Records != bom.Totals.Docs || audited.Tokens != bom.Totals.Tokens || audited.EncodedBytes != bom.Totals.Bytes {
		return model.PreparedStage{}, fmt.Errorf("stage %s audited totals differ from index manifests", stage.Name)
	}
	return model.PrepareStage(stage, bom, inputs)
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
