# statusline

A status line for [Claude Code](https://code.claude.com/docs/en/statusline.md):
one static Go binary that turns the JSON Claude Code pipes to it into one
colored line. Built for my own `~/.claude`; anyone with Go can use it.

```sh
GOBIN="$HOME/.claude" go install github.com/andygeiss/statusline@latest
~/.claude/statusline install    # registers it in ~/.claude/settings.json
```

From a checkout, `make install` does both, and `make uninstall` removes the
entry and the binary again.

Restart Claude Code (or open a new session) and the line appears:

```
~/workspace/statusline │ Fable │ high │ ██░░░ 42% │ ⎇ main │ 5h 24% 7d 41% │ +156 -23
```

## What the line shows

Left to right; a segment whose data is absent is skipped.

| Segment | Source | Color |
|---|---|---|
| Directory (`~`-abbreviated, long paths shortened) | `workspace.current_dir` | blue |
| Model | `model.display_name` | magenta |
| Reasoning effort (`low`…`max`; only when the model has the parameter) | `effort.level` | dim magenta |
| Context-window gauge + rounded percentage | `context_window.used_percentage` | green → yellow (60%) → red (85%) |
| Git branch, or short hash on a detached HEAD | read from `.git/HEAD` directly — no `git` subprocess, fast in any repo size | cyan |
| Subscription rate limits (Pro/Max only) | `rate_limits` | per-window threshold colors |
| Lines added / removed | `cost.total_lines_*` | green / red |
| Agent name (subagents only) | `agent.name` | dim |
| Output style (only when not `default`) | `output_style.name` | dim |

When the terminal is narrow (`COLUMNS`), the line drops its rightmost
low-priority segments first; the context gauge drops last, and directory and
model always survive.

## How install works

`make install` builds a release-shaped binary (`CGO_ENABLED=0 -trimpath`)
into `~/.claude/statusline`, then runs `statusline install`, which sets in
`~/.claude/settings.json`:

```json
{
  "statusLine": {
    "type": "command",
    "command": "~/.claude/statusline"
  }
}
```

`statusline install` registers only a binary that already sits in
`~/.claude`: one anywhere else would leave Claude Code with a command that
breaks once the file moves. That is why the `go install` line sets `GOBIN` —
it puts the binary where `make install` does.

The binary edits the JSON itself — every other setting survives, the write is
atomic (temp file + rename), and a corrupt file is an error, never data loss.
If `settings.json` already has a *different* statusLine, install refuses, and
`install -force` replaces it. `statusline uninstall` removes the entry
only when it points at this binary and leaves anything else alone;
`make uninstall` deletes the binary too.

Without a Go toolchain, build the binary by hand at the tagged commit (the
baseline's `operations/cli-release.md`), copy it to `~/.claude/statusline`
(Windows: `%USERPROFILE%\.claude\statusline.exe`), and register it with
`~/.claude/statusline install`.

`-claude-dir` (env `STATUSLINE_CLAUDE_DIR`) targets another config dir; see
`go run . -h`.

## Development

```sh
make check    # every gate, against the working tree — before every commit
make ci       # the same gates, against the commit — before every push
make test     # the inner loop
```

Built against the [engineering baseline](https://github.com/andygeiss/baseline)
(`project-types/cli-tool.md`).

## Baseline deviations

Two waived rules and one justification. Everything else follows the baseline.

- **No colors, spinners, or ANSI control sequences** (`patterns/go-cli.md`) —
  waived 2026-08-12 by Andy. The consumer is Claude Code's terminal UI, which
  renders ANSI, so here color *is* the data contract. Scoped to the render
  mode's single line, which stays one parseable line on stdout; the `install`
  and `uninstall` commands print plain text.
- **No `install`/`deploy` targets in the Makefile** (`stack/makefile.md`
  *What NOT to add*) — waived 2026-08-12 by Andy. Installing *is* this tool's
  job, and the two halves — build the binary, register it — have to agree on
  one directory. Scoped to `install` and `uninstall`, which shell out to the
  binary for the `settings.json` edit and touch nothing outside
  `$STATUSLINE_CLAUDE_DIR`.

Conformance, not a waiver:

- **Dependency `golang.org/x/text`** — outside the approved list in
  `stack/go.md`, which asks for exactly this: a written justification in the
  README. Fitting the line to the terminal needs each rune's East-Asian-width
  class, and hand-rolled codepoint ranges misclassify real characters.
  `x/text/width` is the authoritative table, maintained by the Go team. Used
  in one function, `internal/render.isWide`.
