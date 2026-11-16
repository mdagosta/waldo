#!/bin/bash
# Dev container controls. Run on the Mac host from a checkout of md/waldito, not inside the container.
# See .devcontainer/README.md. Every openwaldo repo (each folder of ~/src/openwaldo with its own .git
# directory; worktrees are skipped) gets a folder in the container's repos volume.
#
#   .devcontainer/dev.sh build               build the app image only
#   .devcontainer/dev.sh up                  build and start, seed any repo not in the container yet, run setup
#   .devcontainer/dev.sh push REPO [BRANCH]  push a branch (default: that checkout's current) into the container
#   .devcontainer/dev.sh shell               shell in the app container
#   .devcontainer/dev.sh down                stop (volumes and data are kept)
#   .devcontainer/dev.sh restart             rerun the start (firewall, Claude policy from dotfiles); no rebuild
#   .devcontainer/dev.sh ssh-config          add "Host waldo-dev" to ~/.ssh/config, for git and herdr (run once)
set -euo pipefail

if [ -f /.dockerenv ]; then
    echo "dev.sh runs on the Mac host, not inside the container." >&2
    exit 1
fi

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
compose=(docker compose -f "$script_directory/compose.yaml")
# The checkout this runs from is a worktree of waldo, so the repos sit beside it.
mac_openwaldo="$(cd "$script_directory/../.." && pwd)"
container_openwaldo=/home/mdagosta/src/openwaldo
# Pushed into the waldo repo too when it's first seeded, so the personal tools are there.
extra_waldo_branch=md/waldito

mac_repos() {
    local directory
    for directory in "$mac_openwaldo"/*/; do
        [ -d "$directory.git" ] && basename "$directory"
    done
}

in_container() {
    "${compose[@]}" exec -T --user mdagosta app "$@"
}

# Git is the only bridge between the Mac's checkouts and the container's repos. Pushing runs
# git-receive-pack inside the container; fetching runs git-upload-pack inside. Neither runs anything from
# the container on this Mac: hooks and git config never travel.
push_to_container() {
    local repo="${1:-}" branch="${2:-}"
    local mac_repo="$mac_openwaldo/$repo" container_repo="$container_openwaldo/$repo"
    if [ -z "$repo" ] || [ ! -d "$mac_repo/.git" ]; then
        echo "push needs a repo, one of: $(mac_repos | tr '\n' ' ')" >&2
        exit 1
    fi
    branch="${branch:-$(git -C "$mac_repo" symbolic-ref --short HEAD)}"
    if ! grep -q "^Host waldo-dev\$" ~/.ssh/config 2>/dev/null; then
        echo "No 'Host waldo-dev' in ~/.ssh/config yet: run make dev-ssh-config first." >&2
        exit 1
    fi
    if ! git -C "$mac_repo" remote get-url waldo-dev > /dev/null 2>&1; then
        git -C "$mac_repo" remote add waldo-dev "waldo-dev:src/openwaldo/$repo"
        echo "$repo: added git remote waldo-dev"
    fi
    # Check every object fetched from the container, so a malformed one can't reach a git bug here.
    git -C "$mac_repo" config fetch.fsckObjects true
    # First push into a new folder: an empty repo on the branch being pushed, so the push checks it out.
    if ! in_container test -d "$container_repo/.git"; then
        in_container git init --quiet --initial-branch="$branch" "$container_repo"
        echo "$repo: created an empty repo in the container on $branch"
    fi
    # Pushing the checked-out branch updates the working tree, and is refused if it has uncommitted changes.
    in_container git -C "$container_repo" config receive.denyCurrentBranch updateInstead
    git -C "$mac_repo" push waldo-dev "$branch"
}

case "${1:-}" in
    build)
        "${compose[@]}" build app
        ;;
    up)
        "${compose[@]}" up -d --build
        # Seed only repos the container doesn't have yet, or has with no commits (a first push that
        # failed after git init). After that, updates are an explicit push.
        for repo in $(mac_repos); do
            if ! in_container git -C "$container_openwaldo/$repo" rev-parse --quiet --verify HEAD > /dev/null 2>&1; then
                push_to_container "$repo"
                if [ "$repo" = waldo ] && git -C "$mac_openwaldo/waldo" rev-parse --verify --quiet "$extra_waldo_branch" > /dev/null; then
                    push_to_container waldo "$extra_waldo_branch"
                fi
            fi
        done
        # The Mac's copy of post-create.sh, piped in, so setup doesn't depend on what's committed inside.
        in_container bash -s < "$script_directory/post-create.sh"
        ;;
    push)
        push_to_container "${2:-}" "${3:-}"
        ;;
    shell)
        "${compose[@]}" exec --user mdagosta app bash
        ;;
    down)
        "${compose[@]}" down
        ;;
    restart)
        # The entrypoint runs again: firewall re-resolved, Claude policy recopied from dotfiles. Ends every
        # shell and agent in the container.
        "${compose[@]}" restart app
        "${compose[@]}" logs app | grep -E 'firewall|entrypoint' | tail -2
        ;;
    ssh-config)
        # `ssh waldo-dev` (and herdr, and git) reach the container through docker exec: sshd runs once per
        # connection and never listens, so there's no port. Host keys are skipped because they change
        # on every rebuild and the connection never touches the network. Full docker path, since GUI
        # apps like herdr don't get the shell's PATH.
        if grep -q "^Host waldo-dev\$" ~/.ssh/config 2>/dev/null; then
            echo "already in ~/.ssh/config: Host waldo-dev"
            exit 0
        fi
        docker_path="$(command -v docker)"
        printf '\n# OpenWALDO dev container (waldo md/waldito: .devcontainer/dev.sh ssh-config)\nHost waldo-dev\n    User mdagosta\n    ProxyCommand %s exec -i waldo-app-1 /usr/sbin/sshd -i\n    IdentityFile ~/.ssh/id_orpheus\n    IdentitiesOnly yes\n    ForwardAgent no\n    StrictHostKeyChecking no\n    UserKnownHostsFile /dev/null\n    LogLevel ERROR\n' "$docker_path" >> ~/.ssh/config
        echo "added to ~/.ssh/config: Host waldo-dev (try: ssh waldo-dev)"
        ;;
    *)
        sed -n '2,13p' "$0"
        exit 1
        ;;
esac
