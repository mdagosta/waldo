package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/openwaldo/waldo/internal/shard"
)

func runShardSummary(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return usageError{message: "usage: waldo shard summary <path...>"}
	}
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

func runShardAudit(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return usageError{message: "usage: waldo shard audit <path...>"}
	}
	paths, err := shard.ResolvePaths(args)
	if err != nil {
		return err
	}
	summary, err := shard.Audit(context.Execution, paths)
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

func runShardListRecords(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) != 1 {
		return usageError{message: "usage: waldo shard list-records <shard-file>"}
	}
	paths, err := shard.ResolvePaths(args)
	if err != nil {
		return err
	}
	if len(paths) != 1 {
		return usageError{message: "list-records requires exactly one shard file"}
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
	if len(args) != 2 {
		return usageError{message: "usage: waldo shard export-record <shard-file> <record-id>"}
	}
	if context.JSON {
		return usageError{message: "--json is not supported by shard export-record because stdout is the record data"}
	}
	paths, err := shard.ResolvePaths(args[:1])
	if err != nil {
		return err
	}
	if len(paths) != 1 {
		return usageError{message: "export-record requires exactly one shard file"}
	}
	return shard.ExportRecord(paths[0], args[1], stdout)
}

func printShardSummary(output io.Writer, summary shard.Summary) {
	fmt.Fprintf(output, "SHARDS:         %s\n", humanInteger(summary.Shards))
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
