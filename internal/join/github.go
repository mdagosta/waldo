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

// gh runs the gh command, waiting out GitHub's API rate limit, which every
// identity sharing one GitHub account shares, for up to an hour.
func (joiner *Joiner) gh(ctx context.Context, args ...string) (string, error) {
	for attempt := 1; ; attempt++ {
		command := exec.CommandContext(ctx, "gh", args...)
		command.Dir = joiner.clone
		command.Env = joiner.environment()
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		if err == nil {
			return strings.TrimSpace(string(output)), nil
		}
		if !strings.Contains(strings.ToLower(stderr.String()), "rate limit") || attempt == 60 {
			return "", fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(stderr.Bytes()))
		}
		fmt.Fprintf(joiner.Log, "   GitHub API rate limit reached; waiting a minute\n")
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Minute):
		}
	}
}

// headRemote is where record branches go: the runs repo itself when this account can push to it, else its fork.
func (joiner *Joiner) headRemote(ctx context.Context) (string, string, error) {
	if joiner.remote != "" {
		return joiner.remote, joiner.owner, nil
	}
	// Remembered in the clone, so a restarted pass costs no API calls to find it again.
	if remembered, err := joiner.git(ctx, joiner.clone, "config", "--get", "waldo-join.head"); err == nil {
		if remote, owner, ok := strings.Cut(strings.TrimSpace(remembered), " "); ok {
			joiner.remote, joiner.owner = remote, owner
			return joiner.remote, joiner.owner, nil
		}
	}
	remote, owner, err := joiner.findHeadRemote(ctx)
	if err != nil {
		return "", "", err
	}
	if _, err := joiner.git(ctx, joiner.clone, "config", "waldo-join.head", remote+" "+owner); err != nil {
		return "", "", err
	}
	joiner.remote, joiner.owner = remote, owner
	return remote, owner, nil
}

func (joiner *Joiner) findHeadRemote(ctx context.Context) (string, string, error) {
	owner, repo := joiner.github[1], joiner.github[2]
	if push, err := joiner.gh(ctx, "api", "repos/"+owner+"/"+repo, "--jq", ".permissions.push"); err != nil {
		return "", "", err
	} else if push == "true" {
		return "origin", owner, nil
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
	return "fork", login, nil
}

// branch keeps each identity's record branches apart, so two closers' merge records never collide.
func (joiner *Joiner) branch(path string) string {
	relative, _ := filepath.Rel(filepath.Join(joiner.runDir, "rounds"), path)
	return "waldo-join/" + joiner.plan.Name + "/" + joiner.Identity + "/" + strings.TrimSuffix(filepath.ToSlash(relative), ".yaml")
}

// propose pushes the record branch and opens a pull request; true once the
// verifier merges it, false if the verifier closes it or another record for
// the same file lands first.
func (joiner *Joiner) propose(ctx context.Context, branch, path, message string) (bool, error) {
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
	relative, err := filepath.Rel(joiner.clone, path)
	if err != nil {
		return false, err
	}
	record := filepath.ToSlash(relative)
	ours, err := joiner.git(ctx, joiner.clone, "rev-parse", branch+":"+record)
	if err != nil {
		return false, err
	}
	state := "OPEN"
	for poll := range 180 {
		// Watch main with git, which GitHub does not rate limit; the API is asked only every
		// five minutes, for a pull request the verifier closed for a reason other than a lost race.
		if _, err := joiner.git(ctx, joiner.clone, "fetch", "-q", "origin"); err == nil {
			if landed, err := joiner.git(ctx, joiner.clone, "rev-parse", "origin/main:"+record); err == nil {
				state = "MERGED"
				if landed != ours {
					state = "CLOSED"
				}
				break
			}
		}
		if poll%30 == 29 {
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
