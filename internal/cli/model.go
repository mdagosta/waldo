package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/openwaldo/waldo-new/internal/config"
	"github.com/openwaldo/waldo-new/internal/model"
)

func runModelBuild(context Context, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return usageError{message: "usage: waldo model build <recipe.yaml> [--json]"}
	}
	recipe, recipePath, err := model.LoadRecipe(args[0])
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
	inspection, err := builder.Build(context.Execution, recipe)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Recipe string           `json:"recipe"`
			Result model.Inspection `json:"result"`
		}{Recipe: recipePath, Result: inspection})
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
