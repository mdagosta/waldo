package cli

import (
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/openwaldo/waldo-new/internal/ingest"
)

func runIndexAdd(context Context, args []string, stdout, _ io.Writer) error {
	request, inputs, dryRun, err := parseIndexAdd(args)
	if err != nil {
		return err
	}
	if !dryRun {
		return fmt.Errorf("index add execution is not enabled yet; rerun with --dry-run to inspect the immutable ingestion plan")
	}
	probe, err := ingest.ProbePaths(context.Execution, inputs)
	if err != nil {
		return err
	}
	plan, err := ingest.NewPlan(probe, request)
	if err != nil {
		return err
	}
	identity, err := plan.Identity()
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Identity string      `json:"identity"`
			Plan     ingest.Plan `json:"plan"`
		}{Identity: identity, Plan: plan})
	}
	fmt.Fprintf(stdout, "ingestion plan %s\n", identity[:12])
	fmt.Fprintf(stdout, "  destination  %s\n", plan.Destination)
	fmt.Fprintf(stdout, "  title        %s\n", plan.Title)
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

func parseIndexAdd(args []string) (ingest.PlanRequest, []string, bool, error) {
	var request ingest.PlanRequest
	var inputs []string
	dryRun := false
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
			dryRun = true
		case "--to":
			request.Destination, err = value("--to")
		case "--title":
			request.Title, err = value("--title")
		case "--license":
			request.License, err = value("--license")
		case "--source":
			request.Source.URL, err = value("--source")
		case "--source-name":
			request.Source.Name, err = value("--source-name")
		case "--source-category":
			request.Source.Category, err = value("--source-category")
		case "--text-column":
			request.TextColumn, err = value("--text-column")
		case "--mode":
			request.Mode, err = value("--mode")
		case "--memory":
			var raw string
			raw, err = value("--memory")
			if err == nil {
				request.MemoryBytes, err = parseMemory(raw)
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return ingest.PlanRequest{}, nil, false, usageError{message: fmt.Sprintf("unknown index add option %q", arg)}
			}
			inputs = append(inputs, arg)
		}
		if err != nil {
			return ingest.PlanRequest{}, nil, false, err
		}
	}
	if len(inputs) == 0 {
		return ingest.PlanRequest{}, nil, false, usageError{message: "index add needs at least one input path"}
	}
	if request.Destination == "" || request.Title == "" || request.License == "" || request.Source.URL == "" || request.Source.Category == "" {
		return ingest.PlanRequest{}, nil, false, usageError{message: "index add requires --to, --title, --license, --source, and --source-category"}
	}
	if request.Source.Name == "" {
		request.Source.Name = path.Base(strings.TrimSuffix(request.Destination, "/"))
	}
	return request, inputs, dryRun, nil
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
