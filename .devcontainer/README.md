# OpenWALDO dev container

Personal, on the `md/waldito` branch of mdagosta/waldo (not for upstream). Copied from aegis's
`.devcontainer` (and so from epiphyte's), with the same boundary: no credentials inside, no sudo, work
leaves only through a git fetch reviewed on the Mac, and outbound HTTPS is open to the public internet but
never to the Mac, the LAN or the Docker host.

**The split.** The container holds every openwaldo repo and is where code is written and tested. orpheus
keeps the minimum to run things: the Mac checkouts (the trusted copies that merge, sign off and push),
`~/.waldo` (236 GB of index, corpora and models), MLX on Metal, AWS/lookaside credentials, and SSH to the
GPU hosts. Training never runs in the container: there's no GPU, and MLX needs macOS.

## The shape

| Piece | File | Notes |
|---|---|---|
| Image | `Dockerfile` | Ubuntu 24.04, Go 1.27.2 (`GOTOOLCHAIN=local`), python3.12 with a venv holding CPU torch 2.15.0 and numpy, ripgrep, shellcheck, Claude Code, herdr; dev user, no sudo |
| Service | `compose.yaml` | Volumes: `repos` (`~/src/openwaldo`), `claude`, `go` (module and build caches), `waldo_home` (the container's own `~/.waldo`); read-only mounts of dotfiles and the SSH public key |
| Start | `entrypoint.sh` | Root: firewall, Claude policy from dotfiles, volume ownership, then drops to the dev user |
| Firewall | `firewall.sh`, `firewall-allowlist` | Mode from `WALDO_FIREWALL_MODE` (compose default `open-https`); fails closed |
| Setup | `post-create.sh` | Dotfiles links, `go mod download` per Go repo, versions; piped in from the Mac's copy by `dev.sh up` |
| Getting in | `sshd/waldo.conf` | `ssh waldo-dev` is `docker exec … sshd -i`; nothing listens |

## Daily use (on the Mac, in the waldo-waldito worktree)

```
make dev-ssh-config            # once: Host waldo-dev in ~/.ssh/config
make dev-up                    # build if needed, start, seed every repo it lacks, run setup
make dev-shell                 # or: ssh waldo-dev, or herdr
make dev-push REPO=waldo BRANCH=name   # or /git-push waldo-dev name, from that repo's checkout
```

`dev-up` seeds each folder of `~/src/openwaldo` that has its own `.git` (worktrees are skipped) with its
current branch, plus `md/waldito` for waldo, and adds a `waldo-dev` remote to each Mac checkout. Only
committed work travels; uncommitted changes stay on the Mac.

Getting work out: from the Mac checkout of that repo, `/git-pull waldo-dev <branch>` (or `git fetch
waldo-dev` and read `git log -p`). Then merge and push to GitHub from the Mac. The waldo checkout has an
`mdagosta` remote too, so name `waldo-dev` when the skills ask.

Inside: `cd ~/src/openwaldo/waldo && ./testing/all.sh` (or `make check` on md/waldito). MLX, CUDA and
TorchTitan tests report skips; the PyTorch lifecycle runs on CPU, which it can't on the Mac.

- **Review before merging.** After a merge into md/waldito, `make dev-*` runs the merged `Makefile` and
  `.devcontainer/` files on the Mac. Read every diff there with extra care.
- **Edit the allowlist or Dockerfile → rebuild** (`make dev-up`), not restart.
- **Bump Go or torch:** the Dockerfile's `GO_VERSION`/`GO_SHA256` or `TORCH_WHEEL`/`TORCH_SHA256`, then
  rebuild. Pick releases at least two weeks old.
- A rebuild ends every shell and agent in the container. Commit and fetch first. It also gives herdr-core
  a new identity, so herdr calls the machine unreachable until `hc machine rm waldo-dev && hc machine add
  waldo-dev`; then `/herdr-space waldo` makes the space again.
- The engine is Colima on orpheus (`colima start`; `brew services start colima` to start it at login).
  `host.docker.internal` is `192.168.5.2` there, inside the refused `192.168/16`.
- The machine-specific Claude file is `CLAUDE.waldo-dev.md` in the dotfiles, if you want one.

## Firewall modes

Same as aegis's (see aegis `docs/dev-container.md`): `open-https` refuses `10/8`, `172.16/12`,
`192.168/16`, `169.254/16`, `100.64/10`, `0/8`, `198.18/15`, multicast and reserved space, and the start
fails unless every rule is in place. `WALDO_FIREWALL_MODE=allowlist make dev-up` allows only
`firewall-allowlist` plus GitHub (Go proxy, PyPI and Hugging Face are listed).

In `open-https`, anything readable inside (Claude's login, the repos) can be sent anywhere. Keep real
secrets out: no AWS keys, no `waldo lookaside login`, no Hugging Face tokens. S3 and public-index live
tests run on the Mac.

## Boundary checklist

The aegis checklist, with `waldo-` names (`/usr/local/bin/waldo-firewall`), plus:

| Check | Command | Expect |
|---|---|---|
| Go stays local | `go env GOTOOLCHAIN` | `local` |
| torch on CPU | `python3 -c 'import torch; print(torch.cuda.is_available())'` | `False` |
| Mac's ~/.waldo absent | `du -sh ~/.waldo` | small; its own volume |
