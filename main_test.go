package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andygeiss/statusline/internal/settings"
)

func TestRun_RenderHappyPath(t *testing.T) {
	t.Setenv("COLUMNS", "")               // a small exported COLUMNS would drop segments and fail the assertions
	dir, err := json.Marshal(t.TempDir()) // Marshal, not raw splice: Windows paths contain JSON escapes
	if err != nil {
		t.Fatal(err)
	}
	payload := `{
		"model": {"display_name": "Fable"},
		"workspace": {"current_dir": ` + string(dir) + `},
		"context_window": {"used_percentage": 8}
	}`
	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), nil, strings.NewReader(payload), &stdout, &stderr); err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}
	line := stdout.String()
	if !strings.Contains(line, "Fable") || !strings.Contains(line, "8%") {
		t.Errorf("line %q missing model or context", line)
	}
	if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Errorf("stdout must be exactly one line, got %q", line)
	}
}

// The render mode blocks on stdin; the first Ctrl-C/SIGTERM cancels the
// context and must end the run (non-zero), not be swallowed.
func TestRun_RenderInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	blocked, _ := io.Pipe() // a reader that never delivers, like a terminal without input
	var stdout, stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- run(ctx, nil, blocked, &stdout, &stderr) }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("run = %v, want context.Canceled", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay clean on interrupt, got %q", stdout.String())
	}
}

func TestRun_RenderRejectsBadJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), nil, strings.NewReader("not json"), &stdout, &stderr)
	if err == nil {
		t.Fatal("run accepted invalid stdin")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay clean on failure, got %q", stdout.String())
	}
}

func TestRun_InstallUninstallRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Install refuses to register a command that doesn't exist.
	if err := os.WriteFile(settings.BinaryPath(dir), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	err := run(t.Context(), []string{"install", "-claude-dir", dir}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("install: %v (stderr: %s)", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("install wrote to stdout: %q", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json missing after install: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("settings.json invalid: %v", err)
	}
	if _, ok := cfg["statusLine"]; !ok {
		t.Fatalf("statusLine missing: %s", data)
	}

	err = run(t.Context(), []string{"uninstall", "-claude-dir", dir}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("uninstall: %v (stderr: %s)", err, stderr.String())
	}
	data, _ = os.ReadFile(filepath.Join(dir, "settings.json"))
	if strings.Contains(string(data), "statusLine") {
		t.Errorf("statusLine still present after uninstall: %s", data)
	}
}

func TestRun_ClaudeDirEnvDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLINE_CLAUDE_DIR", dir)
	if err := os.WriteFile(settings.BinaryPath(dir), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), []string{"install"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("install: %v (stderr: %s)", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		t.Errorf("env-configured dir not used: %v", err)
	}
}

func TestRun_Version(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), []string{"version"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("version: %v", err)
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Error("version printed nothing")
	}
}

func TestRun_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), []string{"-h"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("-h: %v", err)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("usage not printed: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("usage leaked to stdout: %q", stdout.String())
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"bogus"}, strings.NewReader(""), &stdout, &stderr)
	if !errors.Is(err, errUsage) {
		t.Fatalf("run = %v, want errUsage", err)
	}
}

// A positional argument on install/uninstall is a usage error: silently
// operating on the default directory instead would edit the wrong file.
func TestRun_UnexpectedArgument(t *testing.T) {
	t.Setenv("STATUSLINE_CLAUDE_DIR", t.TempDir()) // hermetic: never near a real ~/.claude
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"install", dir}, strings.NewReader(""), &stdout, &stderr)
	if !errors.Is(err, errUsage) {
		t.Fatalf("run = %v, want errUsage", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil {
		t.Error("install with a positional argument still wrote settings.json")
	}
}

func TestRun_BadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"install", "-bogus"}, strings.NewReader(""), &stdout, &stderr)
	if !errors.Is(err, errUsage) {
		t.Fatalf("run = %v, want errUsage", err)
	}
}
