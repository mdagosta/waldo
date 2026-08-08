// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestManagedCheckoutCloneFetchAndPull(t *testing.T) {
	upstream := fixtureUpstream(t)
	manager := Manager{URL: upstream, Branch: "main"}
	destination := filepath.Join(t.TempDir(), "managed")

	result, err := manager.Ensure(context.Background(), destination, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "cloned" || result.State.Relation != "current" {
		t.Fatalf("initial ensure = %+v", result)
	}

	commitFile(t, upstream, "science/index.yaml", "kind: index\nschema: 1\nname: science\n")
	result, err = CheckoutFetch(context.Background(), destination, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "fetched" || result.State.Relation != "behind" {
		t.Fatalf("fetch = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(destination, "science", "index.yaml")); !os.IsNotExist(err) {
		t.Fatalf("fetch changed worktree: %v", err)
	}

	result, err = CheckoutPull(context.Background(), destination, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "updated" || result.State.Relation != "current" {
		t.Fatalf("pull = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(destination, "science", "index.yaml")); err != nil {
		t.Fatalf("pull did not update worktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(destination, "local.txt"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckoutPull(context.Background(), destination, nil); err == nil || !strings.Contains(err.Error(), "is dirty") {
		t.Fatalf("dirty pull error = %v", err)
	}
}

func TestGitTransportWritesOnlyConciseProgress(t *testing.T) {
	upstream := fixtureUpstream(t)
	manager := Manager{URL: upstream, Branch: "main"}
	destination := filepath.Join(t.TempDir(), "managed")
	var progress bytes.Buffer

	if _, err := manager.Ensure(context.Background(), destination, &progress); err != nil {
		t.Fatal(err)
	}
	want := "cloning managed index from " + upstream + "\n"
	if progress.String() != want {
		t.Fatalf("clone progress = %q, want %q", progress.String(), want)
	}

	commitFile(t, upstream, "new.yaml", "new: true\n")
	progress.Reset()
	if _, err := CheckoutFetch(context.Background(), destination, &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Len() != 0 {
		t.Fatalf("fetch leaked transport progress: %q", progress.String())
	}
}

func TestCloneRefusesExistingDestination(t *testing.T) {
	upstream := fixtureUpstream(t)
	manager := Manager{URL: upstream, Branch: "main"}
	destination := t.TempDir()
	if _, err := manager.Clone(context.Background(), destination, nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("clone error = %v", err)
	}
}

func TestCheckoutPullRefusesDirtyAheadAndDiverged(t *testing.T) {
	upstream := fixtureUpstream(t)
	manager := Manager{URL: upstream, Branch: "main"}
	dirty := filepath.Join(t.TempDir(), "dirty")
	if _, err := manager.Clone(context.Background(), dirty, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirty, "local.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckoutPull(context.Background(), dirty, nil); err == nil || !strings.Contains(err.Error(), "is dirty") {
		t.Fatalf("dirty checkout pull error = %v", err)
	}

	ahead := filepath.Join(t.TempDir(), "ahead")
	if _, err := manager.Clone(context.Background(), ahead, nil); err != nil {
		t.Fatal(err)
	}
	commitFile(t, ahead, "local.yaml", "local: true\n")
	if _, err := CheckoutPull(context.Background(), ahead, nil); err == nil || !strings.Contains(err.Error(), "is ahead") {
		t.Fatalf("ahead checkout pull error = %v", err)
	}

	commitFile(t, upstream, "remote.yaml", "remote: true\n")
	if _, err := CheckoutPull(context.Background(), ahead, nil); err == nil || !strings.Contains(err.Error(), "is diverged") {
		t.Fatalf("diverged checkout pull error = %v", err)
	}
}

func TestCheckoutPullFastForwardsConfiguredTrackingBranch(t *testing.T) {
	upstream := fixtureUpstream(t)
	manager := Manager{URL: upstream, Branch: "main"}
	destination := filepath.Join(t.TempDir(), "configured")
	if _, err := manager.Clone(context.Background(), destination, nil); err != nil {
		t.Fatal(err)
	}
	commitFile(t, upstream, "new.yaml", "new: true\n")
	result, err := CheckoutPull(context.Background(), destination, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "updated" || result.State.Relation != "current" {
		t.Fatalf("checkout pull = %+v", result)
	}
}

func fixtureUpstream(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "upstream")
	repository, err := git.PlainInitWithOptions(root, &git.PlainInitOptions{InitOptions: git.InitOptions{DefaultBranch: "refs/heads/main"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.yaml"), []byte("kind: index\nschema: 1\nname: root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("index.yaml"); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit("initial", &git.CommitOptions{Author: testSignature()}); err != nil {
		t.Fatal(err)
	}
	return root
}

func commitFile(t *testing.T, root, name, contents string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	repository, err := git.PlainOpen(root)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(filepath.ToSlash(name)); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit("update", &git.CommitOptions{Author: testSignature()}); err != nil {
		t.Fatal(err)
	}
}

func testSignature() *object.Signature {
	return &object.Signature{Name: "WALDO Test", Email: "test@example.invalid", When: time.Unix(1, 0).UTC()}
}
