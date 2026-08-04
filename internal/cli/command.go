package cli

import (
	"context"
	"io"
)

type Context struct {
	Execution context.Context
	IndexPath string
	JSON      bool
}

type Handler func(Context, []string, io.Writer, io.Writer) error

// Command describes one stable part of the WALDO command vocabulary. Phase 0
// intentionally contains no operational handlers: the command tree is a UX
// contract that implementation slices will fill in one domain at a time.
type Command struct {
	Name     string
	Summary  string
	Usage    string
	Children []Command
	Handler  Handler
}

func commandTree() Command {
	return Command{
		Name:    "waldo",
		Summary: "Build and use auditable AI training data",
		Children: []Command{
			{Name: "index", Summary: "Manage indexed training data", Children: []Command{
				{Name: "init", Summary: "Initialize an index checkout"},
				{Name: "list", Summary: "List all corpora beneath an index path", Usage: "waldo index list [path] [--index <checkout>] [--json]", Handler: runIndexList},
				{Name: "show", Summary: "Show an index entry or corpus manifest", Usage: "waldo index show [path] [--index <checkout>] [--json]", Handler: runIndexShow},
				{Name: "summary", Summary: "Summarize corpora, licenses, and totals", Usage: "waldo index summary [path] [--index <checkout>] [--json]", Handler: runIndexSummary},
				{Name: "verify", Summary: "Verify index structure and optionally its objects", Usage: "waldo index verify [path] [--objects] [--index <checkout>] [--json]", Handler: runIndexVerify},
				{Name: "add", Summary: "Add acquired material to an index"},
				{Name: "update", Summary: "Append new material to an existing corpus"},
				{Name: "export", Summary: "Export a verified corpus selection and BOM", Usage: "waldo index export <path...> --output <directory> [--license <glob,...>] [--exclude-license <glob,...>] [--force] [--index <checkout>] [--json]", Handler: runIndexExport},
				{Name: "remove", Summary: "Remove a corpus from the current index revision"},
			}},
			{Name: "lookaside", Summary: "Configure and maintain content-addressed objects", Children: []Command{
				{Name: "configure", Summary: "Configure a lookaside location"},
				{Name: "login", Summary: "Save credentials for lookaside writes"},
				{Name: "status", Summary: "Show the local verified-object cache", Usage: "waldo lookaside status [--json]", Handler: runLookasideStatus},
				{Name: "verify", Summary: "Scrub cached objects against their hashes", Usage: "waldo lookaside verify [--json]", Handler: runLookasideVerify},
				{Name: "mirror", Summary: "Copy verified objects to another lookaside"},
				{Name: "gc", Summary: "Safely reclaim unreferenced lookaside objects"},
			}},
			{Name: "model", Summary: "Build and inspect provenance-carrying models", Children: []Command{
				{Name: "create", Summary: "Create an untrained model"},
				{Name: "build", Summary: "Build a model from a declarative recipe"},
				{Name: "train", Summary: "Add an explicit training run"},
				{Name: "inspect", Summary: "Inspect architecture, runs, and lineage"},
				{Name: "test", Summary: "Evaluate a model on a corpus selection"},
				{Name: "chat", Summary: "Generate interactively with a local model"},
				{Name: "fork", Summary: "Create a model with inherited lineage"},
				{Name: "export", Summary: "Export weights with model provenance"},
				{Name: "remove", Summary: "Remove a local model artifact"},
			}},
			{Name: "bom", Summary: "Inspect and exchange provenance records", Children: []Command{
				{Name: "show", Summary: "Show a corpus or model BOM"},
				{Name: "verify", Summary: "Verify the internally checkable claims in a BOM"},
				{Name: "export", Summary: "Export a BOM in a supported interchange format"},
			}},
			{Name: "config", Summary: "Inspect and change machine-local preferences", Children: []Command{
				{Name: "show", Summary: "Show effective configuration"},
				{Name: "get", Summary: "Read one configuration value"},
				{Name: "set", Summary: "Set one configuration value"},
			}},
		},
	}
}
