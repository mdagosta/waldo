// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/openwaldo/waldo/internal/config"
	"github.com/openwaldo/waldo/internal/corpus"
	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/lookaside"
)

type listedLookasideObject struct {
	lookaside.ListedObject
	References []string `json:"references,omitempty"`
}

type missingLookasideReference struct {
	Name       string   `json:"name"`
	References []string `json:"references"`
}

type lookasideListTotals struct {
	Objects          int   `json:"objects"`
	Canonical        int   `json:"canonical"`
	Bytes            int64 `json:"bytes"`
	Referenced       int   `json:"referenced,omitempty"`
	NotInIndex       int   `json:"not_in_selected_index,omitempty"`
	MissingIndex     int   `json:"missing_index_objects,omitempty"`
	WithinLookaside  int   `json:"within_configured_lookaside"`
	InventoryObjects int   `json:"inventory_objects"`
	InventoryBytes   int64 `json:"inventory_bytes"`
}

func runLookasideList(commandContext Context, args []string, stdout, _ io.Writer) error {
	indexArgument := ""
	showAll := false
	for _, arg := range args {
		switch {
		case arg == "--all":
			if showAll {
				return usageError{message: "--all was specified more than once"}
			}
			showAll = true
		case strings.HasPrefix(arg, "-"):
			return usageError{message: fmt.Sprintf("unknown lookaside list option %q", arg)}
		case indexArgument != "":
			return usageError{message: "usage: waldo lookaside list [index-path] [--all] [--json]"}
		default:
			indexArgument = arg
		}
	}
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	if configuration.Lookaside.Publish == nil {
		return usageError{message: "configure a writable lookaside before listing objects: waldo config set lookaside <url>"}
	}
	lister, err := lookaside.NewObjectLister(commandContext.Execution, *configuration.Lookaside.Publish)
	if err != nil {
		return err
	}
	objects, err := lister.List(commandContext.Execution)
	if err != nil {
		return err
	}

	references := map[string][]string{}
	indexPath := ""
	if indexArgument != "" {
		target, err := resolveIndexArgument([]string{indexArgument}, nil)
		if err != nil {
			return err
		}
		if _, err := waldoindex.Verify(target); err != nil {
			return err
		}
		policy, err := corpus.NewLicensePolicy(nil, nil)
		if err != nil {
			return err
		}
		cache, err := lookaside.DefaultCache()
		if err != nil {
			return err
		}
		bom, err := corpus.BuildBOM(commandContext.Execution, []waldoindex.Target{target}, policy, cache)
		if err != nil {
			return err
		}
		indexPath = target.Rel
		if indexPath == "" {
			indexPath = "."
		}
		for _, shard := range bom.Shards {
			references[shard.SHA256] = appendUnique(references[shard.SHA256], shard.Manifest)
		}
		for name := range references {
			sort.Strings(references[name])
		}
	}

	listed := make([]listedLookasideObject, 0, len(objects))
	present := map[string]bool{}
	var totalBytes int64
	canonical := 0
	referenced := 0
	for _, object := range objects {
		item := listedLookasideObject{ListedObject: object}
		if object.Canonical {
			canonical++
			present[object.Name] = true
			item.References = references[object.Name]
			if len(item.References) > 0 {
				referenced++
			}
		}
		totalBytes += object.Bytes
		listed = append(listed, item)
	}
	missing := []missingLookasideReference{}
	for name, manifests := range references {
		if !present[name] {
			missing = append(missing, missingLookasideReference{Name: name, References: manifests})
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Name < missing[j].Name })
	displayed := listed
	if indexPath != "" && !showAll {
		displayed = make([]listedLookasideObject, 0, referenced)
		for _, object := range listed {
			if len(object.References) > 0 {
				displayed = append(displayed, object)
			}
		}
	}
	var displayedBytes int64
	displayedCanonical := 0
	displayedWithin := 0
	for _, object := range displayed {
		displayedBytes += object.Bytes
		if object.Canonical {
			displayedCanonical++
		}
		if object.InConfiguredLookaside {
			displayedWithin++
		}
	}
	totals := lookasideListTotals{
		Objects: len(displayed), Canonical: displayedCanonical, Bytes: displayedBytes, Referenced: referenced,
		NotInIndex: canonical - referenced, MissingIndex: len(missing), WithinLookaside: displayedWithin,
		InventoryObjects: len(listed), InventoryBytes: totalBytes,
	}

	if commandContext.JSON {
		return writeJSON(stdout, struct {
			Lookaside         string                      `json:"lookaside"`
			Inventory         string                      `json:"inventory"`
			Index             string                      `json:"index,omitempty"`
			Objects           []listedLookasideObject     `json:"objects"`
			MissingReferences []missingLookasideReference `json:"missing_references,omitempty"`
			Totals            lookasideListTotals         `json:"totals"`
		}{Lookaside: lister.BaseURL(), Inventory: lister.InventoryURL(), Index: indexPath, Objects: displayed, MissingReferences: missing, Totals: totals})
	}

	fmt.Fprintf(stdout, "%-16s  %10s  %-16s  %-24s  %s\n", "OBJECT", "SIZE", "STORED (UTC)", "PREFIX", "INDEX")
	for _, object := range displayed {
		fmt.Fprintf(stdout, "%-16s  %10s  %-16s  %-24s  %s\n",
			compactObjectName(object), humanBytes(object.Bytes), formatStored(object.StoredAt),
			clipColumn(object.Prefix, 24), compactIndexReferences(object.References, 36))
	}
	return nil
}

func compactObjectName(object listedLookasideObject) string {
	if !object.Canonical {
		return "[noncanonical]"
	}
	return object.Name[:16]
}

func compactIndexReferences(references []string, width int) string {
	if len(references) == 0 {
		return "--"
	}
	value := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(references[0], ".json"), ".yaml"), ".yml")
	if len(references) > 1 {
		value += fmt.Sprintf(" +%d", len(references)-1)
	}
	return clipColumn(value, width)
}

func clipColumn(value string, width int) string {
	characters := []rune(value)
	if len(characters) <= width {
		return value
	}
	if width <= 1 {
		return string(characters[:width])
	}
	return string(characters[:width-1]) + "…"
}

func formatStored(stored time.Time) string {
	if stored.IsZero() {
		return "--"
	}
	return stored.UTC().Format("2006-01-02 15:04")
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
