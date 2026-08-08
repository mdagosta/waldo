// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

const version = "0.0.0-dev"

// Run executes WALDO's Cobra command tree with process-independent streams.
func Run(args []string, stdout, stderr io.Writer) int {
	return RunContext(context.Background(), args, stdout, stderr)
}

func RunContext(execution context.Context, args []string, stdout, stderr io.Writer) int {
	root := newRootCommand()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(execution)
	if err == nil {
		return 0
	}
	var usage usageError
	if errors.As(err, &usage) {
		writeUsageError(stderr, usage.Error())
		return 2
	}
	fmt.Fprintf(stderr, "waldo: %v\n", err)
	return 1
}

type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

func writeUsageError(output io.Writer, message string) {
	if strings.HasPrefix(message, "usage: ") {
		fmt.Fprintf(output, "Usage:\n  %s\n", strings.TrimPrefix(message, "usage: "))
		return
	}
	fmt.Fprintf(output, "waldo: %s\n", message)
}

func wrapCobraUsageErrors(command *cobra.Command) {
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{message: err.Error()}
	})
}
