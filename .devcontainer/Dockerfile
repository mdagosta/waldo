# OpenWALDO dev container: Go, Python with CPU PyTorch, and every openwaldo repo in one volume. Copied
# from aegis's .devcontainer (itself from epiphyte's); see .devcontainer/README.md.
# Build context is .devcontainer/ only, so the repos (and anything secret in them) never enter the image.
FROM ubuntu:24.04

# Go: the Mac's version (go.mod asks for 1.25 or newer). GOTOOLCHAIN=local, so go never downloads a
# different toolchain at run time; bump it here and rebuild. Hash from https://go.dev/dl/?mode=json.
ARG GO_VERSION=1.27.2
ARG GO_SHA256=94f3e30b8e374bc285e7dadc11e0865726b9bc6e85b841ccceaabc0214c6b7c8
ARG USERNAME=mdagosta
ARG USER_UID=1000
ARG USER_GID=1000

# PATH starts with directories the dev user can write, so the root scripts (entrypoint, firewall) set
# their own. The venv comes first, so python3 is the one with torch (waldo's tests find it by name).
ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    CLAUDE_CONFIG_DIR=/home/mdagosta/.claude \
    GOPATH=/home/mdagosta/go \
    GOCACHE=/home/mdagosta/go/build-cache \
    GOTOOLCHAIN=local \
    PATH=/home/mdagosta/.venv/bin:/home/mdagosta/go/bin:/home/mdagosta/.local/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# ripgrep for waldo's testing/docs.sh, shellcheck for waldo-fetchers' make test, python3-venv for the
# docs repo's make setup, python3-dev for torch.compile (it builds C++ against Python.h).
RUN apt-get update && apt-get install -y --no-install-recommends \
        ack build-essential ca-certificates curl emacs-nox git jq less openssh-server procps \
        python3 python3-dev python3-venv ripgrep shellcheck tig tmux unzip xz-utils \
    && rm -rf /var/lib/apt/lists/*

# Go, checked against its published SHA-256 (the arm64 build, since the image is native arm64).
RUN cd /tmp \
    && curl --proto '=https' --tlsv1.2 -fsSLO https://go.dev/dl/go${GO_VERSION}.linux-arm64.tar.gz \
    && echo "${GO_SHA256}  go${GO_VERSION}.linux-arm64.tar.gz" | sha256sum -c - \
    && tar -xzf go${GO_VERSION}.linux-arm64.tar.gz -C /usr/local \
    && rm go${GO_VERSION}.linux-arm64.tar.gz \
    && /usr/local/go/bin/go version

# Ubuntu's image ships a user "ubuntu" at uid 1000; replace it with ours. No sudo: system changes go
# through this file and a rebuild.
RUN userdel --remove ubuntu 2>/dev/null || true; \
    groupadd --gid ${USER_GID} ${USERNAME}; \
    useradd --uid ${USER_UID} --gid ${USER_GID} --create-home --shell /bin/bash ${USERNAME}; \
    mkdir -p /home/${USERNAME}/src/openwaldo /home/${USERNAME}/.claude /home/${USERNAME}/go /home/${USERNAME}/.waldo; \
    chown -R ${USERNAME}:${USERNAME} /home/${USERNAME}

# SSH for herdr and git (see sshd/waldo.conf). Host keys are made at build, so they change on rebuild;
# the Mac's ssh config entry skips host key checks, since the connection is docker exec, never the
# network. /etc/environment gives SSH sessions the same variables as `docker exec` shells.
COPY sshd/waldo.conf /etc/ssh/sshd_config.d/waldo.conf
RUN ssh-keygen -A; \
    mkdir -p /run/sshd /etc/ssh/authorized_keys; \
    printf 'LANG=%s\nCLAUDE_CONFIG_DIR=%s\nGOPATH=%s\nGOCACHE=%s\nGOTOOLCHAIN=%s\nPATH=%s\n' \
        "$LANG" "$CLAUDE_CONFIG_DIR" "$GOPATH" "$GOCACHE" "$GOTOOLCHAIN" "$PATH" > /etc/environment

# Outbound firewall (firewall.sh, run by the entrypoint as root at every start): iptables and ipset for the
# rules, dig to resolve the allowlist, aggregate to merge GitHub's ranges.
RUN apt-get update && apt-get install -y --no-install-recommends aggregate dnsutils ipset iptables \
    && rm -rf /var/lib/apt/lists/*
COPY firewall.sh /usr/local/bin/waldo-firewall
COPY firewall-allowlist /etc/waldo-dev/firewall-allowlist
RUN chmod 755 /usr/local/bin/waldo-firewall

COPY entrypoint.sh /usr/local/bin/waldo-entrypoint
RUN chmod 755 /usr/local/bin/waldo-entrypoint

USER ${USERNAME}
# CPU PyTorch, so waldo's Linux-only PyTorch lifecycle (testing/e2e/model-pytorch.sh) and the FP64
# conformance check run here; there's no GPU, so MLX, CUDA and TorchTitan tests skip. The wheel is the
# official CPU build, pinned by URL and SHA-256; numpy pinned. Both at least two weeks old when chosen.
# In the image, not a volume: a rebuild resets anything installed by hand.
ARG TORCH_WHEEL=https://download.pytorch.org/whl/cpu/torch-2.15.0%2Bcpu-cp312-cp312-manylinux_2_28_aarch64.whl
ARG TORCH_SHA256=75941d429aaf4450bcd364345d7677e2d54aad45b38fbfcb1c0552450a6e9c40
RUN python3 -m venv /home/${USERNAME}/.venv \
    && /home/${USERNAME}/.venv/bin/pip install --no-cache-dir "torch @ ${TORCH_WHEEL}#sha256=${TORCH_SHA256}" numpy==2.5.4 \
    && /home/${USERNAME}/.venv/bin/python3 -c 'import torch; assert torch.tensor([1.0]).sum().item() == 1.0; print("torch", torch.__version__)'
RUN curl -fsSL https://claude.ai/install.sh | bash
# herdr's server, so herdr on the Mac can attach to agents running here. --version fails the build if
# the installer put it somewhere off PATH.
RUN curl -fsSL https://herdr.dev/install.sh | sh && herdr --version
# herdr-core (`hc`), which the herdr app on the Mac runs on the machine it's attached to. Its installer
# verifies the download's SHA-256 and only writes ~/.local.
RUN curl -fsSL https://releases.herdr.dev/core/install.sh | sh && hc --version

# Claude Code managed settings: root-owned (COPY ignores USER), so Claude, running as mdagosta, can't edit
# them. Only what's specific to the container (auto-updater off: updates come by rebuild); the shared
# policy comes from the Mac's dotfiles at every start (entrypoint.sh). Last, so changing it rebuilds only
# this layer.
COPY claude/managed-settings.json /etc/claude-code/managed-settings.json

# The entrypoint starts as root only to put up the firewall and fix named-volume ownership, then drops
# to the user.
USER root
WORKDIR /home/mdagosta/src/openwaldo
ENTRYPOINT ["/usr/local/bin/waldo-entrypoint"]
