package render

import (
	json "encoding/json/v2"
	"path/filepath"
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

// fullPayload builds the everything-populated payload with an OS-native
// current_dir, so the tilde-abbreviation assertions hold on Windows too.
func fullPayload(tb testing.TB) string {
	tb.Helper()
	dir, err := json.Marshal(filepath.FromSlash("/home/andy/workspace/statusline"))
	if err != nil {
		tb.Fatal(err)
	}
	return `{
	"model": {"id": "claude-fable-5", "display_name": "Fable"},
	"workspace": {"current_dir": ` + string(dir) + `},
	"output_style": {"name": "default"},
	"effort": {"level": "high"},
	"cost": {"total_lines_added": 156, "total_lines_removed": 23},
	"context_window": {"used_percentage": 42},
	"rate_limits": {"five_hour": {"used_percentage": 23.5}, "seven_day": {"used_percentage": 91.2}}
}`
}

// testHome is the OS-native form of the payload's home directory.
func testHome() string { return filepath.FromSlash("/home/andy") }

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
			fullPayload(t),
			Options{Branch: "main", Home: testHome()},
			[]string{filepath.FromSlash("~/workspace/statusline"), "Fable", "high", "⎇ main", "██░░░ 42%", "+156", "-23", "5h 24%", "7d 91%"},
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
			"untouched lines are skipped",
			`{"model": {"display_name": "Fable"}, "cost": {}}`,
			Options{},
			[]string{"Fable"},
			[]string{"+0", "-0"},
		},
		{
			"no rate limits for API-key users",
			`{"model": {"display_name": "Fable"}}`,
			Options{},
			[]string{"Fable"},
			[]string{"5h", "7d"},
		},
		{
			"effort level appears next to the model",
			`{"model": {"display_name": "Fable"}, "effort": {"level": "xhigh"}}`,
			Options{},
			[]string{"Fable", "xhigh"},
			nil,
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
	in := input(t, fullPayload(t))
	wide := Line(in, Options{Branch: "main", Home: testHome(), Width: 200})
	narrow := Line(in, Options{Branch: "main", Home: testHome(), Width: 45})
	if !strings.Contains(wide, "5h") {
		t.Fatalf("wide line %q missing rate limits", wide)
	}
	for _, dropped := range []string{"5h", "+156"} {
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

// Models without a reasoning-effort parameter send no effort object; the
// line must then carry no empty segment and no separator for it.
func TestLine_EffortAbsentWhenModelHasNone(t *testing.T) {
	t.Parallel()
	got := Line(input(t, `{"model": {"display_name": "Fable"}, "effort": {}}`), Options{})
	if strings.Contains(got, "│") {
		t.Errorf("line %q has a separator, want the model segment alone", got)
	}
}

// A CJK directory occupies two columns per rune. Counting runes instead
// would judge this line as fitting (30 ≤ 35) and keep the agent segment;
// counting columns (39 > 35) must drop it.
func TestLine_WideRunesCountTwoColumns(t *testing.T) {
	t.Parallel()
	in := input(t, `{
		"workspace": {"current_dir": "/日本語/プロジェクト"},
		"model": {"display_name": "Fable"},
		"agent": {"name": "reviewer"}
	}`)
	got := Line(in, Options{Width: 35})
	if strings.Contains(got, "reviewer") {
		t.Errorf("line %q kept the agent segment despite wide runes overflowing 35 columns", got)
	}
	if !strings.Contains(got, "日本語") {
		t.Errorf("line %q lost the directory segment", got)
	}
}

// Control characters in external names must not reach stdout: a newline
// would break the one-line contract, an ESC would inject color across the
// following segments.
func TestLine_ControlCharactersAreNeutralized(t *testing.T) {
	t.Parallel()
	in := input(t, `{"model": {"display_name": "evil\u001b[31mRED\nname"}}`)
	got := Line(in, Options{})
	if strings.Contains(got, "\n") {
		t.Errorf("line %q contains a newline", got)
	}
	if strings.Contains(got, red) {
		t.Errorf("line %q contains injected ANSI red", got)
	}
	if !strings.Contains(got, "RED name") {
		t.Errorf("line %q should keep the printable text with controls as spaces", got)
	}
}

func TestDisplayWidth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		s    string
		want int
	}{
		{"ascii", "abc", 3},
		{"cjk takes two columns per rune", "日本語", 6},
		{"nfd combining mark takes no column", "U\u0308bungen", 7},
		{"emoji takes two columns", "\U0001F680", 2},
		{"ideographic half fill space is narrow", "〿", 1},
		{"gauge blocks and separator are narrow", "█░ │ ⎇ …", 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := displayWidth(tt.s); got != tt.want {
				t.Errorf("displayWidth(%q) = %d, want %d", tt.s, got, tt.want)
			}
		})
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
		{"bar agrees with the rounded percentage", 99.6, red, "█████ 100%"},
		{"barely used still shows one block", 0.4, green, "█░░░░ 0%"},
		{"color agrees with the rounded percentage at the red band", 84.6, red, "████░ 85%"},
		{"color agrees with the rounded percentage at the yellow band", 59.6, yellow, "███░░ 60%"},
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
			// The cases are written with slashes; shortDir works in the
			// target OS's separator, so convert both sides.
			got := shortDir(filepath.FromSlash(tt.dir), filepath.FromSlash(tt.home))
			if want := filepath.FromSlash(tt.want); got != want {
				t.Errorf("shortDir(%q, %q) = %q, want %q", tt.dir, tt.home, got, want)
			}
		})
	}
}

// FuzzLine feeds arbitrary bytes through the JSON decode + render path:
// whatever Claude Code sends, the statusline must never panic.
func FuzzLine(f *testing.F) {
	f.Add([]byte(fullPayload(f)))
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
