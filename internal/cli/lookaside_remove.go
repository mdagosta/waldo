// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"

	"github.com/openwaldo/waldo/internal/config"
	"github.com/openwaldo/waldo/internal/lookaside"
)

func runLookasideRemove(commandContext Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError{message: "usage: waldo lookaside rm <sha256>... [--json]"}
	}
	seen := make(map[string]struct{}, len(args))
	for _, name := range args {
		if err := lookaside.ValidateObjectName(name); err != nil {
			return usageError{message: err.Error()}
		}
		if _, exists := seen[name]; exists {
			return usageError{message: fmt.Sprintf("object %s is listed more than once", name)}
		}
		seen[name] = struct{}{}
	}

	configuration, err := config.Load()
	if err != nil {
		return err
	}
	if configuration.Lookaside.Publish == nil {
		return usageError{message: "configure a writable lookaside before removing objects: waldo config set lookaside <url>"}
	}
	remover, err := lookaside.NewObjectRemover(commandContext.Execution, *configuration.Lookaside.Publish)
	if err != nil {
		return err
	}

	// Complete the existence preflight before deleting anything so a typo does
	// not turn an otherwise correct list into a partial removal.
	for _, name := range args {
		present, err := remover.Contains(commandContext.Execution, name)
		if err != nil {
			return err
		}
		if !present {
			return fmt.Errorf("lookaside object %s does not exist at %s", name, remover.BaseURL())
		}
	}

	for _, name := range args {
		fmt.Fprintf(stderr, "removing %s from %s\n", name, remover.BaseURL())
		if err := remover.Remove(commandContext.Execution, name); err != nil {
			return err
		}
	}
	if commandContext.JSON {
		return writeJSON(stdout, struct {
			Lookaside string   `json:"lookaside"`
			Removed   []string `json:"removed"`
		}{Lookaside: remover.BaseURL(), Removed: args})
	}
	fmt.Fprintf(stdout, "removed %s object(s) from %s\n", humanInteger(int64(len(args))), remover.BaseURL())
	return nil
}
