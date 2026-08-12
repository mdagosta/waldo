// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"

	"github.com/openwaldo/waldo/internal/config"
	managedgit "github.com/openwaldo/waldo/internal/git"
)

func runIndexPull(context Context, _ []string, stdout, stderr io.Writer) error {
	root, label, err := selectedIndexCheckout(context, stderr)
	if err != nil {
		return err
	}
	var result managedgit.Result
	if label == "managed index" {
		result, err = indexGitManager.PullManaged(context.Execution, root, stderr)
	} else {
		result, err = managedgit.CheckoutPull(context.Execution, root, stderr)
	}
	if err != nil {
		return err
	}
	return writeIndexGitResult(context, stdout, result, label)
}

func selectedIndexCheckout(context Context, progress io.Writer) (string, string, error) {
	configuration, err := config.Load()
	if err != nil {
		return "", "", err
	}
	root, managed, err := config.EffectiveIndexRoot(configuration)
	if err != nil {
		return "", "", err
	}
	label := "configured index"
	if managed {
		label = "managed index"
		if _, err := indexGitManager.Ensure(context.Execution, root, progress); err != nil {
			return "", "", err
		}
	}
	return root, label, nil
}

func writeIndexGitResult(context Context, stdout io.Writer, result managedgit.Result, label string) error {
	if context.JSON {
		return writeJSON(stdout, result)
	}
	state := result.State
	commit := state.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	fmt.Fprintf(stdout, "%s %s\n", label, result.Action)
	fmt.Fprintf(stdout, "  path      %s\n", state.Path)
	fmt.Fprintf(stdout, "  upstream  %s (%s/%s)\n", state.Remote, state.RemoteName, state.UpstreamBranch)
	fmt.Fprintf(stdout, "  branch    %s\n", state.Branch)
	fmt.Fprintf(stdout, "  revision  %s\n", commit)
	fmt.Fprintf(stdout, "  relation  %s\n", state.Relation)
	fmt.Fprintf(stdout, "  worktree  %s\n", map[bool]string{true: "modified", false: "clean"}[state.Dirty])
	return nil
}
