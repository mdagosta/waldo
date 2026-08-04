package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/openwaldo/waldo-new/internal/corpus"
	waldoindex "github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/lookaside"
	"github.com/openwaldo/waldo-new/internal/provenance"
)

type exportOptions struct {
	Paths   []string
	Output  string
	Include []string
	Exclude []string
	Force   bool
}

func runIndexExport(context Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseExportOptions(args)
	if err != nil {
		return err
	}
	policy, err := corpus.NewLicensePolicy(options.Include, options.Exclude)
	if err != nil {
		return usageError{message: err.Error()}
	}
	targets := make([]waldoindex.Target, 0, len(options.Paths))
	for _, path := range options.Paths {
		target, err := waldoindex.Resolve(context.IndexPath, path)
		if err != nil {
			return err
		}
		targets = append(targets, target)
	}
	bom, err := corpus.BuildBOM(targets, policy)
	if err != nil {
		return err
	}
	if len(bom.Shards) == 0 {
		return fmt.Errorf("the selected paths and license policy contain no shards")
	}
	if err := provenance.CheckCorpusExportDestination(options.Output, bom); err != nil {
		return err
	}
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "exporting %s shards, %s docs, %s tokens, %s through %s\n",
		humanInteger(bom.Totals.Shards), humanCount(bom.Totals.Docs),
		humanCount(bom.Totals.Tokens), humanBytes(bom.Totals.Bytes), cache.Root())
	materialized, err := corpus.Materialize(context.Execution, bom, cache, func(event corpus.MaterializeProgress) {
		if event.Current == 1 || event.Current == event.Total || event.Current%25 == 0 {
			fmt.Fprintf(stderr, "  verify %s/%s  %s\n", humanInteger(int64(event.Current)), humanInteger(int64(event.Total)), event.Shard.SHA256[:12])
		}
	})
	if err != nil {
		return err
	}
	files, err := corpus.ExportNative(materialized, options.Output, options.Force)
	if err != nil {
		return err
	}
	document := provenance.NewCorpusExport(bom, files, time.Now())
	if err := provenance.WriteCorpusExport(options.Output, document); err != nil {
		return err
	}
	existing := 0
	for _, file := range files {
		if file.Existing {
			existing++
		}
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Output   string              `json:"output"`
			Totals   waldoindex.Measures `json:"totals"`
			Files    int                 `json:"files"`
			Existing int                 `json:"existing"`
			BOM      string              `json:"bom"`
		}{Output: options.Output, Totals: bom.Totals, Files: len(files), Existing: existing, BOM: "EXPORT.json"})
	}
	fmt.Fprintf(stdout, "exported %s files (%s already verified) and EXPORT.json to %s\n",
		humanInteger(int64(len(files))), humanInteger(int64(existing)), options.Output)
	return nil
}

func parseExportOptions(args []string) (exportOptions, error) {
	var options exportOptions
	for i := 0; i < len(args); i++ {
		argument := args[i]
		switch {
		case argument == "--output":
			value, next, err := optionValue(args, i, "--output")
			if err != nil {
				return exportOptions{}, err
			}
			options.Output, i = value, next
		case strings.HasPrefix(argument, "--output="):
			options.Output = strings.TrimPrefix(argument, "--output=")
		case argument == "--license":
			value, next, err := optionValue(args, i, "--license")
			if err != nil {
				return exportOptions{}, err
			}
			options.Include, i = append(options.Include, splitComma(value)...), next
		case strings.HasPrefix(argument, "--license="):
			options.Include = append(options.Include, splitComma(strings.TrimPrefix(argument, "--license="))...)
		case argument == "--exclude-license":
			value, next, err := optionValue(args, i, "--exclude-license")
			if err != nil {
				return exportOptions{}, err
			}
			options.Exclude, i = append(options.Exclude, splitComma(value)...), next
		case strings.HasPrefix(argument, "--exclude-license="):
			options.Exclude = append(options.Exclude, splitComma(strings.TrimPrefix(argument, "--exclude-license="))...)
		case argument == "--force":
			options.Force = true
		case strings.HasPrefix(argument, "-"):
			return exportOptions{}, usageError{message: fmt.Sprintf("unknown index export option %q", argument)}
		default:
			options.Paths = append(options.Paths, argument)
		}
	}
	if len(options.Paths) == 0 || options.Output == "" {
		return exportOptions{}, usageError{message: "usage: waldo index export <path...> --output <directory>"}
	}
	return options, nil
}

func optionValue(args []string, index int, name string) (string, int, error) {
	if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
		return "", index, usageError{message: name + " requires a value"}
	}
	return args[index+1], index + 1, nil
}

func splitComma(value string) []string {
	var result []string
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
