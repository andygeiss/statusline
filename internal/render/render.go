// Package render turns the JSON that Claude Code pipes to a statusline
// command into one colored terminal line.
package render

import (
	"fmt"
	"strings"
	"unicode/utf8"
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
		TotalCostUSD      float64 `json:"total_cost_usd"`
		TotalLinesAdded   int     `json:"total_lines_added"`
		TotalLinesRemoved int     `json:"total_lines_removed"`
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
// rune count — ANSI codes make len(out) useless for fitting.
type segment struct {
	out      string
	width    int
	priority int // 0 is always kept; higher numbers drop first when narrow
}

// seg builds the common single-color segment.
func seg(plain, color string, priority int) segment {
	return segment{
		out:      color + plain + reset,
		width:    utf8.RuneCountInString(plain),
		priority: priority,
	}
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
	if in.Cost.TotalCostUSD > 0 {
		segs = append(segs, seg(fmt.Sprintf("$%.2f", in.Cost.TotalCostUSD), green, 4))
	}
	if s := linesSegment(in.Cost.TotalLinesAdded, in.Cost.TotalLinesRemoved); s != nil {
		segs = append(segs, *s)
	}
	if in.Agent.Name != "" {
		segs = append(segs, seg(in.Agent.Name, dim, 6))
	}
	if s := in.OutputStyle.Name; s != "" && s != "default" {
		segs = append(segs, seg(s, dim, 6))
	}

	segs = fit(segs, opts.Width)

	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = s.out
	}
	return strings.Join(parts, dim+" │ "+reset)
}

// shortDir abbreviates home to ~ and long paths to …/parent/base, keeping
// the line stable in deeply nested directories.
func shortDir(dir, home string) string {
	if dir == "" {
		return ""
	}
	if home != "" {
		if dir == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(dir, home+"/"); ok {
			dir = "~/" + rest
		}
	}
	const maxRunes = 40
	if utf8.RuneCountInString(dir) <= maxRunes {
		return dir
	}
	parts := strings.Split(dir, "/")
	if len(parts) <= 2 {
		return dir
	}
	return "…/" + strings.Join(parts[len(parts)-2:], "/")
}

// contextSegment shows context-window usage as a five-block gauge plus the
// exact percentage, green → yellow → red as the window fills.
func contextSegment(pct float64) segment {
	pct = min(max(pct, 0), 100)
	filled := int(pct / 20)
	if pct > 0 && filled == 0 {
		filled = 1
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 5-filled)
	return seg(fmt.Sprintf("%s %.0f%%", bar, pct), thresholdColor(pct), 1)
}

// rateSegment shows the subscription windows, each colored by its own usage.
// Absent windows are skipped; both absent means no segment (API-key users).
func rateSegment(fiveHour, sevenDay *RateWindow) *segment {
	var out, plain []string
	add := func(label string, w *RateWindow) {
		if w == nil {
			return
		}
		text := fmt.Sprintf("%s %.0f%%", label, w.UsedPercentage)
		out = append(out, thresholdColor(w.UsedPercentage)+text+reset)
		plain = append(plain, text)
	}
	add("5h", fiveHour)
	add("7d", sevenDay)
	if len(out) == 0 {
		return nil
	}
	return &segment{
		out:      strings.Join(out, " "),
		width:    utf8.RuneCountInString(strings.Join(plain, " ")),
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
		width:    utf8.RuneCountInString(plain),
		priority: 5,
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
