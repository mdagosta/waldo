// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package join

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// GitHub runs repos take records as pull requests, merged by the repo's verifier Action.
var githubURL = regexp.MustCompile(`^(?:https://github\.com/|git@github\.com:)([^/]+)/([^/]+?)(?:\.git)?/?$`)

var credentials = []string{"-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential"}

func (joiner *Joiner) gh(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "gh", args...)
	command.Dir = joiner.clone
	command.Env = joiner.environment()
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(stderr.Bytes()))
	}
	return strings.TrimSpace(string(output)), nil
}

// headRemote is where record branches go: the runs repo itself when this account can push to it, else its fork.
func (joiner *Joiner) headRemote(ctx context.Context) (string, string, error) {
	if joiner.remote != "" {
		return joiner.remote, joiner.owner, nil
	}
	owner, repo := joiner.github[1], joiner.github[2]
	if push, err := joiner.gh(ctx, "api", "repos/"+owner+"/"+repo, "--jq", ".permissions.push"); err != nil {
		return "", "", err
	} else if push == "true" {
		joiner.remote, joiner.owner = "origin", owner
		return joiner.remote, joiner.owner, nil
	}
	login, err := joiner.gh(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", "", err
	}
	// Forking needs more than a fork-only token, so only fork when the fork is missing.
	if _, err := joiner.gh(ctx, "api", "repos/"+login+"/"+repo, "--jq", ".fork"); err != nil {
		if _, err := joiner.gh(ctx, "repo", "fork", owner+"/"+repo, "--clone=false"); err != nil {
			return "", "", err
		}
	}
	fork := "https://github.com/" + login + "/" + repo + ".git"
	if _, err := joiner.git(ctx, joiner.clone, "remote", "set-url", "fork", fork); err != nil {
		if _, err := joiner.git(ctx, joiner.clone, "remote", "add", "fork", fork); err != nil {
			return "", "", err
		}
	}
	joiner.remote, joiner.owner = "fork", login
	return joiner.remote, joiner.owner, nil
}

// branch keeps each identity's record branches apart, so two closers' merge records never collide.
func (joiner *Joiner) branch(path string) string {
	relative, _ := filepath.Rel(filepath.Join(joiner.runDir, "rounds"), path)
	return "waldo-join/" + joiner.plan.Name + "/" + joiner.Identity + "/" + strings.TrimSuffix(filepath.ToSlash(relative), ".yaml")
}

// propose pushes the record branch and opens a pull request; true once the
// verifier merges it, false if the verifier closes it.
func (joiner *Joiner) propose(ctx context.Context, branch, message string) (bool, error) {
	remote, owner, err := joiner.headRemote(ctx)
	if err != nil {
		return false, err
	}
	head, err := joiner.git(ctx, joiner.clone, "rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	for attempt := 1; ; attempt++ {
		_, pushErr := joiner.git(ctx, joiner.clone, append(credentials, "push", "-q", "--force", remote, branch)...)
		if pushErr == nil {
			break
		}
		// A push can land even when git reports it failed (seen as "cannot lock ref" from GitHub).
		if listed, _ := joiner.git(ctx, joiner.clone, "ls-remote", remote, "refs/heads/"+branch); strings.Contains(listed, strings.TrimSpace(head)) {
			break
		}
		if attempt == 3 {
			return false, fmt.Errorf("push of %s failed 3 times; nothing was proposed: %w", branch, pushErr)
		}
		time.Sleep(time.Duration(15*attempt) * time.Second)
	}
	if _, err := joiner.git(ctx, joiner.clone, "checkout", "-q", "main"); err != nil {
		return false, err
	}
	url, err := joiner.gh(ctx, "pr", "create", "--repo", joiner.github[1]+"/"+joiner.github[2], "--base", "main", "--head", owner+":"+branch,
		"--title", message, "--body", "A signed record from `waldo model join`; the runs repo's verifier merges it if its checks pass.")
	if err != nil {
		return false, err
	}
	fmt.Fprintf(joiner.Log, "   %s: waiting for the verifier\n", url)
	state := "OPEN"
	for range 180 {
		output, err := joiner.gh(ctx, "pr", "view", url, "--json", "state")
		if err != nil {
			return false, err
		}
		var view struct{ State string }
		if err := json.Unmarshal([]byte(output), &view); err != nil {
			return false, err
		}
		if state = view.State; state != "OPEN" {
			break
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	if state == "OPEN" {
		return false, fmt.Errorf("%s is still open after 30 minutes; is the verifier Action enabled?", url)
	}
	_, _ = joiner.git(ctx, joiner.clone, "branch", "-q", "-D", branch)
	_, _ = joiner.git(ctx, joiner.clone, append(credentials, "push", "-q", remote, "--delete", branch)...)
	if _, err := joiner.git(ctx, joiner.clone, "pull", "-q", "--rebase"); err != nil {
		return false, err
	}
	if state != "MERGED" {
		fmt.Fprintf(joiner.Log, "%s was closed by the verifier; its comment says why\n", url)
	}
	return state == "MERGED", nil
}
