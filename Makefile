# Personal targets, on md/waldito only (not for upstream). The project's own checks are testing/*.sh.
.PHONY: list check fmt

list: ## Show targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*## /\t/'

check: ## gofmt check, then the full local suite (AGENTS.md's handoff check; hardware tests skip)
	test -z "$$(gofmt -l .)"
	./testing/all.sh

fmt: ## gofmt -w .
	gofmt -w .

# Dev container (on the Mac host; dev.sh refuses inside). See .devcontainer/README.md.
# First time: dev-ssh-config, dev-up. After that: dev-up, dev-shell or ssh waldo-dev.
.PHONY: dev-build dev-up dev-push dev-shell dev-down dev-restart dev-ssh-config

dev-build: ## Build the dev container image only
	.devcontainer/dev.sh build

dev-up: ## Build if needed, start the dev container, seed any openwaldo repo it lacks, run setup
	.devcontainer/dev.sh up

dev-push: ## Push a branch into the container: REPO=name (e.g. waldo, docs), BRANCH=name (default: current)
	.devcontainer/dev.sh push $(REPO) $(BRANCH)

dev-shell: ## Open a shell in the dev container
	.devcontainer/dev.sh shell

dev-down: ## Stop the dev container (data is kept)
	.devcontainer/dev.sh down

dev-restart: ## Restart the dev container: firewall and Claude policy re-read, no rebuild (ends its shells)
	.devcontainer/dev.sh restart

dev-ssh-config: ## Add "Host waldo-dev" to ~/.ssh/config for git and herdr (once)
	.devcontainer/dev.sh ssh-config
