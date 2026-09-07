package settings

import (
	json "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setHome points os.UserHomeDir at a temp dir — via HOME on Unix,
// USERPROFILE on Windows.
func setHome(t *testing.T, home string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
		t.Setenv("HOME", home) // Git Bash sessions export HOME; keep the two agreeing
	} else {
		t.Setenv("HOME", home)
	}
}

// claudeDirWithBinary returns a temp Claude dir holding a stub binary —
// Install refuses to register a command that doesn't exist.
func claudeDirWithBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(BinaryPath(dir), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

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

	t.Run("refuses to register without the binary in place", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir() // deliberately no stub binary
		err := Install(dir, false)
		if err == nil || !strings.Contains(err.Error(), "no statusline binary") {
			t.Fatalf("Install = %v, want missing-binary refusal", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil {
			t.Error("settings.json was created despite the refusal")
		}
	})

	t.Run("creates settings.json when absent", func(t *testing.T) {
		t.Parallel()
		dir := claudeDirWithBinary(t)
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		got := statusLineCommand(t, readConfig(t, dir))
		if want := filepath.ToSlash(BinaryPath(dir)); got != want {
			t.Errorf("command = %q, want %q", got, want)
		}
	})

	t.Run("preserves 64-bit integers exactly", func(t *testing.T) {
		t.Parallel()
		dir := claudeDirWithBinary(t)
		existing := `{"someId": 1754000000123456789}`
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "1754000000123456789") {
			t.Errorf("64-bit integer corrupted: %s", data)
		}
	})

	t.Run("preserves unrelated settings", func(t *testing.T) {
		t.Parallel()
		dir := claudeDirWithBinary(t)
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
		dir := claudeDirWithBinary(t)
		if err := Install(dir, false); err != nil {
			t.Fatalf("first Install: %v", err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("second Install: %v", err)
		}
	})

	t.Run("rerun keeps user-added keys in our entry", func(t *testing.T) {
		t.Parallel()
		dir := claudeDirWithBinary(t)
		existing := fmt.Sprintf(`{"statusLine": {"type": "command", "command": %q, "padding": 0}}`,
			BinaryPath(dir))
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		entry, _ := readConfig(t, dir)["statusLine"].(map[string]any)
		if padding, ok := entry["padding"].(float64); !ok || padding != 0 {
			t.Errorf("padding lost on rerun: %v", entry)
		}
	})

	t.Run("refuses a foreign statusLine without force", func(t *testing.T) {
		t.Parallel()
		dir := claudeDirWithBinary(t)
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
		dir := claudeDirWithBinary(t)
		existing := `{"statusLine": {"type": "command", "command": "/somewhere/else.sh"}}`
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, true); err != nil {
			t.Fatalf("Install -force: %v", err)
		}
		if got, want := statusLineCommand(t, readConfig(t, dir)), filepath.ToSlash(BinaryPath(dir)); got != want {
			t.Errorf("command = %q, want %q", got, want)
		}
	})

	t.Run("writes through a symlinked settings.json", func(t *testing.T) {
		t.Parallel()
		source := filepath.Join(t.TempDir(), "dotfiles-settings.json")
		if err := os.WriteFile(source, []byte(`{"model": "opus"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		dir := claudeDirWithBinary(t)
		link := filepath.Join(dir, "settings.json")
		if err := os.Symlink(source, link); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("settings.json is no longer a symlink: %v, %v", fi, err)
		}
		data, err := os.ReadFile(source)
		if err != nil || !strings.Contains(string(data), "statusLine") {
			t.Errorf("symlink target not updated: %q, %v", data, err)
		}
	})

	t.Run("writes through a dangling symlink instead of severing it", func(t *testing.T) {
		t.Parallel()
		source := filepath.Join(t.TempDir(), "not-yet-restored", "settings.json")
		dir := claudeDirWithBinary(t)
		link := filepath.Join(dir, "settings.json")
		if err := os.Symlink(source, link); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("settings.json is no longer a symlink: %v, %v", fi, err)
		}
		data, err := os.ReadFile(source)
		if err != nil || !strings.Contains(string(data), "statusLine") {
			t.Errorf("symlink target not created: %q, %v", data, err)
		}
	})

	t.Run("preserves a restrictive file mode", func(t *testing.T) {
		t.Parallel()
		dir := claudeDirWithBinary(t)
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

	t.Run("a dir containing a space registers a quoted command", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "dir with space")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(BinaryPath(dir), nil, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		got := statusLineCommand(t, readConfig(t, dir))
		if want := "'" + filepath.ToSlash(BinaryPath(dir)) + "'"; got != want {
			t.Errorf("command = %q, want %q", got, want)
		}
		// The quoted form must round-trip: rerun and uninstall recognize it.
		if err := Install(dir, false); err != nil {
			t.Fatalf("rerun Install: %v", err)
		}
		removed, err := Uninstall(dir)
		if err != nil || !removed {
			t.Fatalf("Uninstall = %v, %v; want true, nil", removed, err)
		}
	})

	t.Run("shell metacharacters in the dir are quoted inertly", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "my $tuff")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(BinaryPath(dir), nil, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		got := statusLineCommand(t, readConfig(t, dir))
		if want := "'" + filepath.ToSlash(BinaryPath(dir)) + "'"; got != want {
			t.Errorf("command = %q, want %q", got, want)
		}
		removed, err := Uninstall(dir)
		if err != nil || !removed {
			t.Fatalf("Uninstall = %v, %v; want true, nil", removed, err)
		}
	})

	t.Run("a trailing slash on the dir still matches our own entry", func(t *testing.T) {
		t.Parallel()
		dir := claudeDirWithBinary(t)
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		if err := Install(dir+string(filepath.Separator), false); err != nil {
			t.Fatalf("Install with trailing slash refused its own entry: %v", err)
		}
		removed, err := Uninstall(dir + string(filepath.Separator))
		if err != nil || !removed {
			t.Fatalf("Uninstall with trailing slash = %v, %v; want true, nil", removed, err)
		}
	})

	t.Run("an empty settings.json is an empty configuration", func(t *testing.T) {
		t.Parallel()
		dir := claudeDirWithBinary(t)
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		if got, want := statusLineCommand(t, readConfig(t, dir)), filepath.ToSlash(BinaryPath(dir)); got != want {
			t.Errorf("command = %q, want %q", got, want)
		}
	})

	t.Run("a settings.json holding null is an empty configuration", func(t *testing.T) {
		t.Parallel()
		dir := claudeDirWithBinary(t)
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("null\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err != nil {
			t.Fatalf("Install: %v", err)
		}
		if got, want := statusLineCommand(t, readConfig(t, dir)), filepath.ToSlash(BinaryPath(dir)); got != want {
			t.Errorf("command = %q, want %q", got, want)
		}
	})

	t.Run("data after the top-level value is an error, not data loss", func(t *testing.T) {
		t.Parallel()
		dir := claudeDirWithBinary(t)
		existing := `{"model": "opus"} {"stray": true}`
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Install(dir, false); err == nil {
			t.Fatal("Install accepted settings.json with trailing data")
		}
		data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
		if err != nil || string(data) != existing {
			t.Errorf("file with trailing data was rewritten: %q, %v", data, err)
		}
	})

	t.Run("invalid settings.json is an error, not data loss", func(t *testing.T) {
		t.Parallel()
		dir := claudeDirWithBinary(t)
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
		dir := claudeDirWithBinary(t)
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
		dir := claudeDirWithBinary(t)
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

// A relative dir must register an absolute command — Claude Code executes
// it from its own working directory. Top-level test: t.Chdir cannot run
// under a parallel parent.
func TestInstall_RelativeDirRegistersAbsoluteCommand(t *testing.T) {
	dir := claudeDirWithBinary(t)
	t.Chdir(filepath.Dir(dir))
	if err := Install(filepath.Base(dir), false); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got := statusLineCommand(t, readConfig(t, dir))
	if !filepath.IsAbs(filepath.FromSlash(got)) {
		t.Errorf("command = %q, want an absolute path", got)
	}
}

// Command's tilde form matters: the same settings.json keeps working if the
// home directory moves, and it reads cleanly in the settings file.
func TestCommand_TildeForDefaultDir(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	want := "~/.claude/" + binaryName()
	if got := Command(filepath.Join(home, ".claude")); got != want {
		t.Errorf("Command = %q, want %q", got, want)
	}
	// Custom dirs get forward slashes: Git Bash strips unquoted backslashes.
	other := t.TempDir()
	if got, want := Command(other), filepath.ToSlash(BinaryPath(other)); got != want {
		t.Errorf("Command = %q, want %q", got, want)
	}
}

// Uninstall must recognize the tilde form Install writes for the default
// directory — that is the exact shape a real ~/.claude install produces.
func TestUninstall_MatchesTildeForm(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BinaryPath(claudeDir), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(claudeDir, false); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got, want := statusLineCommand(t, readConfig(t, claudeDir)), "~/.claude/"+binaryName(); got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
	removed, err := Uninstall(claudeDir)
	if err != nil || !removed {
		t.Fatalf("Uninstall = %v, %v; want true, nil", removed, err)
	}
}
