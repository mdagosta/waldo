package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/openwaldo/waldo-new/internal/corpus"
	waldoindex "github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/lookaside"
)

func runIndexList(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) > 1 {
		return usageError{message: "usage: waldo index list [path]"}
	}
	target, err := resolveIndexArgument(context, args)
	if err != nil {
		return err
	}
	corpora, err := waldoindex.ListCorpora(target)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Path    string                  `json:"path"`
			Corpora []waldoindex.CorpusInfo `json:"corpora"`
		}{Path: target.Rel, Corpora: corpora})
	}
	if len(corpora) == 0 {
		fmt.Fprintf(stdout, "no corpora indexed beneath %s\n", displayPath(target.Rel))
		return nil
	}
	table := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "PATH\tTITLE\tSHARDS\tDOCS\tTOKENS\tSIZE\tLICENSE")
	for _, corpus := range corpora {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			corpus.Path, corpus.Title, humanInteger(corpus.Shards), humanCount(corpus.Docs),
			humanCount(corpus.Tokens), humanBytes(corpus.Bytes), licenseSummary(corpus.Licenses))
	}
	return table.Flush()
}

func runIndexShow(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) > 1 {
		return usageError{message: "usage: waldo index show [path]"}
	}
	target, err := resolveIndexArgument(context, args)
	if err != nil {
		return err
	}
	info, err := os.Stat(target.Abs)
	if err != nil {
		return err
	}
	if info.IsDir() {
		directory, err := waldoindex.LoadDirectory(target.Abs)
		if err != nil {
			return err
		}
		if len(directory.Entries) == 1 && directory.Entries[0].Type == "manifest" {
			target.Abs = filepath.Join(target.Abs, directory.Entries[0].Name)
			target.Rel = filepath.ToSlash(filepath.Join(target.Rel, directory.Entries[0].Name))
		} else {
			if context.JSON {
				return writeJSON(stdout, directory)
			}
			fmt.Fprintf(stdout, "index %s\n", displayPath(directory.Path))
			for _, entry := range waldoindex.SortedEntries(directory.Entries) {
				fmt.Fprintf(stdout, "  %-10s %s\n", entry.Type, entry.Name)
			}
			return nil
		}
	}

	manifest, err := waldoindex.LoadManifest(target.Abs)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, manifest)
	}
	printManifest(stdout, target.Rel, manifest)
	return nil
}

func runIndexSummary(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) > 1 {
		return usageError{message: "usage: waldo index summary [path]"}
	}
	target, err := resolveIndexArgument(context, args)
	if err != nil {
		return err
	}
	totals, err := waldoindex.Summarize(target)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Path   string            `json:"path"`
			Totals waldoindex.Totals `json:"totals"`
		}{Path: target.Rel, Totals: totals})
	}
	fmt.Fprintf(stdout, "index %s\n", displayPath(target.Rel))
	fmt.Fprintf(stdout, "  corpora  %s\n", humanInteger(totals.Corpora))
	fmt.Fprintf(stdout, "  shards   %s\n", humanInteger(totals.Shards))
	fmt.Fprintf(stdout, "  docs     %s\n", humanCount(totals.Docs))
	fmt.Fprintf(stdout, "  tokens   %s\n", humanCount(totals.Tokens))
	fmt.Fprintf(stdout, "  bytes    %s\n", humanBytes(totals.Bytes))
	if len(totals.Licenses) > 0 {
		fmt.Fprintln(stdout, "\nlicenses")
		licenses := make([]string, 0, len(totals.Licenses))
		for license := range totals.Licenses {
			licenses = append(licenses, license)
		}
		sort.Strings(licenses)
		for _, license := range licenses {
			licenseTotals := totals.Licenses[license]
			shardWord := "shards"
			if licenseTotals.Shards == 1 {
				shardWord = "shard"
			}
			fmt.Fprintf(stdout, "  %-48s %12s tokens  %4s %s\n", license, humanCount(licenseTotals.Tokens), humanInteger(licenseTotals.Shards), shardWord)
		}
	}
	return nil
}

func runIndexVerify(context Context, args []string, stdout, stderr io.Writer) error {
	return runIndexVerifyWithProgress(context, args, stdout, stderr)
}

func runIndexVerifyWithProgress(context Context, args []string, stdout, progress io.Writer) error {
	var path string
	objects := false
	for _, arg := range args {
		if arg == "--objects" {
			objects = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return usageError{message: fmt.Sprintf("unknown index verify option %q", arg)}
		}
		if path != "" {
			return usageError{message: "usage: waldo index verify [path] [--objects]"}
		}
		path = arg
	}
	var paths []string
	if path != "" {
		paths = []string{path}
	}
	target, err := resolveIndexArgument(context, paths)
	if err != nil {
		return err
	}
	verification, err := waldoindex.Verify(target)
	if err != nil {
		return err
	}
	if !objects && context.JSON {
		return writeJSON(stdout, struct {
			Path         string                  `json:"path"`
			Verification waldoindex.Verification `json:"verification"`
		}{Path: target.Rel, Verification: verification})
	}
	if !objects {
		fmt.Fprintf(stdout, "verified %s: %s directories, %s corpora, %s shards\n",
			displayPath(target.Rel), humanInteger(verification.Directories), humanInteger(verification.Corpora), humanInteger(verification.Shards))
		return nil
	}

	policy, err := corpus.NewLicensePolicy(nil, nil)
	if err != nil {
		return err
	}
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return err
	}
	bom, err := corpus.BuildBOM(context.Execution, []waldoindex.Target{target}, policy, cache)
	if err != nil {
		return err
	}
	fmt.Fprintf(progress, "verifying %s objects (%s) through lookaside cache %s\n",
		humanInteger(int64(len(bom.Shards))), humanBytes(bom.Totals.Bytes), cache.Root())
	materialized, err := corpus.Materialize(context.Execution, bom, cache, func(event corpus.MaterializeProgress) {
		if event.Current == 1 || event.Current == event.Total || event.Current%25 == 0 {
			fmt.Fprintf(progress, "  %s/%s  %s\n", humanInteger(int64(event.Current)), humanInteger(int64(event.Total)), event.Shard.SHA256[:12])
		}
	})
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Path         string                  `json:"path"`
			Verification waldoindex.Verification `json:"verification"`
			Objects      int                     `json:"objects_verified"`
			Bytes        int64                   `json:"bytes_verified"`
			Cache        string                  `json:"cache"`
		}{Path: target.Rel, Verification: verification, Objects: len(materialized.Objects), Bytes: bom.Totals.Bytes, Cache: cache.Root()})
	}
	fmt.Fprintf(stdout, "verified %s: %s directories, %s corpora, %s objects (%s)\n",
		displayPath(target.Rel), humanInteger(verification.Directories), humanInteger(verification.Corpora),
		humanInteger(int64(len(materialized.Objects))), humanBytes(bom.Totals.Bytes))
	return nil
}

func resolveIndexArgument(context Context, args []string) (waldoindex.Target, error) {
	target := ""
	if len(args) == 1 {
		target = args[0]
	}
	return waldoindex.Resolve(context.IndexPath, target)
}

func printManifest(w io.Writer, path string, manifest waldoindex.Manifest) {
	fmt.Fprintf(w, "%s\n", manifest.Title)
	fmt.Fprintf(w, "  path         %s\n", path)
	fmt.Fprintf(w, "  name         %s\n", manifest.Name)
	fmt.Fprintf(w, "  license      %s\n", manifest.License)
	fmt.Fprintf(w, "  description  %s\n", manifest.Description)
	var shards, docs, tokens, bytes int64
	if manifest.Rollup != nil {
		shards, docs, tokens, bytes = manifest.Rollup.Count, manifest.Rollup.Docs, manifest.Rollup.Tokens, manifest.Rollup.Bytes
	} else {
		shards = int64(len(manifest.Shards))
		for _, shard := range manifest.Shards {
			docs += shard.Docs
			tokens += shard.Tokens
			bytes += shard.Bytes
		}
	}
	fmt.Fprintf(w, "  contents     %s shards, %s docs, %s tokens, %s\n", humanInteger(shards), humanCount(docs), humanCount(tokens), humanBytes(bytes))
	fmt.Fprintln(w, "  sources")
	for _, source := range manifest.Sources {
		fmt.Fprintf(w, "    %s — %s\n", source.Name, source.URL)
	}
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func displayPath(path string) string {
	if path == "" {
		return "."
	}
	return path
}

func licenseSummary(licenses []string) string {
	switch len(licenses) {
	case 0:
		return "(none declared)"
	case 1:
		const max = 40
		if len(licenses[0]) > max {
			return licenses[0][:max-1] + "…"
		}
		return licenses[0]
	default:
		return fmt.Sprintf("%d licenses", len(licenses))
	}
}
