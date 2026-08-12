# statusline

A status line for [Claude Code](https://code.claude.com/docs/en/statusline.md):
one static Go binary that turns the JSON Claude Code pipes to it into one
colored line. Built for my own `~/.claude`; anyone with Go and Make can use it.

```sh
make install    # builds ~/.claude/statusline and registers it in settings.json
make uninstall  # removes the entry and the binary
```

Restart Claude Code (or open a new session) and the line appears:

```
~/workspace/statusline │ Fable │ ██░░░ 42% │ ⎇ main │ 5h 24% 7d 41% │ +156 -23
```

## What the line shows

Left to right; a segment whose data is absent is skipped.

| Segment | Source | Color |
|---|---|---|
| Directory (`~`-abbreviated, long paths shortened) | `workspace.current_dir` | blue |
| Model | `model.display_name` | magenta |
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

The binary edits the JSON itself — every other setting survives, the write is
atomic (temp file + rename), and a corrupt file is an error, never data loss.
If `settings.json` already has a *different* statusLine, install refuses;
rerun with `go run . install -force` to replace it. `make uninstall` removes
the entry only when it points at this binary and leaves anything else alone.

`-claude-dir` (env `STATUSLINE_CLAUDE_DIR`) targets another config dir; see
`go run . -h`.

## Development

```sh
make check    # every CI gate, in CI's order
make test     # the inner loop
```

Built against the [engineering baseline](https://github.com/andygeiss/baseline)
(`project-types/cli-tool.md`). Deviations, per its rules:

- **ANSI colors on stdout** (waives the baseline's no-ANSI rule): the
  consumer is Claude Code's terminal UI, which renders ANSI — here color *is*
  the data contract, and stdout stays one parseable line.
- **Makefile `install`/`uninstall` targets** (normally banned): repo tooling
  that wires the tool into the current user's Claude Code, in the same spirit
  as the baseline repository's own Makefile.
- **Private repository, so no `go install` line**: the module proxy cannot
  serve it. Install from a checkout with `make install`, or take a binary
  from a GitHub release (built by `operations/cli-release.md`'s workflow),
  copy it to `~/.claude/statusline` (Windows: `%USERPROFILE%\.claude\statusline.exe`),
  and run `~/.claude/statusline install` — `install` refuses to register a
  binary that is not in place.
- **Dependency `golang.org/x/text`** (outside the baseline's approved list):
  fitting the line to the terminal needs each rune's East-Asian-width class,
  and hand-rolled codepoint ranges misclassify real characters. `x/text/width`
  is the authoritative table, maintained by the Go team.
