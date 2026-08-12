package render

import (
	"encoding/json"
	"strings"
	"testing"
)

func input(t *testing.T, raw string) Input {
	t.Helper()
	var in Input
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatalf("test payload: %v", err)
	}
	return in
}

const fullPayload = `{
	"model": {"id": "claude-fable-5", "display_name": "Fable"},
	"workspace": {"current_dir": "/home/andy/workspace/statusline"},
	"output_style": {"name": "default"},
	"cost": {"total_cost_usd": 0.1234, "total_lines_added": 156, "total_lines_removed": 23},
	"context_window": {"used_percentage": 42},
	"rate_limits": {"five_hour": {"used_percentage": 23.5}, "seven_day": {"used_percentage": 91.2}}
}`

func TestLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload string
		opts    Options
		want    []string
		absent  []string
	}{
		{
			"full payload shows every segment",
			fullPayload,
			Options{Branch: "main", Home: "/home/andy"},
			[]string{"~/workspace/statusline", "Fable", "⎇ main", "██░░░ 42%", "$0.12", "+156", "-23", "5h 24%", "7d 91%"},
			nil,
		},
		{
			"empty payload renders empty line",
			`{}`,
			Options{},
			nil,
			[]string{"$", "%", "⎇"},
		},
		{
			"null context percentage skips the gauge",
			`{"model": {"display_name": "Fable"}, "context_window": {"used_percentage": null}}`,
			Options{},
			[]string{"Fable"},
			[]string{"%"},
		},
		{
			"zero cost and untouched lines are skipped",
			`{"model": {"display_name": "Fable"}, "cost": {"total_cost_usd": 0}}`,
			Options{},
			[]string{"Fable"},
			[]string{"$", "+0"},
		},
		{
			"no rate limits for API-key users",
			`{"model": {"display_name": "Fable"}}`,
			Options{},
			[]string{"Fable"},
			[]string{"5h", "7d"},
		},
		{
			"agent and non-default output style appear",
			`{"model": {"display_name": "Fable"}, "agent": {"name": "reviewer"}, "output_style": {"name": "explanatory"}}`,
			Options{},
			[]string{"reviewer", "explanatory"},
			nil,
		},
		{
			"home directory itself renders as tilde",
			`{"workspace": {"current_dir": "/home/andy"}}`,
			Options{Home: "/home/andy"},
			[]string{"~"},
			nil,
		},
		{
			"detached branch shown verbatim",
			`{"workspace": {"current_dir": "/x"}}`,
			Options{Branch: "1a2b3c4"},
			[]string{"⎇ 1a2b3c4"},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Line(input(t, tt.payload), tt.opts)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("line %q missing %q", got, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("line %q unexpectedly contains %q", got, absent)
				}
			}
		})
	}
}

func TestLine_NarrowTerminalDropsOptionalSegments(t *testing.T) {
	t.Parallel()
	in := input(t, fullPayload)
	wide := Line(in, Options{Branch: "main", Home: "/home/andy", Width: 200})
	narrow := Line(in, Options{Branch: "main", Home: "/home/andy", Width: 45})
	if !strings.Contains(wide, "5h") {
		t.Fatalf("wide line %q missing rate limits", wide)
	}
	for _, dropped := range []string{"5h", "$0.12", "+156"} {
		if strings.Contains(narrow, dropped) {
			t.Errorf("narrow line %q still contains %q", narrow, dropped)
		}
	}
	// The identity segments survive any width.
	for _, kept := range []string{"statusline", "Fable"} {
		if !strings.Contains(narrow, kept) {
			t.Errorf("narrow line %q lost %q", narrow, kept)
		}
	}
}

func TestLine_ContextGaugeThresholds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		pct   float64
		color string
		bar   string
	}{
		{"low usage is green", 10, green, "█░░░░ 10%"},
		{"mid usage is yellow", 60, yellow, "███░░ 60%"},
		{"high usage is red", 92, red, "████░ 92%"},
		{"full window is red and full bar", 100, red, "█████ 100%"},
		{"zero usage is empty bar", 0, green, "░░░░░ 0%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := contextSegment(tt.pct)
			if !strings.Contains(got.out, tt.color+tt.bar) {
				t.Errorf("contextSegment(%v) = %q, want color %q and bar %q", tt.pct, got.out, tt.color, tt.bar)
			}
		})
	}
}

func TestShortDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		dir, home string
		want      string
	}{
		{"home prefix becomes tilde", "/home/andy/src", "/home/andy", "~/src"},
		{"home itself becomes tilde", "/home/andy", "/home/andy", "~"},
		{"sibling of home stays absolute", "/home/andyx/src", "/home/andy", "/home/andyx/src"},
		{"empty home disables abbreviation", "/home/andy/src", "", "/home/andy/src"},
		{
			"deep path keeps last two components",
			"/home/andy/very/long/nested/path/to/some/deep/project/dir", "",
			"…/project/dir",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shortDir(tt.dir, tt.home); got != tt.want {
				t.Errorf("shortDir(%q, %q) = %q, want %q", tt.dir, tt.home, got, tt.want)
			}
		})
	}
}

// FuzzLine feeds arbitrary bytes through the JSON decode + render path:
// whatever Claude Code sends, the statusline must never panic.
func FuzzLine(f *testing.F) {
	f.Add([]byte(fullPayload))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"context_window": {"used_percentage": -5}}`))
	f.Add([]byte(`{"context_window": {"used_percentage": 1e300}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var in Input
		if err := json.Unmarshal(data, &in); err != nil {
			t.Skip()
		}
		Line(in, Options{Branch: "main", Home: "/home/andy", Width: 60})
	})
}
