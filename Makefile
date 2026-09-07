# Copied from the baseline (stack/makefile.md), rule-5 single-binary layout.
# Deviation, recorded in the README: install/uninstall wire the binary into
# the current user's Claude Code — repo tooling in the same spirit as the
# baseline repository's own Makefile.

# The main package: . for a single-binary CLI.
MAIN = .

# Where install puts the binary. Honors the same env var as the binary's own
# -claude-dir default, so both halves of install always agree on the dir.
STATUSLINE_CLAUDE_DIR ?= $(HOME)/.claude

# Targets are alphabetical, so the default is named rather than first.
.DEFAULT_GOAL = check
.PHONY: build check ci clean fmt install run test uninstall

# Release-shaped local binary in bin/ (go build creates the directory).
build:
	CGO_ENABLED=0 go build -trimpath -o bin/ $(MAIN)

# Default. Every gate, in this order (operations/ci.md), against the working
# tree. Run before every commit.
check:
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
	go fix -diff ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	go mod tidy -diff
	go test -race -shuffle=on ./...
	CGO_ENABLED=0 go build -trimpath ./...

# The same gates against the commit: a file never added, or a .env, cannot
# make it green. Run before every push. go version runs first, inside the
# copy, so the run records which toolchain ran. The archive goes through a
# file so git's exit status stops the run; one shell line so the trap cleans
# up however check ends.
ci:
	t=$$(mktemp); d=$$(mktemp -d); trap 'rm -rf "$$t" "$$d"' EXIT; git archive -o "$$t" HEAD && tar -xf "$$t" -C "$$d" && go -C "$$d" version && $(MAKE) -C "$$d" check

clean:
	rm -rf bin/ statusline

# goimports first: go fix type-checks, so a missing import would stop the
# recipe before goimports could add it. go fix manages the imports its own
# rewrites need.
fmt:
	go run golang.org/x/tools/cmd/goimports@latest -w .
	go fix ./...

# Build a release-shaped binary into the Claude dir and register it in
# settings.json. The binary does the JSON editing — Make and shell cannot
# edit JSON safely.
install:
	mkdir -p "$(STATUSLINE_CLAUDE_DIR)"
	CGO_ENABLED=0 go build -trimpath -o "$(STATUSLINE_CLAUDE_DIR)/statusline" $(MAIN)
	"$(STATUSLINE_CLAUDE_DIR)/statusline" install

# Loads .env when it is there, so a local start is one command. Only run:
# check and test MUST NOT depend on a developer's machine (rule 6). One shell
# line, because each recipe line gets its own shell.
run:
	set -a; if [ -f .env ]; then . ./.env; fi; set +a; go run $(MAIN)

# The inner loop.
test:
	go test -race -shuffle=on ./...

# go run, not the installed binary: uninstall must work even after the
# binary is gone.
uninstall:
	go run $(MAIN) uninstall
	rm -f "$(STATUSLINE_CLAUDE_DIR)/statusline"
