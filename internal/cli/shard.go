// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/openwaldo/waldo/internal/shard"
)

func runShardSummary(context Context, args []string, stdout, _ io.Writer) error {
	paths, err := shard.ResolvePaths(args)
	if err != nil {
		return err
	}
	summary, err := shard.Summarize(paths)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, summary)
	}
	printShardSummary(stdout, summary)
	return nil
}

func runShardAudit(context Context, args []string, stdout, progress io.Writer) error {
	options, err := cobraAuditOptions(context)
	if err != nil {
		return err
	}
	paths, err := shard.ResolvePaths(args)
	if err != nil {
		return err
	}
	options.Progress = auditProgressPrinter(progress)
	summary, err := shard.AuditWithOptions(context.Execution, paths, options)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Status  string        `json:"status"`
			Summary shard.Summary `json:"summary"`
		}{Status: "verified", Summary: summary})
	}
	fmt.Fprintln(stdout, "STATUS:         VERIFIED")
	printShardSummary(stdout, summary)
	return nil
}

func cobraAuditOptions(context Context) (shard.AuditOptions, error) {
	workers := intOption(context, "workers")
	if workers < 0 || workers > 32 || (optionChanged(context, "workers") && workers == 0) {
		return shard.AuditOptions{}, fmt.Errorf("--workers must be an integer from 1 to 32")
	}
	return shard.AuditOptions{Workers: workers}, nil
}

func auditProgressPrinter(output io.Writer) func(shard.AuditProgress) {
	return func(event shard.AuditProgress) {
		if event.Current == 1 || event.Current == event.Total || event.Current%5 == 0 {
			fmt.Fprintf(output, "  validated %s/%s shards  %s records  %s tokens\n", humanInteger(int64(event.Current)), humanInteger(int64(event.Total)), humanInteger(event.Summary.Records), humanInteger(event.Summary.Tokens))
		}
	}
}

func runShardListRecords(context Context, args []string, stdout, _ io.Writer) error {
	paths, err := shard.ResolvePaths(args)
	if err != nil {
		return err
	}
	if len(paths) != 1 {
		return fmt.Errorf("list-records requires exactly one shard file")
	}
	if context.JSON {
		encoder := newJSONLineEncoder(stdout)
		return shard.WalkRecords(paths[0], func(position int64, record shard.RecordView) error {
			return encoder.Encode(struct {
				Position int64 `json:"position"`
				shard.RecordView
			}{position, record})
		})
	}
	table := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "RECORD\tBYTES\tTOKENS\tLICENSE\tLANGUAGE\tSOURCE")
	err = shard.WalkRecords(paths[0], func(_ int64, record shard.RecordView) error {
		id := record.ID
		if len(id) > 16 {
			id = id[:16]
		}
		language := record.Language
		if language == "" {
			language = "--"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", id, humanBytes(record.Bytes), humanInteger(record.Tokens), compactShardField(record.License, 24), compactShardField(language, 12), compactShardField(record.Source, 40))
		return nil
	})
	if err != nil {
		return err
	}
	return table.Flush()
}

func compactShardField(value string, maximum int) string {
	characters := []rune(value)
	if len(characters) <= maximum {
		return value
	}
	return string(characters[:maximum-1]) + "…"
}

func runShardExportRecord(context Context, args []string, stdout, _ io.Writer) error {
	if context.JSON {
		return fmt.Errorf("--json is not supported by shard export-record because stdout is the record data")
	}
	paths, err := shard.ResolvePaths(args[:1])
	if err != nil {
		return err
	}
	if len(paths) != 1 {
		return fmt.Errorf("export-record requires exactly one shard file")
	}
	return shard.ExportRecord(paths[0], args[1], stdout)
}

func printShardSummary(output io.Writer, summary shard.Summary) {
	fmt.Fprintf(output, "SHARDS:         %s\n", humanInteger(summary.Shards))
	if summary.Attested > 0 || summary.DeepScanned > 0 {
		fmt.Fprintf(output, "ATTESTED:       %s\n", humanInteger(summary.Attested))
		fmt.Fprintf(output, "DEEP SCANNED:   %s\n", humanInteger(summary.DeepScanned))
	}
	fmt.Fprintf(output, "RECORDS:        %s\n", humanInteger(summary.Records))
	fmt.Fprintf(output, "TOKENS:         %s\n", humanInteger(summary.Tokens))
	fmt.Fprintf(output, "LICENSES:       %s\n", humanInteger(int64(len(summary.Licenses))))
	fmt.Fprintf(output, "CONTENT:        %s\n", humanBytes(summary.ContentBytes))
	fmt.Fprintf(output, "ENCODED:        %s\n", humanBytes(summary.EncodedBytes))
	fmt.Fprintf(output, "ROW GROUPS:     %s\n", humanInteger(summary.RowGroups))
}

type jsonLineEncoder interface{ Encode(any) error }

func newJSONLineEncoder(output io.Writer) jsonLineEncoder { return &lineEncoder{output: output} }

type lineEncoder struct{ output io.Writer }

func (encoder *lineEncoder) Encode(value any) error { return writeJSON(encoder.output, value) }
