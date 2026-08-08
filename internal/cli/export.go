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
	"time"

	"github.com/openwaldo/waldo/internal/corpus"
	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/lookaside"
	"github.com/openwaldo/waldo/internal/provenance"
)

type exportOptions struct {
	Paths   []string
	Output  string
	Include []string
	Exclude []string
	Force   bool
	Format  string
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
	targets, err := resolveIndexArgumentsWithWarning(context.Execution, options.Paths, stderr)
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
	if len(bom.Shards) == 0 {
		return fmt.Errorf("the selected paths and license policy contain no shards")
	}
	if err := provenance.CheckCorpusExportDestination(options.Output, bom, options.Format); err != nil {
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
	var files []corpus.ExportedFile
	if options.Format == "jsonl" {
		files, err = corpus.ExportJSONL(materialized, options.Output, options.Force)
	} else {
		files, err = corpus.ExportNative(materialized, options.Output, options.Force)
	}
	if err != nil {
		return err
	}
	for position, file := range files {
		status := "wrote"
		if file.Existing {
			status = "resumed"
		}
		fmt.Fprintf(stderr, "  local %s/%s  %-7s %s (%s, %s docs)\n",
			humanInteger(int64(position+1)), humanInteger(int64(len(files))), status,
			filepath.Join(options.Output, filepath.FromSlash(file.Path)), humanBytes(file.Bytes), humanCount(file.Docs))
	}
	document := provenance.NewCorpusExport(bom, options.Format, files, time.Now())
	if err := provenance.WriteCorpusExport(options.Output, document); err != nil {
		return err
	}
	purged, err := cache.PurgeUsed()
	if err != nil {
		return fmt.Errorf("purge successful export cache: %w", err)
	}
	if !cache.Retained() {
		fmt.Fprintf(stderr, "purged %s cached objects (%s)\n", humanInteger(purged.Objects), humanBytes(purged.Bytes))
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
			Purged   lookaside.Stats     `json:"scratch_purged"`
		}{Output: options.Output, Totals: bom.Totals, Files: len(files), Existing: existing, BOM: "EXPORT.json", Purged: purged})
	}
	fmt.Fprintf(stdout, "exported %s files (%s already verified) and EXPORT.json to %s\n",
		humanInteger(int64(len(files))), humanInteger(int64(existing)), options.Output)
	return nil
}

func parseExportOptions(args []string) (exportOptions, error) {
	options := exportOptions{Format: "native"}
	for i := 0; i < len(args); i++ {
		argument := args[i]
		switch {
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
		case argument == "--format":
			value, next, err := optionValue(args, i, "--format")
			if err != nil {
				return exportOptions{}, err
			}
			options.Format, i = value, next
		case strings.HasPrefix(argument, "--format="):
			options.Format = strings.TrimPrefix(argument, "--format=")
		case strings.HasPrefix(argument, "-"):
			return exportOptions{}, usageError{message: fmt.Sprintf("unknown index export option %q", argument)}
		default:
			options.Paths = append(options.Paths, argument)
		}
	}
	if len(options.Paths) < 1 {
		return exportOptions{}, usageError{message: "usage: waldo index export [path...] <directory> [--format native|jsonl]"}
	}
	options.Output = options.Paths[len(options.Paths)-1]
	options.Paths = options.Paths[:len(options.Paths)-1]
	if options.Format != "native" && options.Format != "jsonl" {
		return exportOptions{}, usageError{message: fmt.Sprintf("unsupported export format %q; use native or jsonl", options.Format)}
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
