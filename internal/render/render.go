// Package render turns the JSON that Claude Code pipes to a statusline
// command into one colored terminal line.
package render

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/text/width"
)

// Input mirrors the statusline JSON contract
// (https://code.claude.com/docs/en/statusline.md). Only the fields the line
// shows are declared; unknown fields are ignored by encoding/json.
type Input struct {
	Model struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	OutputStyle struct {
		Name string `json:"name"`
	} `json:"output_style"`
	Agent struct {
		Name string `json:"name"`
	} `json:"agent"`
	Cost struct {
		TotalLinesAdded   int `json:"total_lines_added"`
		TotalLinesRemoved int `json:"total_lines_removed"`
	} `json:"cost"`
	// UsedPercentage is a pointer: the docs allow null early in a session
	// and right after /compact.
	ContextWindow struct {
		UsedPercentage *float64 `json:"used_percentage"`
	} `json:"context_window"`
	RateLimits struct {
		FiveHour *RateWindow `json:"five_hour"`
		SevenDay *RateWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

// RateWindow is one subscription rate-limit window (Pro/Max only).
type RateWindow struct {
	UsedPercentage float64 `json:"used_percentage"`
}

// Options carries the context Line needs beyond the JSON payload, so Line
// itself stays pure and table-testable.
type Options struct {
	Branch string // current git branch, "" when not in a repository
	Home   string // user home directory, for ~ abbreviation; "" disables it
	Width  int    // terminal columns; <= 0 means unlimited
}

const (
	reset   = "\x1b[0m"
	dim     = "\x1b[2m"
	bold    = "\x1b[1m"
	red     = "\x1b[31m"
	green   = "\x1b[32m"
	yellow  = "\x1b[33m"
	blue    = "\x1b[34m"
	magenta = "\x1b[35m"
	cyan    = "\x1b[36m"
)

// segment is one already-colored piece of the line. width is the visible
// terminal-column count — ANSI codes make len(out) useless for fitting.
type segment struct {
	out      string
	width    int
	priority int // 0 is always kept; higher numbers drop first when narrow
}

// seg builds the common single-color segment. plain carries external input
// (paths, model/branch/agent names): a control rune there would break the
// one-line stdout contract or inject ANSI codes, so each becomes a space.
func seg(plain, color string, priority int) segment {
	plain = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, plain)
	return segment{
		out:      color + plain + reset,
		width:    displayWidth(plain),
		priority: priority,
	}
}

// displayWidth reports the terminal columns s occupies. Combining marks and
// format controls take no cell (macOS sends NFD paths, whose accents arrive
// as separate runes); East Asian wide and fullwidth runes take two, per the
// Unicode table in x/text/width; everything else takes one. Cluster-level
// width (ZWJ emoji sequences) is out of scope — those overcount, which only
// makes the drop logic slightly conservative.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		case isWide(r):
			w += 2
		default:
			w++
		}
	}
	return w
}

func isWide(r rune) bool {
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return true
	}
	return false
}

// Line renders the status line. Segments whose data is absent are skipped;
// when the line exceeds opts.Width, optional segments drop highest-priority
// first until it fits.
func Line(in Input, opts Options) string {
	var segs []segment

	if dir := shortDir(in.Workspace.CurrentDir, opts.Home); dir != "" {
		segs = append(segs, seg(dir, bold+blue, 0))
	}
	if in.Model.DisplayName != "" {
		segs = append(segs, seg(in.Model.DisplayName, magenta, 0))
	}
	if p := in.ContextWindow.UsedPercentage; p != nil {
		segs = append(segs, contextSegment(*p))
	}
	if opts.Branch != "" {
		segs = append(segs, seg("⎇ "+opts.Branch, cyan, 2))
	}
	if s := rateSegment(in.RateLimits.FiveHour, in.RateLimits.SevenDay); s != nil {
		segs = append(segs, *s)
	}
	if s := linesSegment(in.Cost.TotalLinesAdded, in.Cost.TotalLinesRemoved); s != nil {
		segs = append(segs, *s)
	}
	if in.Agent.Name != "" {
		segs = append(segs, seg(in.Agent.Name, dim, 5))
	}
	if s := in.OutputStyle.Name; s != "" && s != "default" {
		segs = append(segs, seg(s, dim, 5))
	}

	segs = fit(segs, opts.Width)

	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = s.out
	}
	return strings.Join(parts, dim+" │ "+reset)
}

// shortDir abbreviates home to ~ and long paths to …/parent/base, keeping
// the line stable in deeply nested directories. Paths use the target OS's
// separator — the release ships Windows binaries too.
func shortDir(dir, home string) string {
	if dir == "" {
		return ""
	}
	sep := string(filepath.Separator)
	if home != "" {
		if dir == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(dir, home+sep); ok {
			dir = "~" + sep + rest
		}
	}
	const maxColumns = 40
	if displayWidth(dir) <= maxColumns {
		return dir
	}
	parts := strings.Split(dir, sep)
	if len(parts) <= 2 {
		return dir
	}
	return "…" + sep + strings.Join(parts[len(parts)-2:], sep)
}

// contextSegment shows context-window usage as a five-block gauge plus the
// rounded percentage, green → yellow → red as the window fills. Bar, number,
// and color all derive from the same rounded value so they can never
// disagree (99.6% must not read "100%" next to a non-full bar, and 84.6%
// must not read "85%" in anything but the 85-band color).
func contextSegment(pct float64) segment {
	pct = min(max(pct, 0), 100)
	shown := int(math.Round(pct))
	filled := shown / 20
	if pct > 0 && filled == 0 {
		filled = 1
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 5-filled)
	return seg(fmt.Sprintf("%s %d%%", bar, shown), thresholdColor(float64(shown)), 1)
}

// rateSegment shows the subscription windows, each colored by its own usage.
// Absent windows are skipped; both absent means no segment (API-key users).
func rateSegment(fiveHour, sevenDay *RateWindow) *segment {
	var out, plain []string
	add := func(label string, w *RateWindow) {
		if w == nil {
			return
		}
		// Round once and derive text and color from it, like contextSegment.
		shown := math.Round(w.UsedPercentage)
		text := fmt.Sprintf("%s %.0f%%", label, shown)
		out = append(out, thresholdColor(shown)+text+reset)
		plain = append(plain, text)
	}
	add("5h", fiveHour)
	add("7d", sevenDay)
	if len(out) == 0 {
		return nil
	}
	return &segment{
		out:      strings.Join(out, " "),
		width:    displayWidth(strings.Join(plain, " ")),
		priority: 3,
	}
}

// linesSegment shows the session's diff footprint, additions green and
// removals red; nil when the session has not touched a line yet.
func linesSegment(added, removed int) *segment {
	if added == 0 && removed == 0 {
		return nil
	}
	plain := fmt.Sprintf("+%d -%d", added, removed)
	return &segment{
		out:      fmt.Sprintf("%s+%d%s %s-%d%s", green, added, reset, red, removed, reset),
		width:    displayWidth(plain),
		priority: 4,
	}
}

func thresholdColor(pct float64) string {
	switch {
	case pct >= 85:
		return red
	case pct >= 60:
		return yellow
	default:
		return green
	}
}

// fit drops optional segments, highest priority number first and rightmost
// within a tie, until the visible line fits width. Priority-0 segments never
// drop — if they alone overflow, the terminal's own truncation wins.
func fit(segs []segment, width int) []segment {
	if width <= 0 {
		return segs
	}
	const sepWidth = 3 // " │ "
	for {
		total := 0
		for i, s := range segs {
			if i > 0 {
				total += sepWidth
			}
			total += s.width
		}
		if total <= width {
			return segs
		}
		drop, maxPriority := -1, 0
		for i, s := range segs {
			if s.priority >= maxPriority && s.priority > 0 {
				drop, maxPriority = i, s.priority
			}
		}
		if drop < 0 {
			return segs
		}
		segs = append(segs[:drop:drop], segs[drop+1:]...)
	}
}
