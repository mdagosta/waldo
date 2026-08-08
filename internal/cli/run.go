// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"io"

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
		return 2
	}
	return 1
}

type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

func wrapCobraUsageErrors(command *cobra.Command) {
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{message: err.Error()}
	})
}
