package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/openwaldo/waldo-new/internal/corpus"
	"github.com/openwaldo/waldo-new/internal/provenance"
)

func runBOMShow(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) != 1 {
		return usageError{message: "usage: waldo bom show <export-directory|EXPORT.json> [--json]"}
	}
	document, path, err := provenance.LoadCorpusExport(args[0])
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, document)
	}
	fmt.Fprintln(stdout, "OpenWALDO corpus export")
	fmt.Fprintf(stdout, "  document      %s\n", path)
	fmt.Fprintf(stdout, "  generated     %s\n", document.Generated)
	fmt.Fprintf(stdout, "  format        %s\n", document.Format)
	if document.BOM.Index.Remote != "" {
		fmt.Fprintf(stdout, "  index         %s\n", document.BOM.Index.Remote)
	}
	if document.BOM.Index.Commit != "" {
		commit := document.BOM.Index.Commit
		if len(commit) > 12 {
			commit = commit[:12]
		}
		dirty := ""
		if document.BOM.Index.Dirty {
			dirty = " (working tree was dirty)"
		}
		fmt.Fprintf(stdout, "  revision      %s%s\n", commit, dirty)
	}
	fmt.Fprintf(stdout, "  paths         %s\n", strings.Join(document.BOM.Paths, ", "))
	fmt.Fprintf(stdout, "  contents      %s shards, %s docs, %s tokens, %s source bytes\n",
		humanInteger(document.BOM.Totals.Shards), humanCount(document.BOM.Totals.Docs),
		humanCount(document.BOM.Totals.Tokens), humanBytes(document.BOM.Totals.Bytes))
	var exportedBytes int64
	for _, file := range document.Files {
		exportedBytes += file.Bytes
	}
	fmt.Fprintf(stdout, "  export        %s files, %s\n", humanInteger(int64(len(document.Files))), humanBytes(exportedBytes))
	return nil
}

func runBOMVerify(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) != 1 {
		return usageError{message: "usage: waldo bom verify <export-directory|EXPORT.json> [--json]"}
	}
	document, report, err := provenance.VerifyCorpusExport(args[0])
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Verification provenance.ExportVerification `json:"verification"`
			BOM          corpus.BOM                    `json:"bom"`
		}{Verification: report, BOM: document.BOM})
	}
	fmt.Fprintf(stdout, "verified OpenWALDO BOM and %s exported files (%s)\n", humanInteger(report.Files), humanBytes(report.Bytes))
	return nil
}
