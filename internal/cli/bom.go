package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openwaldo/waldo-new/internal/config"
	"github.com/openwaldo/waldo-new/internal/corpus"
	"github.com/openwaldo/waldo-new/internal/disclosure"
	"github.com/openwaldo/waldo-new/internal/model"
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

type bomExportOptions struct {
	Model           string
	Output          string
	Format          string
	Provider        string
	AllowIncomplete bool
	Force           bool
}

func runBOMExport(context Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseBOMExportOptions(args)
	if err != nil {
		return err
	}
	if options.Output != "" && !options.Force {
		if _, err := os.Stat(options.Output); err == nil {
			return fmt.Errorf("%s already exists; use --force to replace it", options.Output)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	root, err := config.EffectiveModelRoot(configuration)
	if err != nil {
		return err
	}
	inspection, err := model.Inspect(root, options.Model)
	if err != nil {
		return err
	}
	var provider *disclosure.ProviderProfile
	if options.Provider != "" {
		profile, err := disclosure.LoadProvider(options.Provider)
		if err != nil {
			return err
		}
		provider = &profile
	}
	report, err := disclosure.BuildEUGPAIReport(inspection, provider, time.Now())
	if err != nil {
		return err
	}
	blocking := report.BlockingGaps()
	if blocking > 0 && !options.AllowIncomplete {
		fmt.Fprintf(stderr, "EU GPAI export blocked by %s required disclosure gaps:\n", humanInteger(int64(blocking)))
		var required []disclosure.Gap
		for _, gap := range report.Gaps {
			if gap.Severity == "required" {
				required = append(required, gap)
			}
		}
		limit := len(required)
		if limit > 12 {
			limit = 12
		}
		for _, gap := range required[:limit] {
			context := ""
			if gap.Context != "" {
				context = " (" + gap.Context + ")"
			}
			fmt.Fprintf(stderr, "  %s %-34s %s%s\n", gap.Section, gap.Field, gap.Message, context)
		}
		if len(required) > limit {
			fmt.Fprintf(stderr, "  ... and %s more\n", humanInteger(int64(len(required)-limit)))
		}
		return fmt.Errorf("no output written; supply the missing facts or use --allow-incomplete for a marked draft")
	}
	if options.Output == "" {
		return writeJSON(stdout, report)
	}
	if err := disclosure.WriteEUGPAIReport(options.Output, report); err != nil {
		return err
	}
	absolute, _ := filepath.Abs(options.Output)
	if context.JSON {
		return writeJSON(stdout, struct {
			Output string                  `json:"output"`
			Report disclosure.EUGPAIReport `json:"report"`
		}{Output: absolute, Report: report})
	}
	fmt.Fprintf(stdout, "wrote %s EU GPAI disclosure mapping to %s\n", report.Status, absolute)
	fmt.Fprintf(stdout, "  template      %s (%s)\n", report.Template.Document, report.Template.Version)
	fmt.Fprintf(stdout, "  model         %s (%s)\n", report.Model.Name, shortModelHash(report.Model.ID))
	fmt.Fprintf(stdout, "  corpora       %s unique across %s stages\n", humanInteger(int64(len(report.Training.UniqueCorpora))), humanInteger(int64(len(report.Training.Stages))))
	fmt.Fprintf(stdout, "  gaps          %s\n", humanInteger(int64(len(report.Gaps))))
	if blocking > 0 {
		fmt.Fprintln(stdout, "  warning       incomplete draft; this is not a compliance finding")
	}
	return nil
}

func parseBOMExportOptions(args []string) (bomExportOptions, error) {
	var options bomExportOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		argument := args[i]
		switch {
		case argument == "--format":
			value, next, err := optionValue(args, i, "--format")
			if err != nil {
				return bomExportOptions{}, err
			}
			options.Format, i = value, next
		case strings.HasPrefix(argument, "--format="):
			options.Format = strings.TrimPrefix(argument, "--format=")
		case argument == "--provider":
			value, next, err := optionValue(args, i, "--provider")
			if err != nil {
				return bomExportOptions{}, err
			}
			options.Provider, i = value, next
		case strings.HasPrefix(argument, "--provider="):
			options.Provider = strings.TrimPrefix(argument, "--provider=")
		case argument == "--allow-incomplete":
			options.AllowIncomplete = true
		case argument == "--force":
			options.Force = true
		case strings.HasPrefix(argument, "-"):
			return bomExportOptions{}, usageError{message: fmt.Sprintf("unknown bom export option %q", argument)}
		default:
			positional = append(positional, argument)
		}
	}
	if (len(positional) != 1 && len(positional) != 2) || options.Format == "" {
		return bomExportOptions{}, usageError{message: "usage: waldo bom export <model-name-or-path> [output.json] --format eu-gpai [--provider <profile.json>] [--allow-incomplete] [--force]"}
	}
	if options.Format != "eu-gpai" {
		return bomExportOptions{}, usageError{message: fmt.Sprintf("unsupported BOM export format %q; use eu-gpai", options.Format)}
	}
	if len(positional) == 1 && options.Force {
		return bomExportOptions{}, usageError{message: "--force requires an output file"}
	}
	if len(positional) == 2 && strings.ToLower(filepath.Ext(positional[1])) != ".json" {
		return bomExportOptions{}, usageError{message: "eu-gpai output must use a .json filename; editable document rendering is not implemented yet"}
	}
	options.Model = positional[0]
	if len(positional) == 2 {
		options.Output = positional[1]
	}
	return options, nil
}
