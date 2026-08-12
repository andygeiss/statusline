package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readConfig(t *testing.T, claudeDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatalf("reading settings.json: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}
	return cfg
}

func statusLineCommand(t *testing.T, cfg map[string]any) string {
	t.Helper()
	entry, ok := cfg["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("statusLine entry missing or wrong shape: %v", cfg["statusLine"])
	}
	if entry["type"] != "command" {
		t.Fatalf("statusLine type = %v, want command", entry["type"])
	}
	command, _ := entry["command"].(string)
	return command
}

func TestInstall(t *testing.T) {
	t.Parallel()

	t.Run("creates settings.json when absent", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		got := statusLineCommand(t, readConfig(t, dir))
		if want := filepath.Join(dir, "statusline"); got != want {
			t.Errorf("command = %q, want %q", got, want)
		}
	})

	t.Run("preserves unrelated settings", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		existing := `{"model": "opus", "permissions": {"allow": ["Bash(ls:*)"]}}`
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		cfg := readConfig(t, dir)
		if cfg["model"] != "opus" {
			t.Errorf("model = %v, want opus", cfg["model"])
		}
		permissions, ok := cfg["permissions"].(map[string]any)
		if !ok || permissions["allow"] == nil {
			t.Errorf("permissions lost: %v", cfg["permissions"])
		}
	})

	t.Run("rerun is idempotent", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := Install(dir, false); err != nil {
			t.Fatalf("first Install: %v", err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("second Install: %v", err)
		}
	})

	t.Run("refuses a foreign statusLine without force", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		existing := `{"statusLine": {"type": "command", "command": "/somewhere/else.sh"}}`
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}
		err := Install(dir, false)
		if err == nil || !strings.Contains(err.Error(), "-force") {
			t.Fatalf("Install = %v, want refusal mentioning -force", err)
		}
		if got := statusLineCommand(t, readConfig(t, dir)); got != "/somewhere/else.sh" {
			t.Errorf("foreign entry was modified: %q", got)
		}
	})

	t.Run("force replaces a foreign statusLine", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		existing := `{"statusLine": {"type": "command", "command": "/somewhere/else.sh"}}`
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, true); err != nil {
			t.Fatalf("Install -force: %v", err)
		}
		if got, want := statusLineCommand(t, readConfig(t, dir)), filepath.Join(dir, "statusline"); got != want {
			t.Errorf("command = %q, want %q", got, want)
		}
	})

	t.Run("preserves a restrictive file mode", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		fi, err := os.Stat(filepath.Join(dir, "settings.json"))
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != 0o600 {
			t.Errorf("mode = %v, want 0600", got)
		}
	})

	t.Run("invalid settings.json is an error, not data loss", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err == nil {
			t.Fatal("Install accepted corrupt settings.json")
		}
		data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
		if err != nil || string(data) != "{not json" {
			t.Errorf("corrupt file was rewritten: %q, %v", data, err)
		}
	})
}

func TestUninstall(t *testing.T) {
	t.Parallel()

	t.Run("removes our entry and keeps the rest", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"model": "opus"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		removed, err := Uninstall(dir)
		if err != nil || !removed {
			t.Fatalf("Uninstall = %v, %v; want true, nil", removed, err)
		}
		cfg := readConfig(t, dir)
		if _, ok := cfg["statusLine"]; ok {
			t.Errorf("statusLine still present: %v", cfg)
		}
		if cfg["model"] != "opus" {
			t.Errorf("model = %v, want opus", cfg["model"])
		}
	})

	t.Run("leaves a foreign entry alone", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		existing := `{"statusLine": {"type": "command", "command": "/somewhere/else.sh"}}`
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}
		removed, err := Uninstall(dir)
		if err != nil || removed {
			t.Fatalf("Uninstall = %v, %v; want false, nil", removed, err)
		}
		if got := statusLineCommand(t, readConfig(t, dir)); got != "/somewhere/else.sh" {
			t.Errorf("foreign entry was modified: %q", got)
		}
	})

	t.Run("missing settings.json is a clean no-op", func(t *testing.T) {
		t.Parallel()
		removed, err := Uninstall(t.TempDir())
		if err != nil || removed {
			t.Fatalf("Uninstall = %v, %v; want false, nil", removed, err)
		}
	})
}

// Command's tilde form matters: the same settings.json keeps working if the
// home directory moves, and it reads cleanly in the settings file.
func TestCommand_TildeForDefaultDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, want := Command(filepath.Join(home, ".claude")), "~/.claude/statusline"; got != want {
		t.Errorf("Command = %q, want %q", got, want)
	}
	other := t.TempDir()
	if got, want := Command(other), filepath.Join(other, "statusline"); got != want {
		t.Errorf("Command = %q, want %q", got, want)
	}
}

// Uninstall must recognize the tilde form Install writes for the default
// directory — that is the exact shape a real ~/.claude install produces.
func TestUninstall_MatchesTildeForm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	if err := Install(claudeDir, false); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := statusLineCommand(t, readConfig(t, claudeDir)); got != "~/.claude/statusline" {
		t.Fatalf("command = %q, want tilde form", got)
	}
	removed, err := Uninstall(claudeDir)
	if err != nil || !removed {
		t.Fatalf("Uninstall = %v, %v; want true, nil", removed, err)
	}
}
