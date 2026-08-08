// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/openwaldo/waldo/internal/config"
	managedgit "github.com/openwaldo/waldo/internal/git"
)

func runIndexStatus(context Context, args []string, stdout, stderr io.Writer) error {
	if len(args) != 0 {
		return usageError{message: "usage: waldo index status"}
	}
	root, label, err := selectedIndexCheckout(context, stderr)
	if err != nil {
		return err
	}
	state, err := managedgit.CheckoutStatus(root)
	if err != nil {
		return err
	}
	return writeIndexGitResult(context, stdout, managedgit.Result{Action: "status", State: state}, label)
}

func runIndexFetch(context Context, args []string, stdout, stderr io.Writer) error {
	if len(args) != 0 {
		return usageError{message: "usage: waldo index fetch"}
	}
	root, label, err := selectedIndexCheckout(context, stderr)
	if err != nil {
		return err
	}
	result, err := managedgit.CheckoutFetch(context.Execution, root, stderr)
	if err != nil {
		return err
	}
	return writeIndexGitResult(context, stdout, result, label)
}

func runIndexPull(context Context, args []string, stdout, stderr io.Writer) error {
	if len(args) != 0 {
		return usageError{message: "usage: waldo index pull"}
	}
	root, label, err := selectedIndexCheckout(context, stderr)
	if err != nil {
		return err
	}
	result, err := managedgit.CheckoutPull(context.Execution, root, stderr)
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

func runIndexClone(context Context, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return usageError{message: "usage: waldo index clone <directory>"}
	}
	destination, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	managed, err := config.IsManagedIndexPath(destination)
	if err != nil {
		return err
	}
	if managed {
		return usageError{message: "the managed index is cloned automatically; choose a different directory for a contributor checkout"}
	}
	result, err := indexGitManager.Clone(context.Execution, destination, stderr)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, result)
	}
	if err := writeIndexGitResult(context, stdout, result, "contributor checkout"); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Configure this contributor checkout with `waldo config set index %s`.\n", destination)
	return nil
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
