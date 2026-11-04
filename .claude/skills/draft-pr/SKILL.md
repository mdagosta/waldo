---
name: draft-pr
description: Draft an upstream pull request from the current feature branch using that repository's own PR template, and hand the user the title and body to paste into GitHub themselves.
disable-model-invocation: true
argument-hint: "[worktree path or branch; default: the current worktree]"
---

# Draft an upstream PR

The user signs off on every PR personally, so they must understand and stand behind everything
in it. Move at a pace they can follow: one PR at a time, plain words. The user opens the PR in
GitHub's web page and pastes the draft; this skill never posts, pushes, or uses `gh`.

## 1. Read the branch and its repository (no questions)

From the feature worktree (the argument, or the current directory):
- Upstream repo from the `origin` remote (e.g. `openwaldo/waldo`, `openwaldo/waldo-index`); the user's fork
  is the remote that points at `mdagosta/...`, named `mdagosta` (push with `git push mdagosta <branch>`);
  base is upstream's default branch (`git fetch origin` first).
- `git log --format='%h %s' origin/<base>..HEAD` and `git diff --stat origin/<base>...HEAD`.
- The repository's own template, from the branch, first found of: `.github/PULL_REQUEST_TEMPLATE.md`,
  `.github/pull_request_template.md`, `PULL_REQUEST_TEMPLATE.md`, `docs/PULL_REQUEST_TEMPLATE.md`, or one
  file from a `PULL_REQUEST_TEMPLATE/` directory (ask which if there are several). Use its sections and
  checkboxes exactly; never a remembered copy. With no template, use: What changed, Verification, Contracts.
- The repository's contribution rules if present (`CONTRIBUTING.md`, `.github/CONTRIBUTING.md`,
  `docs/CONTRIBUTING.md`): sign-off, required checks, ADRs, and anything else they ask a PR to state.

## 2. Preflight; report each result, fix nothing silently

- Working tree clean, and the branch contains `origin/<base>` (else say a rebase is needed; the user rebases).
- Every commit has a `Signed-off-by` trailer when the rules ask for DCO.
- The checks the contribution rules name (for waldo: `gofmt -l .` empty and `./testing/all.sh`).
  Never run tests while a `waldo model join` round is training (`pgrep -f "waldo model join"`); say so and wait.
  Note any suites that were skipped (MLX, Linux-only e2e) for the template's verification section.
- Privacy: scan the diff and the draft for anything private-looking (emails, tokens, keys, hostnames,
  internal URLs). Name each in one line and let the user decide. The username `mdagosta`, `/Users/mdagosta`
  paths, and the Mac/M2 Max/MLX setup are already public and fine.

## 3. Explain, then draft

First, 3-4 lines on what the change does and why upstream would want it, judged on its own merits.
Then fill the template:
- **Title**: under 72 characters, imperative, matching the main commit's style.
- Each section answered briefly and concretely; replace the template's comments, keep its headings.
- Verification lists only checks actually run in step 2, with the environment and any skips.
- Tick a checkbox only when step 2 confirmed it.
- No "Generated with Claude Code" footer or other tool attribution: the user doesn't want their PRs to
  carry tool marketing.

Write the body to the scratchpad as `pr-<branch>.md`.

## 4. Show everything, then revise

Show, in one message:
- the repo, the branch to open it from (`mdagosta:<branch>`), and the base;
- which template file was used;
- the commit list and the diff stat;
- the title, in a code block on its own, ready to copy;
- the body, in a code block, for reading and approval;
- the compare link to open: `https://github.com/<upstream>/compare/<base>...mdagosta:<branch>`

Revise on feedback and show the whole draft again.

Long lines copied out of the terminal arrive damaged (dropped words, joined lines), so the user never
copies the body from the terminal. Once they approve it, put the exact file on the clipboard with
`pbcopy < <body file>` and say so; to fix a PR already opened, do the same and have them paste it over
the description with Edit. After they paste, fetch the saved description from the public API
(`curl -s https://api.github.com/repos/<upstream>/pulls/<n>`) and diff it against the file.

## 5. Hand off

- If the fork's branch is behind local, say so; the user pushes (never force-push for them).
- Never commit, amend, rebase, push, or post: the user does those (DCO). Offer a one-line Slack note, as text only.
