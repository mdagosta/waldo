package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/openwaldo/waldo-new/internal/config"
	"github.com/openwaldo/waldo-new/internal/corpus"
	waldoindex "github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/lookaside"
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
	Objects      int   `json:"objects"`
	Canonical    int   `json:"canonical"`
	Bytes        int64 `json:"bytes"`
	Referenced   int   `json:"referenced,omitempty"`
	NotInIndex   int   `json:"not_in_selected_index,omitempty"`
	MissingIndex int   `json:"missing_index_objects,omitempty"`
}

func runLookasideList(commandContext Context, args []string, stdout, _ io.Writer) error {
	if len(args) > 1 {
		return usageError{message: "usage: waldo lookaside list [index-path] [--json]"}
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
	if len(args) == 1 {
		target, err := resolveIndexArgument(commandContext, args)
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
	totals := lookasideListTotals{
		Objects: len(listed), Canonical: canonical, Bytes: totalBytes, Referenced: referenced,
		NotInIndex: canonical - referenced, MissingIndex: len(missing),
	}

	if commandContext.JSON {
		return writeJSON(stdout, struct {
			Lookaside         string                      `json:"lookaside"`
			Index             string                      `json:"index,omitempty"`
			Objects           []listedLookasideObject     `json:"objects"`
			MissingReferences []missingLookasideReference `json:"missing_references,omitempty"`
			Totals            lookasideListTotals         `json:"totals"`
		}{Lookaside: lister.BaseURL(), Index: indexPath, Objects: listed, MissingReferences: missing, Totals: totals})
	}

	fmt.Fprintf(stdout, "lookaside %s\n", lister.BaseURL())
	if indexPath != "" {
		fmt.Fprintf(stdout, "selected index %s\n", displayPath(indexPath))
	}
	if len(listed) == 0 {
		fmt.Fprintln(stdout, "  no objects")
	} else {
		table := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		if indexPath == "" {
			fmt.Fprintln(table, "OBJECT\tSIZE\tPATH")
		} else {
			fmt.Fprintln(table, "OBJECT\tSIZE\tREFERENCED BY SELECTED INDEX\tPATH")
		}
		for _, object := range listed {
			name := object.Name
			if !object.Canonical {
				name = "(noncanonical)"
			}
			if indexPath == "" {
				fmt.Fprintf(table, "%s\t%s\t%s\n", name, humanBytes(object.Bytes), object.Path)
			} else {
				reference := "(not in selected index)"
				if len(object.References) > 0 {
					reference = strings.Join(object.References, ", ")
				}
				fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", name, humanBytes(object.Bytes), reference, object.Path)
			}
		}
		if err := table.Flush(); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "%s objects (%s); %s canonical\n", humanInteger(int64(len(listed))), humanBytes(totalBytes), humanInteger(int64(canonical)))
	if indexPath != "" {
		fmt.Fprintf(stdout, "%s referenced, %s not in selected index, %s index references absent from this lookaside\n",
			humanInteger(int64(referenced)), humanInteger(int64(canonical-referenced)), humanInteger(int64(len(missing))))
		for _, item := range missing {
			fmt.Fprintf(stdout, "  MISSING %s  %s\n", item.Name, strings.Join(item.References, ", "))
		}
	}
	return nil
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
