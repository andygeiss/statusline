// Package settings registers and removes the statusline binary in Claude
// Code's settings.json. It edits only the statusLine key and preserves every
// other setting byte-for-byte in meaning (the file is re-marshaled, so key
// order and indentation normalize).
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Command reports the value written to settings.json: the portable "~" form
// when claudeDir is the default ~/.claude, the absolute path otherwise.
// Claude Code expands "~" itself.
func Command(claudeDir string) string {
	if home, err := os.UserHomeDir(); err == nil && claudeDir == filepath.Join(home, ".claude") {
		return "~/.claude/statusline"
	}
	return filepath.Join(claudeDir, "statusline")
}

// BinaryPath reports where the statusline binary lives inside claudeDir.
func BinaryPath(claudeDir string) string {
	return filepath.Join(claudeDir, "statusline")
}

// Install points settings.json's statusLine at the installed binary. A
// statusLine entry already pointing at a different command is someone else's
// configuration: Install refuses to replace it unless force is set.
// Rerunning is idempotent.
func Install(claudeDir string, force bool) error {
	cfg, path, err := read(claudeDir)
	if err != nil {
		return err
	}
	command := Command(claudeDir)
	if existing, ok := cfg["statusLine"]; ok && !force && !pointsAt(existing, claudeDir) {
		return fmt.Errorf("%s already sets a different statusLine (%s); rerun with -force to replace it", path, describe(existing))
	}
	cfg["statusLine"] = map[string]any{
		"type":    "command",
		"command": command,
	}
	return write(path, cfg)
}

// Uninstall removes the statusLine entry when it points at our binary. It
// reports whether an entry was removed; an entry owned by something else is
// left alone.
func Uninstall(claudeDir string) (removed bool, err error) {
	cfg, path, err := read(claudeDir)
	if err != nil {
		return false, err
	}
	existing, ok := cfg["statusLine"]
	if !ok || !pointsAt(existing, claudeDir) {
		return false, nil
	}
	delete(cfg, "statusLine")
	return true, write(path, cfg)
}

// read loads settings.json into a generic map so unknown keys survive the
// round trip. A missing file is an empty configuration, not an error.
func read(claudeDir string) (cfg map[string]any, path string, err error) {
	path = filepath.Join(claudeDir, "settings.json")
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return map[string]any{}, path, nil
	case err != nil:
		return nil, path, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, path, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, path, nil
}

// write replaces settings.json atomically (temp file + rename) so an
// interrupted run can never leave Claude Code with half a file. The rename
// would discard the old file's mode, so it is carried over explicitly — a
// user who keeps settings.json at 0600 must not end up world-readable.
func write(path string, cfg map[string]any) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.json")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// pointsAt reports whether an existing statusLine entry targets our binary,
// accepting both the "~" form and the absolute path.
func pointsAt(entry any, claudeDir string) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	command, _ := m["command"].(string)
	return command == Command(claudeDir) || command == BinaryPath(claudeDir)
}

// describe summarizes a foreign statusLine entry for the refusal message.
func describe(entry any) string {
	if m, ok := entry.(map[string]any); ok {
		if command, ok := m["command"].(string); ok && command != "" {
			return fmt.Sprintf("command %q", command)
		}
	}
	return "a non-command entry"
}
