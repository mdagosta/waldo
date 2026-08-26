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
	fmt.Fprintln(stdout, "Host")
	printStatusField(stdout, "Platform", fmt.Sprintf("%s/%s", report.Host.OS, report.Host.Architecture))
	printStatusField(stdout, "CPUs", fmt.Sprintf("%d", report.Host.CPUs))
	printStatusField(stdout, "Memory", humanBytesUint(report.Host.MemoryBytes))
	printStatusField(stdout, "Disk", fmt.Sprintf("%s available / %s total", humanBytesUint(report.Host.DiskFree), humanBytesUint(report.Host.DiskBytes)))
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Index")
	printStatusField(stdout, "Path", report.Index.Path)
	printStatusField(stdout, "Ready", readiness(report.Index.Ready))
	if !report.Index.Ready {
		printStatusField(stdout, "Reason", report.Index.Reason)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Lookaside")
	printStatusField(stdout, "Cache", report.Lookaside.Cache)
	if report.Lookaside.Publish == nil {
		printStatusField(stdout, "Publish", "(not configured)")
	} else {
		printStatusField(stdout, "Publish", report.Lookaside.Publish.URL)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Training")
	if report.Training.Execution == nil {
		printStatusField(stdout, "Backend", "unavailable")
	} else {
		printTrainingExecution(stdout, *report.Training.Execution)
	}
	printStatusField(stdout, "Ready", readiness(report.Training.Ready))
	if !report.Training.Ready {
		printStatusField(stdout, "Reason", report.Training.Reason)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Overall")
	printStatusField(stdout, "Ready", readiness(report.Ready))
	if !report.Ready {
		for _, reason := range report.Reasons {
			printStatusField(stdout, "Reason", singleLine(reason))
		}
	}
	return nil
}

func printTrainingExecution(output io.Writer, execution training.Execution) {
	printStatusField(output, "Backend", execution.Backend.Name+"@"+execution.Backend.Revision)
	printStatusField(output, "Runtime", singleLine(execution.Runtime))
	if len(execution.Accelerators) == 0 {
		printStatusField(output, "Accelerator", "CPU")
		return
	}
	for _, accelerator := range execution.Accelerators {
		printStatusField(output, "Accelerator", fmt.Sprintf("%s, %s", acceleratorDisplay(accelerator), humanBytesUint(accelerator.MemoryBytes)))
	}
}

func printStatusField(output io.Writer, label, value string) {
	fmt.Fprintf(output, "  %-11s %s\n", label, singleLine(value))
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
