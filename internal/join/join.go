// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package join

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/openwaldo/waldo/internal/config"
)

// Joiner is one identity's view of a run: its own clone of the runs repo,
// signing key, and working directory under Home.
type Joiner struct {
	Home     string // per identity work: clone, models, exports
	Identity string
	// Credentials holds the identity's signing key and an optional
	// github-token, apart from its work, e.g. ~/.waldo/identity/<identity>.
	Credentials string
	Key         string // ssh signing key; Credentials/signing-key when empty
	Email       string // commit author email
	Waldo       string // this waldo executable, run with the identity's own config
	Log         io.Writer
	Now         func() time.Time

	clone  string
	runDir string
	plan   Plan
	github []string // owner and repo when the runs repo is on GitHub
	token  string   // GitHub token for this identity's gh and git calls
	remote string   // where record branches are pushed, once known
	owner  string
}

// Open clones the runs repo when needed and makes sure the signing key exists.
func (joiner *Joiner) Open(ctx context.Context, url, runDir string) error {
	if joiner.Now == nil {
		joiner.Now = time.Now
	}
	if joiner.Log == nil {
		joiner.Log = io.Discard
	}
	if absolute, err := filepath.Abs(url); err == nil {
		if _, statErr := os.Stat(absolute); statErr == nil {
			url = absolute
		}
	}
	joiner.github = githubURL.FindStringSubmatch(url)
	name := strings.TrimSuffix(filepath.Base(strings.TrimRight(url, "/")), ".git")
	joiner.clone = filepath.Join(joiner.Home, name)
	joiner.runDir = filepath.Join(joiner.clone, filepath.FromSlash(runDir))
	if err := os.MkdirAll(joiner.Home, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(joiner.clone, ".git")); os.IsNotExist(err) {
		if _, err := joiner.git(ctx, joiner.Home, "clone", "-q", url, joiner.clone); err != nil {
			return err
		}
	}
	if err := joiner.loadToken(); err != nil {
		return err
	}
	if err := joiner.configure(ctx); err != nil {
		return err
	}
	if joiner.Key == "" {
		joiner.Key = filepath.Join(joiner.Credentials, "signing-key")
		if err := os.MkdirAll(joiner.Credentials, 0o700); err != nil {
			return err
		}
	}
	if _, err := os.Stat(joiner.Key); os.IsNotExist(err) {
		comment := "waldo " + joiner.Identity
		if output, err := exec.CommandContext(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", comment, "-f", joiner.Key).CombinedOutput(); err != nil {
			return fmt.Errorf("create signing key: %w: %s", err, bytes.TrimSpace(output))
		}
	}
	return nil
}

// loadToken reads Credentials/github-token when present, so this identity's
// gh and git calls use its own GitHub account; like ssh with private keys,
// it refuses a token file that others can read.
func (joiner *Joiner) loadToken() error {
	path := filepath.Join(joiner.Credentials, "github-token")
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is readable by others; run chmod 600 %s", path, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	joiner.token = strings.TrimSpace(string(data))
	return nil
}

// environment passes this identity's GitHub token, if it has one, to gh and
// to git's gh credential helper; otherwise the caller's environment stands.
func (joiner *Joiner) environment() []string {
	if joiner.token == "" {
		return nil
	}
	return append(os.Environ(), "GH_TOKEN="+joiner.token)
}

// configure gives the identity its own copy of the waldo config with its own
// model store, so identities on one machine never share a model name.
func (joiner *Joiner) configure(ctx context.Context) error {
	if _, err := os.Stat(joiner.config()); err == nil {
		return nil
	}
	if source, err := config.Path(); err == nil {
		if data, err := os.ReadFile(source); err == nil {
			if err := os.WriteFile(joiner.config(), data, 0o600); err != nil {
				return err
			}
		}
	}
	return joiner.waldo(ctx, "config", "set", "model.root", joiner.models())
}

// Step does one unit of work and reports "done", "idle", or "worked".
func (joiner *Joiner) Step(ctx context.Context) (string, error) {
	if err := joiner.sync(ctx); err != nil {
		return "", err
	}
	state, err := joiner.load()
	if err != nil {
		return "", err
	}
	action := state.Next(joiner.Identity, joiner.Now())
	switch action.Kind {
	case "done":
		final := state.Rounds[action.Round].Merge
		fmt.Fprintf(joiner.Log, "run %s is complete: %s (%s), %s\n", joiner.plan.Name, final.Model, final.ModelSHA256[:12], final.URL)
		return "done", nil
	case "idle":
		fmt.Fprintf(joiner.Log, "round %d: nothing for %s until others submit\n", action.Round, joiner.Identity)
		return "idle", nil
	case "contribute":
		return "worked", joiner.contribute(ctx, state, action)
	default:
		return "worked", joiner.close(ctx, state, action)
	}
}

// load reads the plan as of this pass, since amendments land through git, and checks this identity is in it.
func (joiner *Joiner) load() (State, error) {
	plan, err := LoadPlan(filepath.Join(joiner.runDir, "plan.yaml"))
	if err != nil {
		return State{}, err
	}
	public, err := os.ReadFile(joiner.Key + ".pub")
	if err != nil {
		return State{}, err
	}
	key := strings.Join(strings.Fields(string(public))[:2], " ")
	if plan.Identities[joiner.Identity].Key != key {
		return State{}, fmt.Errorf("%s is not in %s with this key; add under identities:\n  %s:\n    key: %s", joiner.Identity, filepath.Join(joiner.runDir, "plan.yaml"), joiner.Identity, key)
	}
	joiner.plan = plan
	allowed := filepath.Join(joiner.workDir(), "allowed_signers")
	if err := os.MkdirAll(joiner.workDir(), 0o755); err != nil {
		return State{}, err
	}
	if err := writeAllowedSigners(allowed, plan.Identities); err != nil {
		return State{}, err
	}
	return ReadState(joiner.runDir, plan, allowed)
}

func (joiner *Joiner) workDir() string {
	return filepath.Join(joiner.Home, "work", filepath.Base(joiner.runDir))
}

// commit signs record into path and pushes it; false if another closer's merge record won the race.
func (joiner *Joiner) commit(ctx context.Context, path string, record any, message string) (bool, error) {
	branch := joiner.branch(path)
	if joiner.github != nil {
		if _, err := joiner.git(ctx, joiner.clone, "fetch", "-q", "origin"); err != nil {
			return false, err
		}
		if _, err := joiner.git(ctx, joiner.clone, "checkout", "-q", "-B", branch, "origin/HEAD"); err != nil {
			return false, err
		}
	}
	if err := writeSigned(path, record, joiner.Key); err != nil {
		return false, err
	}
	if _, err := joiner.git(ctx, joiner.clone, "add", path, path+".sig"); err != nil {
		return false, err
	}
	if _, err := joiner.git(ctx, joiner.clone, "-c", "user.name="+joiner.Identity, "-c", "user.email="+joiner.Email, "commit", "-q", "-s", "-m", message); err != nil {
		return false, err
	}
	if joiner.github != nil {
		return joiner.propose(ctx, branch, message)
	}
	return joiner.push(ctx)
}

func (joiner *Joiner) push(ctx context.Context) (bool, error) {
	for range 5 {
		if _, err := joiner.git(ctx, joiner.clone, "push", "-q"); err == nil {
			return true, nil
		}
		if _, err := joiner.git(ctx, joiner.clone, "pull", "-q", "--rebase"); err != nil {
			// Only another merge.yaml for the same round can conflict; theirs stands.
			_, _ = joiner.git(ctx, joiner.clone, "rebase", "--abort")
			_, resetErr := joiner.git(ctx, joiner.clone, "reset", "-q", "--hard", "@{u}")
			return false, resetErr
		}
	}
	return false, fmt.Errorf("push kept being rejected; try again later")
}

// sync brings the clone up to date and pushes any record a cut-short pass committed but never pushed.
func (joiner *Joiner) sync(ctx context.Context) error {
	if joiner.github != nil {
		// A pass cut short mid-proposal leaves its record branch checked out.
		if _, err := joiner.git(ctx, joiner.clone, "checkout", "-q", "main"); err != nil {
			return err
		}
		_, err := joiner.git(ctx, joiner.clone, "pull", "-q", "--rebase")
		return err
	}
	if _, err := joiner.git(ctx, joiner.clone, "pull", "-q", "--rebase"); err != nil {
		return err
	}
	_, err := joiner.push(ctx)
	return err
}

func (joiner *Joiner) git(ctx context.Context, directory string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	command.Env = joiner.environment()
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(output))
	}
	return string(output), nil
}
