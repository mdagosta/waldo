// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

// Package git owns Git transport for WALDO-managed checkouts. It uses a Go
// implementation directly and never invokes an external git executable.
package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

const (
	UpstreamURL   = "https://github.com/openwaldo/waldo-index.git"
	DefaultBranch = "main"
)

// Manager synchronizes one checkout with one fixed upstream branch.
type Manager struct {
	URL    string
	Branch string
}

// DefaultManager describes the canonical OpenWALDO index upstream.
func DefaultManager() Manager { return Manager{URL: UpstreamURL, Branch: DefaultBranch} }

// State is the locally observable state of a checkout. Relation compares HEAD
// with the last fetched origin branch and is current, behind, ahead, diverged,
// or unknown.
type State struct {
	Path           string `json:"path"`
	Remote         string `json:"remote"`
	Branch         string `json:"branch"`
	Commit         string `json:"commit"`
	UpstreamCommit string `json:"upstream_commit,omitempty"`
	Relation       string `json:"relation"`
	Dirty          bool   `json:"dirty"`
}

// Result reports whether synchronization cloned, fetched, updated, or found a
// checkout already current.
type Result struct {
	Action string `json:"action"`
	State  State  `json:"state"`
}

// Repository describes the revision and worktree state of any ordinary local
// checkout. It is used for provenance without invoking an external command.
type Repository struct {
	Root   string
	Remote string
	Commit string
	Dirty  bool
}

// Inspect discovers and inspects the checkout containing location.
func Inspect(location string) (Repository, error) {
	repository, err := git.PlainOpenWithOptions(location, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return Repository{}, err
	}
	worktree, err := repository.Worktree()
	if err != nil {
		return Repository{}, err
	}
	head, err := repository.Head()
	if err != nil {
		return Repository{}, err
	}
	status, err := worktree.Status()
	if err != nil {
		return Repository{}, err
	}
	remoteURL := ""
	if remote, err := repository.Remote("origin"); err == nil && len(remote.Config().URLs) > 0 {
		remoteURL = remote.Config().URLs[0]
	}
	return Repository{Root: worktree.Filesystem.Root(), Remote: remoteURL, Commit: head.Hash().String(), Dirty: !status.IsClean()}, nil
}

// Ensure opens an existing managed checkout or atomically clones it when it is
// absent. It does not contact the network for an existing checkout.
func (manager Manager) Ensure(ctx context.Context, destination string, progress io.Writer) (Result, error) {
	if _, err := os.Stat(destination); err == nil {
		state, err := manager.Status(destination)
		return Result{Action: "current", State: state}, err
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("inspect managed index %s: %w", destination, err)
	}
	if progress != nil {
		fmt.Fprintf(progress, "cloning managed index from %s\n", manager.URL)
	}
	if err := manager.cloneAtomic(ctx, destination, progress); err != nil {
		return Result{}, err
	}
	state, err := manager.Status(destination)
	return Result{Action: "cloned", State: state}, err
}

// Clone creates a contributor checkout at a new destination.
func (manager Manager) Clone(ctx context.Context, destination string, progress io.Writer) (Result, error) {
	if _, err := os.Stat(destination); err == nil {
		return Result{}, fmt.Errorf("clone destination %s already exists", destination)
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("inspect clone destination %s: %w", destination, err)
	}
	if err := manager.cloneAtomic(ctx, destination, progress); err != nil {
		return Result{}, err
	}
	state, err := manager.Status(destination)
	return Result{Action: "cloned", State: state}, err
}

// Fetch refreshes origin/main without changing the worktree. A missing managed
// checkout is cloned because there is no local repository to fetch yet.
func (manager Manager) Fetch(ctx context.Context, destination string, progress io.Writer) (Result, error) {
	ensured, err := manager.Ensure(ctx, destination, progress)
	if err != nil || ensured.Action == "cloned" {
		return ensured, err
	}
	repository, err := manager.open(destination)
	if err != nil {
		return Result{}, err
	}
	err = repository.FetchContext(ctx, &git.FetchOptions{RemoteName: "origin", Progress: progress, Prune: true})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return Result{}, fmt.Errorf("fetch managed index %s: %w", destination, err)
	}
	state, err := manager.status(repository, destination)
	return Result{Action: "fetched", State: state}, err
}

// Pull fetches and fast-forwards a clean managed checkout. It refuses local
// changes, local-only commits, detached HEADs, and divergent history.
func (manager Manager) Pull(ctx context.Context, destination string, progress io.Writer) (Result, error) {
	result, err := manager.Fetch(ctx, destination, progress)
	if err != nil || result.Action == "cloned" {
		return result, err
	}
	state := result.State
	if state.Dirty {
		return Result{}, fmt.Errorf("managed index %s has local changes; restore or remove them before updating", destination)
	}
	switch state.Relation {
	case "current":
		return Result{Action: "current", State: state}, nil
	case "ahead", "diverged", "unknown":
		return Result{}, fmt.Errorf("managed index %s is %s relative to origin/%s; WALDO only performs clean fast-forward updates", destination, state.Relation, manager.Branch)
	case "behind":
	default:
		return Result{}, fmt.Errorf("managed index %s has unsupported relation %q", destination, state.Relation)
	}
	repository, err := manager.open(destination)
	if err != nil {
		return Result{}, err
	}
	upstream, err := repository.Reference(plumbing.NewRemoteReferenceName("origin", manager.Branch), true)
	if err != nil {
		return Result{}, fmt.Errorf("resolve origin/%s in managed index %s: %w", manager.Branch, destination, err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		return Result{}, fmt.Errorf("open managed index worktree %s: %w", destination, err)
	}
	if err := worktree.Reset(&git.ResetOptions{Commit: upstream.Hash(), Mode: git.HardReset}); err != nil {
		return Result{}, fmt.Errorf("fast-forward managed index %s: %w", destination, err)
	}
	state, err = manager.status(repository, destination)
	return Result{Action: "updated", State: state}, err
}

// Status inspects a checkout without network access.
func (manager Manager) Status(destination string) (State, error) {
	repository, err := manager.open(destination)
	if err != nil {
		return State{}, err
	}
	return manager.status(repository, destination)
}

func (manager Manager) cloneAtomic(ctx context.Context, destination string, progress io.Writer) error {
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	parent := filepath.Dir(absolute)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create index parent %s: %w", parent, err)
	}
	temporary, err := os.MkdirTemp(parent, ".waldo-index-clone-*")
	if err != nil {
		return fmt.Errorf("create temporary index checkout: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(temporary)
		}
	}()
	_, err = git.PlainCloneContext(ctx, temporary, false, &git.CloneOptions{
		URL:           manager.URL,
		ReferenceName: plumbing.NewBranchReferenceName(manager.Branch),
		SingleBranch:  true,
		Progress:      progress,
	})
	if err != nil {
		return fmt.Errorf("clone %s branch %s: %w", manager.URL, manager.Branch, err)
	}
	if err := os.Rename(temporary, absolute); err != nil {
		return fmt.Errorf("publish cloned index at %s: %w", absolute, err)
	}
	committed = true
	return nil
}

func (manager Manager) open(destination string) (*git.Repository, error) {
	repository, err := git.PlainOpen(destination)
	if err != nil {
		return nil, fmt.Errorf("open managed index %s: %w", destination, err)
	}
	remote, err := repository.Remote("origin")
	if err != nil {
		return nil, fmt.Errorf("managed index %s has no origin remote: %w", destination, err)
	}
	urls := remote.Config().URLs
	if len(urls) != 1 || urls[0] != manager.URL {
		return nil, fmt.Errorf("managed index %s origin is %v, want %s", destination, urls, manager.URL)
	}
	return repository, nil
}

func (manager Manager) status(repository *git.Repository, destination string) (State, error) {
	head, err := repository.Head()
	if err != nil {
		return State{}, fmt.Errorf("read managed index HEAD %s: %w", destination, err)
	}
	wantBranch := plumbing.NewBranchReferenceName(manager.Branch)
	if head.Name() != wantBranch {
		return State{}, fmt.Errorf("managed index %s is on %s, want branch %s", destination, head.Name(), manager.Branch)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		return State{}, fmt.Errorf("open managed index worktree %s: %w", destination, err)
	}
	worktreeStatus, err := worktree.Status()
	if err != nil {
		return State{}, fmt.Errorf("inspect managed index worktree %s: %w", destination, err)
	}
	state := State{Path: destination, Remote: manager.URL, Branch: manager.Branch, Commit: head.Hash().String(), Relation: "unknown", Dirty: !worktreeStatus.IsClean()}
	upstream, err := repository.Reference(plumbing.NewRemoteReferenceName("origin", manager.Branch), true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return state, nil
		}
		return State{}, fmt.Errorf("read managed index upstream reference: %w", err)
	}
	state.UpstreamCommit = upstream.Hash().String()
	if head.Hash() == upstream.Hash() {
		state.Relation = "current"
		return state, nil
	}
	headCommit, err := repository.CommitObject(head.Hash())
	if err != nil {
		return State{}, err
	}
	upstreamCommit, err := repository.CommitObject(upstream.Hash())
	if err != nil {
		return State{}, err
	}
	headAncestor, err := headCommit.IsAncestor(upstreamCommit)
	if err != nil {
		return State{}, err
	}
	if headAncestor {
		state.Relation = "behind"
		return state, nil
	}
	upstreamAncestor, err := upstreamCommit.IsAncestor(headCommit)
	if err != nil {
		return State{}, err
	}
	if upstreamAncestor {
		state.Relation = "ahead"
	} else {
		state.Relation = "diverged"
	}
	return state, nil
}
