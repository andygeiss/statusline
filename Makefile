# Copied from the baseline (stack/makefile.md), rule-5 single-binary layout.
# Deviation, recorded in the README: install/uninstall wire the binary into
# the current user's Claude Code — repo tooling in the same spirit as the
# baseline repository's own Makefile.

# The main package: . for a single-binary CLI.
MAIN = .

.PHONY: check test run fmt build clean install uninstall

# Default. Every gate CI runs, identically and in the same order
# (operations/ci.md). Green here means green CI — run before every push.
check:
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	go mod tidy -diff
	go test -race -shuffle=on ./...
	CGO_ENABLED=0 go build -trimpath ./...

# The inner loop.
test:
	go test -race -shuffle=on ./...

run:
	go run $(MAIN)

fmt:
	go run golang.org/x/tools/cmd/goimports@latest -w .

# Release-shaped local binary in bin/ (go build creates the directory).
build:
	CGO_ENABLED=0 go build -trimpath -o bin/ $(MAIN)

clean:
	rm -rf bin/ statusline

# Where the binary lands. Honors the same env var as the binary's own
# -claude-dir default, so both halves of install always agree on the dir.
STATUSLINE_CLAUDE_DIR ?= $(HOME)/.claude

# Build a release-shaped binary into the Claude dir and register it in
# settings.json. The binary does the JSON editing — Make and shell cannot
# edit JSON safely.
install:
	mkdir -p "$(STATUSLINE_CLAUDE_DIR)"
	CGO_ENABLED=0 go build -trimpath -o "$(STATUSLINE_CLAUDE_DIR)/statusline" $(MAIN)
	"$(STATUSLINE_CLAUDE_DIR)/statusline" install

# go run, not the installed binary: uninstall must work even after the
# binary is gone.
uninstall:
	go run $(MAIN) uninstall
	rm -f "$(STATUSLINE_CLAUDE_DIR)/statusline"
