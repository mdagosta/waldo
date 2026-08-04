package cli

import (
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	waldoindex "github.com/openwaldo/waldo-new/internal/index"
	"github.com/openwaldo/waldo-new/internal/ingest"
	"github.com/openwaldo/waldo-new/internal/lookaside"
)

func runIndexAdd(context Context, args []string, stdout, _ io.Writer) error {
	options, err := parseIndexAdd(args)
	if err != nil {
		return err
	}
	if !options.DryRun && (options.Staging == "" || options.ObjectBase == "") {
		return usageError{message: "index add execution requires --staging and --object-base; use --dry-run to preflight only"}
	}
	probe, err := ingest.ProbePaths(context.Execution, options.Inputs)
	if err != nil {
		return err
	}
	plan, err := ingest.NewPlan(probe, options.Request)
	if err != nil {
		return err
	}
	identity, err := plan.Identity()
	if err != nil {
		return err
	}
	if options.DryRun && context.JSON {
		return writeJSON(stdout, struct {
			Identity string      `json:"identity"`
			Plan     ingest.Plan `json:"plan"`
		}{Identity: identity, Plan: plan})
	}
	if !options.DryRun {
		target, err := waldoindex.Resolve(context.IndexPath, "")
		if err != nil {
			return err
		}
		if err := ingest.CheckContributionDestination(target.Root, plan); err != nil {
			return err
		}
		if err := ingest.ValidatePublicObjectBase(options.ObjectBase); err != nil {
			return err
		}
		cache, err := lookaside.DefaultCache()
		if err != nil {
			return err
		}
		assembly, admission, err := ingest.ExecuteAdmission(context.Execution, plan, options.Staging, cache)
		if err != nil {
			return err
		}
		manifest, err := ingest.BuildManifest(plan, assembly, options.ObjectBase)
		if err != nil {
			return err
		}
		contribution, err := ingest.StageContribution(target.Root, options.Staging, plan, manifest)
		if err != nil {
			return err
		}
		if context.JSON {
			return writeJSON(stdout, struct {
				Identity     string                    `json:"identity"`
				Plan         ingest.Plan               `json:"plan"`
				Assembly     ingest.AssemblyResult     `json:"assembly"`
				Admission    ingest.AdmissionResult    `json:"admission"`
				Contribution ingest.ContributionResult `json:"contribution"`
			}{identity, plan, assembly, admission, contribution})
		}
		fmt.Fprintf(stdout, "ingestion %s complete\n", identity[:12])
		fmt.Fprintf(stdout, "  records      %s input, %s retained, %s duplicate\n", humanInteger(assembly.InputDocs), humanInteger(assembly.RetainedDocs), humanInteger(assembly.DuplicateDocs))
		fmt.Fprintf(stdout, "  objects      %s admitted to %s\n", humanInteger(int64(len(admission.Objects))), admission.CacheRoot)
		fmt.Fprintf(stdout, "  contribution %s (%s changed files)\n", contribution.Root, humanInteger(int64(len(contribution.Files))))
		fmt.Fprintln(stdout, "review the overlay, upload the objects to --object-base, then apply and commit the changed index files with DCO sign-off")
		return nil
	}
	fmt.Fprintf(stdout, "ingestion plan %s\n", identity[:12])
	fmt.Fprintf(stdout, "  destination  %s\n", plan.Destination)
	fmt.Fprintf(stdout, "  title        %s\n", plan.Title)
	fmt.Fprintf(stdout, "  description  %s\n", plan.Description)
	fmt.Fprintf(stdout, "  license      %s\n", plan.License)
	fmt.Fprintf(stdout, "  source       %s (%s)\n", plan.Source.Name, plan.Source.Category)
	fmt.Fprintf(stdout, "  mode         %s\n", plan.Mode)
	fmt.Fprintf(stdout, "  memory       %s\n", humanBytes(plan.MemoryBytes))
	fmt.Fprintf(stdout, "  input        %s files, %s\n", humanInteger(int64(len(plan.Inputs))), humanBytes(probe.Totals.Bytes))
	for _, input := range plan.Inputs {
		mapping := input.Adapter
		if input.TextColumn != "" {
			mapping += ":" + input.TextColumn
		}
		fmt.Fprintf(stdout, "    %-18s %s (%s)\n", mapping, input.Artifact.Path, humanBytes(input.Artifact.Bytes))
	}
	fmt.Fprintf(stdout, "  writer       Parquet schema %d, %s target, %s row groups, %s\n",
		plan.Writer.RecordSchema, humanBytes(plan.Writer.CompressedTarget),
		humanBytes(plan.Writer.RowGroupLogicalBytes), plan.Writer.Compression)
	fmt.Fprintln(stdout, "dry run complete; no files were written")
	return nil
}

type indexAddOptions struct {
	Request    ingest.PlanRequest
	Inputs     []string
	DryRun     bool
	Staging    string
	ObjectBase string
}

func parseIndexAdd(args []string) (indexAddOptions, error) {
	var options indexAddOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := func(name string) (string, error) {
			if i+1 >= len(args) {
				return "", usageError{message: name + " needs a value"}
			}
			i++
			return args[i], nil
		}
		var err error
		switch arg {
		case "--dry-run":
			options.DryRun = true
		case "--to":
			options.Request.Destination, err = value("--to")
		case "--title":
			options.Request.Title, err = value("--title")
		case "--description":
			options.Request.Description, err = value("--description")
		case "--license":
			options.Request.License, err = value("--license")
		case "--source":
			options.Request.Source.URL, err = value("--source")
		case "--source-name":
			options.Request.Source.Name, err = value("--source-name")
		case "--source-category":
			options.Request.Source.Category, err = value("--source-category")
		case "--text-column":
			options.Request.TextColumn, err = value("--text-column")
		case "--mode":
			options.Request.Mode, err = value("--mode")
		case "--staging":
			options.Staging, err = value("--staging")
		case "--object-base":
			options.ObjectBase, err = value("--object-base")
		case "--memory":
			var raw string
			raw, err = value("--memory")
			if err == nil {
				options.Request.MemoryBytes, err = parseMemory(raw)
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return indexAddOptions{}, usageError{message: fmt.Sprintf("unknown index add option %q", arg)}
			}
			options.Inputs = append(options.Inputs, arg)
		}
		if err != nil {
			return indexAddOptions{}, err
		}
	}
	if len(options.Inputs) == 0 {
		return indexAddOptions{}, usageError{message: "index add needs at least one input path"}
	}
	request := &options.Request
	if request.Destination == "" || request.Title == "" || request.License == "" || request.Source.URL == "" || request.Source.Category == "" {
		return indexAddOptions{}, usageError{message: "index add requires --to, --title, --license, --source, and --source-category"}
	}
	if request.Source.Name == "" {
		request.Source.Name = path.Base(strings.TrimSuffix(request.Destination, "/"))
	}
	return options, nil
}

func parseMemory(value string) (int64, error) {
	upper := strings.ToUpper(strings.TrimSpace(value))
	units := []struct {
		suffix string
		scale  int64
	}{{"GIB", 1 << 30}, {"MIB", 1 << 20}, {"GB", 1_000_000_000}, {"MB", 1_000_000}}
	for _, unit := range units {
		if strings.HasSuffix(upper, unit.suffix) {
			number := strings.TrimSpace(strings.TrimSuffix(upper, unit.suffix))
			parsed, err := strconv.ParseInt(number, 10, 64)
			if err != nil || parsed <= 0 || parsed > (1<<63-1)/unit.scale {
				return 0, usageError{message: fmt.Sprintf("invalid --memory value %q", value)}
			}
			return parsed * unit.scale, nil
		}
	}
	parsed, err := strconv.ParseInt(upper, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, usageError{message: fmt.Sprintf("invalid --memory value %q", value)}
	}
	return parsed, nil
}
