// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/openwaldo/waldo/internal/config"
	managedgit "github.com/openwaldo/waldo/internal/git"
)

func TestManagedIndexAutoClonesForDefaultRead(t *testing.T) {
	upstream := fixtureGitIndex(t)
	useTestIndexManager(t, upstream)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WALDO_CONFIG", filepath.Join(home, "config.json"))
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"index", "list"}, &stdout, &stderr); code != 0 {
		t.Fatalf("list code = %d, stderr = %q", code, stderr.String())
	}
	managed := filepath.Join(home, ".waldo", "index")
	if !strings.Contains(stderr.String(), "cloning managed index") || !strings.Contains(stderr.String(), "warning: no index path specified; using the entire managed index "+managed) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(managed, "index.json")); err != nil {
		t.Fatalf("managed clone missing: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--json", "index", "status"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), `"relation": "current"`) {
		t.Fatalf("status code output=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestManagedIndexRejectsAuthoring(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WALDO_CONFIG", filepath.Join(home, "config.json"))
	managed := filepath.Join(home, ".waldo", "index")

	var stdout, stderr bytes.Buffer
	args := []string{"index", "ingest", "input.txt", "core/new", "--title", "New", "--license", "CC0-1.0", "--source", "https://example.invalid", "--source-category", "public-dataset", "--dry-run"}
	if code := Run(args, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "managed read-only index") {
		t.Fatalf("ingest code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"index", "init", managed}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "managed read-only index") {
		t.Fatalf("init code=%d stderr=%q", code, stderr.String())
	}
}

func TestManagedIndexRejectsCorpusUpdateAndConfigurationOverride(t *testing.T) {
	upstream := fixtureGitIndex(t)
	useTestIndexManager(t, upstream)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WALDO_CONFIG", filepath.Join(home, "config.json"))
	managed := filepath.Join(home, ".waldo", "index")

	var stdout, stderr bytes.Buffer
	args := []string{"index", "update", "input.txt", "books/books.json", "--title", "Books", "--license", "CC0-1.0", "--source", "https://example.invalid", "--source-category", "public-dataset", "--dry-run"}
	if code := Run(args, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "cannot update the managed read-only index") {
		t.Fatalf("update code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"config", "set", "index", managed}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "already the default") {
		t.Fatalf("config set code=%d stderr=%q", code, stderr.String())
	}
}

func TestConfigIndexShowsManagedDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WALDO_CONFIG", filepath.Join(home, "config.json"))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"config", "get", "index"}, &stdout, &stderr); code != 0 {
		t.Fatalf("config get code=%d stderr=%q", code, stderr.String())
	}
	want := filepath.Join(home, ".waldo", "index")
	if strings.TrimSpace(stdout.String()) != want {
		t.Fatalf("config index = %q, want %q", strings.TrimSpace(stdout.String()), want)
	}
}

func TestIndexCloneCreatesContributorCheckout(t *testing.T) {
	upstream := fixtureGitIndex(t)
	useTestIndexManager(t, upstream)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WALDO_CONFIG", filepath.Join(home, "config.json"))
	destination := filepath.Join(t.TempDir(), "contributor")

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"index", "clone", destination}, &stdout, &stderr); code != 0 {
		t.Fatalf("clone code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Configure this contributor checkout") {
		t.Fatalf("clone output = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"config", "set", "index", destination}, &stdout, &stderr); code != 0 {
		t.Fatalf("config set code=%d stderr=%q", code, stderr.String())
	}
	configuration, err := config.Load()
	if err != nil || configuration.Index != destination {
		t.Fatalf("configuration=%+v err=%v", configuration, err)
	}
}

func fixtureGitIndex(t *testing.T) string {
	t.Helper()
	root := fixtureCLIIndex(t)
	repository, err := git.PlainInitWithOptions(root, &git.PlainInitOptions{InitOptions: git.InitOptions{DefaultBranch: "refs/heads/main"}})
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := worktree.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	_, err = worktree.Commit("fixture", &git.CommitOptions{Author: &object.Signature{Name: "WALDO Test", Email: "test@example.invalid", When: time.Unix(1, 0).UTC()}})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func useTestIndexManager(t *testing.T, upstream string) {
	t.Helper()
	previous := indexGitManager
	indexGitManager = managedgit.Manager{URL: upstream, Branch: "main"}
	t.Cleanup(func() { indexGitManager = previous })
}
