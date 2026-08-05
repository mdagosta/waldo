package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/openwaldo/waldo-new/internal/config"
	"github.com/openwaldo/waldo-new/internal/corpus"
	"github.com/openwaldo/waldo-new/internal/lookaside"
	"github.com/openwaldo/waldo-new/internal/model"
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

func runModelBuild(context Context, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return usageError{message: "usage: waldo model build <compose.yaml> [--json]"}
	}
	compose, composePath, err := model.LoadCompose(args[0])
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
	builder := model.Builder{
		Root: root,
		Progress: func(progress model.Progress) {
			if context.JSON {
				_ = json.NewEncoder(stderr).Encode(progress)
				return
			}
			label := progress.Phase
			if progress.Stage != "" {
				label += "/" + progress.Stage
			}
			if progress.State != "" {
				fmt.Fprintf(stderr, "%-22s %-11s %s\n", label, progress.State, progress.Message)
			} else {
				fmt.Fprintf(stderr, "%-22s %s\n", label, progress.Message)
			}
		},
	}
	inspection, err := builder.Build(context.Execution, compose)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Compose string           `json:"compose"`
			Result  model.Inspection `json:"result"`
		}{Compose: composePath, Result: inspection})
	}
	fmt.Fprintf(stdout, "model %s built with simulated training\n", inspection.Model.Name)
	fmt.Fprintf(stdout, "  location      %s\n", inspection.Path)
	fmt.Fprintf(stdout, "  model id      %s\n", shortModelHash(inspection.Model.ID))
	fmt.Fprintf(stdout, "  architecture  %s\n", shortModelHash(inspection.Model.ArchitectureSHA256))
	fmt.Fprintf(stdout, "  estimate      %s parameters, %s weights\n", humanIntegerUint(inspection.Model.Forecast.ApproximateParameters), humanBytesUint(inspection.Model.Forecast.ParameterBytes))
	fmt.Fprintf(stdout, "  runs          %s complete\n", humanInteger(int64(len(inspection.Model.Runs))))
	fmt.Fprintln(stdout, "  warning       fake backend only; artifacts are not trained model weights")
	return nil
}

func runModelInspect(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) != 1 {
		return usageError{message: "usage: waldo model inspect <name-or-path> [--json]"}
	}
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
	if context.JSON {
		return writeJSON(stdout, inspection)
	}
	fmt.Fprintf(stdout, "model %s\n", inspection.Model.Name)
	fmt.Fprintf(stdout, "  location      %s\n", inspection.Path)
	fmt.Fprintf(stdout, "  model id      %s\n", shortModelHash(inspection.Model.ID))
	fmt.Fprintf(stdout, "  created       %s\n", inspection.Model.Created)
	fmt.Fprintf(stdout, "  architecture  %s %s, %s layers, width %s, %s/%s heads\n",
		shortModelHash(inspection.Model.ArchitectureSHA256), inspection.Model.Architecture.Family,
		humanIntegerUint(inspection.Model.Architecture.Layers), humanIntegerUint(inspection.Model.Architecture.HiddenSize),
		humanIntegerUint(inspection.Model.Architecture.AttentionHeads), humanIntegerUint(inspection.Model.Architecture.KeyValueHeads))
	fmt.Fprintf(stdout, "  tokenizer     %s@%s\n", inspection.Model.Architecture.Tokenizer.Name, inspection.Model.Architecture.Tokenizer.Revision)
	fmt.Fprintf(stdout, "  estimate      %s parameters, %s weights\n", humanIntegerUint(inspection.Model.Forecast.ApproximateParameters), humanBytesUint(inspection.Model.Forecast.ParameterBytes))
	fmt.Fprintf(stdout, "  runs          %s\n", humanInteger(int64(len(inspection.Model.Runs))))
	for position, pin := range inspection.Model.Runs {
		tokens := int64(0)
		simulated := ""
		if position < len(inspection.Runs) && inspection.Runs[position].Observation != nil {
			tokens = inspection.Runs[position].Observation.ConsumedTokens
			if inspection.Runs[position].Observation.Simulated {
				simulated = ", simulated"
			}
		}
		fmt.Fprintf(stdout, "    %04d %-16s %-11s %s tokens%s\n", pin.Ordinal, pin.Stage, pin.State, humanCount(tokens), simulated)
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
