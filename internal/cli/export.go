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
	options, err := cobraExportOptions(context, args)
	if err != nil {
		return err
	}
	policy, err := corpus.NewLicensePolicy(options.Include, options.Exclude)
	if err != nil {
		return err
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

func cobraExportOptions(context Context, args []string) (exportOptions, error) {
	options := exportOptions{
		Paths:  append([]string(nil), args[:len(args)-1]...),
		Output: args[len(args)-1],
		Force:  boolOption(context, "force"),
		Format: stringOption(context, "format"),
	}
	for _, value := range stringArrayOption(context, "license") {
		options.Include = append(options.Include, splitComma(value)...)
	}
	for _, value := range stringArrayOption(context, "exclude-license") {
		options.Exclude = append(options.Exclude, splitComma(value)...)
	}
	if options.Format != "native" && options.Format != "jsonl" {
		return exportOptions{}, fmt.Errorf("unsupported export format %q; use native or jsonl", options.Format)
	}
	return options, nil
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
