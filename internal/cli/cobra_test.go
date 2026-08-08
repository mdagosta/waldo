// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import "testing"

func parseCobraCommand(t *testing.T, path, arguments []string) (Context, []string, error) {
	t.Helper()
	root := newRootCommand()
	command, _, err := root.Find(path)
	if err != nil {
		return Context{}, nil, err
	}
	if err := command.ParseFlags(arguments); err != nil {
		return Context{}, nil, err
	}
	return Context{Execution: t.Context(), Command: command}, command.Flags().Args(), nil
}
