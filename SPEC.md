# statusline — specification

**Job** — Anyone running Claude Code sees one colored line of session state
(directory, model, effort, context, git branch, rate limits, diff size), rendered
by a single static binary that installs itself into `~/.claude`.

**Why** — Claude Code pipes a JSON payload to whatever `statusLine.command`
names and prints its first line verbatim. Without this there is no line; with a
shell script there is a fragile one that re-parses JSON on every keystroke and
edits `settings.json` by hand.

**Guardrails** — The waivers and the dependency justification in the README's
*Baseline deviations* are the project's named decisions; anything not listed
there follows the baseline. Two constraints the baseline does not impose:
`settings.json` belongs to the user — every read/write preserves what the tool
does not own, or refuses — and stdout stays exactly one line, because Claude
Code prints only the first.

**Done means** — `checklists/cli-tool.md` walked, and `make ci` green on the
commit being pushed.
