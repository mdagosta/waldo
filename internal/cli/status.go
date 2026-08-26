// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/openwaldo/waldo/internal/status"
	"github.com/openwaldo/waldo/internal/training"
)

func runStatus(context Context, _ []string, stdout, _ io.Writer) error {
	report, err := status.Inspect(context.Execution)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, report)
	}
	fmt.Fprintf(stdout, "Host          %s/%s, %d CPUs\n", report.Host.OS, report.Host.Architecture, report.Host.CPUs)
	fmt.Fprintf(stdout, "Memory        %s\n", humanBytesUint(report.Host.MemoryBytes))
	fmt.Fprintf(stdout, "Disk          %s available / %s total\n", humanBytesUint(report.Host.DiskFree), humanBytesUint(report.Host.DiskBytes))
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Index         %s\n", report.Index.Path)
	fmt.Fprintf(stdout, "  Ready       %s\n", readiness(report.Index.Ready))
	if !report.Index.Ready {
		fmt.Fprintf(stdout, "  Reason      %s\n", report.Index.Reason)
	}
	fmt.Fprintf(stdout, "Lookaside     %s\n", report.Lookaside.Cache)
	if report.Lookaside.Publish == nil {
		fmt.Fprintln(stdout, "  Publish     (not configured)")
	} else {
		fmt.Fprintf(stdout, "  Publish     %s\n", report.Lookaside.Publish.URL)
	}
	fmt.Fprintln(stdout)
	if report.Training.Execution == nil {
		fmt.Fprintln(stdout, "Training      unavailable")
	} else {
		printTrainingExecution(stdout, *report.Training.Execution)
	}
	fmt.Fprintf(stdout, "  Ready       %s\n", readiness(report.Training.Ready))
	if !report.Training.Ready {
		fmt.Fprintf(stdout, "  Reason      %s\n", report.Training.Reason)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Ready         %s\n", readiness(report.Ready))
	if !report.Ready {
		for _, reason := range report.Reasons {
			fmt.Fprintf(stdout, "Reason        %s\n", singleLine(reason))
		}
	}
	return nil
}

func printTrainingExecution(output io.Writer, execution training.Execution) {
	fmt.Fprintf(output, "Training      %s@%s\n", execution.Backend.Name, execution.Backend.Revision)
	fmt.Fprintf(output, "  Runtime     %s\n", singleLine(execution.Runtime))
	if len(execution.Accelerators) == 0 {
		fmt.Fprintln(output, "  Accelerator CPU")
		return
	}
	for index, accelerator := range execution.Accelerators {
		label := "Accelerator"
		if index > 0 {
			label = ""
		}
		fmt.Fprintf(output, "  %-11s %s, %s\n", label, acceleratorDisplay(accelerator), humanBytesUint(accelerator.MemoryBytes))
	}
}

func acceleratorDisplay(accelerator training.Accelerator) string {
	manufacturer := strings.TrimSpace(accelerator.Manufacturer)
	model := strings.TrimSpace(accelerator.Model)
	if manufacturer == "" || strings.Contains(strings.ToLower(model), strings.ToLower(manufacturer)) {
		return model
	}
	return manufacturer + " " + model
}

func readiness(ready bool) string {
	if ready {
		return "yes"
	}
	return "no"
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
