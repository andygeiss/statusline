// Package settings registers and removes the statusline binary in Claude
// Code's settings.json. It edits only the statusLine key and preserves every
// other setting byte-for-byte in meaning (the file is re-marshaled, so key
// order and indentation normalize).
package settings

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// rawNumbers keeps every JSON number's exact text through the read/write
// round trip. Unmarshaling into an any lands a number in a float64, which
// corrupts a 64-bit id in a setting we never look at; a raw value re-marshals
// as the bytes it came from.
var rawNumbers = json.WithUnmarshalers(json.UnmarshalFromFunc(
	func(dec *jsontext.Decoder, val *any) error {
		if dec.PeekKind() != '0' {
			return errors.ErrUnsupported // every other kind decodes as usual
		}
		num, err := dec.ReadValue()
		if err != nil {
			return err
		}
		*val = num.Clone() // ReadValue's buffer is reused by the next read
		return nil
	}))

// Command reports the value written to settings.json. Claude Code runs it
// through a POSIX shell (Git Bash on Windows), so the default ~/.claude gets
// the "~" form — tilde expansion is not word-split, surviving spaces in the
// home path — and other dirs get the absolute path with forward slashes
// (Git Bash strips unquoted backslashes).
func Command(claudeDir string) string {
	if home, err := os.UserHomeDir(); err == nil && claudeDir == filepath.Join(home, ".claude") && tildeSafe(home) {
		return "~/.claude/" + binaryName()
	}
	command := filepath.ToSlash(BinaryPath(claudeDir))
	if !shellSafe(command) {
		// Single quotes: the shell would word-split a space and expand $,
		// backticks, and globs — even inside double quotes for the first two.
		command = "'" + strings.ReplaceAll(command, "'", `'\''`) + "'"
	}
	return command
}

// shellSafe reports whether s survives sh -c without quoting: ASCII
// letters, digits, and inert path punctuation only. Anything else —
// including non-ASCII, which quoting preserves anyway — gets quoted.
func shellSafe(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '/', r == '.', r == '-', r == '_', r == ':', r == '+', r == ',', r == '@':
		default:
			return false
		}
	}
	return true
}

// tildeSafe reports whether "~" expands to home in the shell Claude Code
// uses. On Windows, Git Bash prefers an inherited HOME over the
// USERPROFILE-derived default that os.UserHomeDir reads; when the two
// disagree, the tilde form would point somewhere else entirely.
func tildeSafe(home string) bool {
	if runtime.GOOS != "windows" {
		return true
	}
	h := os.Getenv("HOME")
	return h == "" || filepath.Clean(h) == filepath.Clean(home)
}

// BinaryPath reports where the statusline binary lives inside claudeDir.
func BinaryPath(claudeDir string) string {
	return filepath.Join(claudeDir, binaryName())
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "statusline.exe"
	}
	return "statusline"
}

// Install points settings.json's statusLine at the installed binary. A
// statusLine entry already pointing at a different command is someone else's
// configuration: Install refuses to replace it unless force is set.
// Rerunning is idempotent and keeps keys the user added to our entry
// (Claude Code supports e.g. "padding").
func Install(claudeDir string, force bool) error {
	// Normalize the dir: tab-completion's trailing slash would make Command
	// miss the tilde form, and a relative dir would register a command that
	// breaks as soon as Claude Code runs it from another working directory.
	if abs, err := filepath.Abs(claudeDir); err == nil {
		claudeDir = abs
	}
	// Registering a command that doesn't exist would leave Claude Code with
	// a silently broken statusline — e.g. a downloaded release binary run
	// from ~/Downloads. Require the binary to be in place first.
	bin := BinaryPath(claudeDir)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("no statusline binary at %s — copy this binary there first (make install does both): %w", bin, err)
	}
	cfg, path, err := read(claudeDir)
	if err != nil {
		return err
	}
	existing, ok := cfg["statusLine"]
	if ok && !pointsAt(existing, claudeDir) {
		if !force {
			return fmt.Errorf("%s already sets a different statusLine (%s); rerun with -force to replace it", path, describe(existing))
		}
		existing = nil // foreign entry: its extra keys belong to its command, replace wholesale
	}
	entry, _ := existing.(map[string]any)
	if entry == nil {
		entry = map[string]any{}
	}
	entry["type"] = "command"
	entry["command"] = Command(claudeDir)
	cfg["statusLine"] = entry
	return write(path, cfg)
}

// Uninstall removes the statusLine entry when it points at our binary. It
// reports whether an entry was removed; an entry owned by something else is
// left alone.
func Uninstall(claudeDir string) (removed bool, err error) {
	if abs, err := filepath.Abs(claudeDir); err == nil { // see Install
		claudeDir = abs
	}
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
// round trip. A missing file is an empty configuration, not an error;
// anything the file cannot mean — trailing data after the top-level value,
// a duplicate key, invalid UTF-8 — is an error rather than a rewrite that
// silently drops half of it.
func read(claudeDir string) (cfg map[string]any, path string, err error) {
	path = filepath.Join(claudeDir, "settings.json")
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return map[string]any{}, path, nil
	case err != nil:
		return nil, path, fmt.Errorf("reading %s: %w", path, err)
	}
	// A touched-but-empty file is an empty configuration like a missing
	// one — not a cryptic "parsing …: EOF" failure.
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, path, nil
	}
	if err := json.Unmarshal(data, &cfg, rawNumbers); err != nil {
		return nil, path, fmt.Errorf("parsing %s: %w", path, err)
	}
	if cfg == nil { // a file holding literal "null" decodes to a nil map
		cfg = map[string]any{}
	}
	return cfg, path, nil
}

// write replaces settings.json atomically (temp file + rename) so an
// interrupted run can never leave Claude Code with half a file. The rename
// would discard the old file's mode, so it is carried over explicitly — a
// user who keeps settings.json at 0600 must not end up world-readable.
func write(path string, cfg map[string]any) error {
	// Deterministic sorts the map: without it every rewrite would reshuffle
	// the user's settings.json into a fresh random key order.
	data, err := json.Marshal(cfg, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	// A dotfiles-managed settings.json is often a symlink; renaming over
	// the link would sever it. Write through to the real file instead —
	// even when the link dangles (dotfiles not yet restored), where
	// EvalSymlinks fails and one manual Readlink hop finds the target.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	} else if target, err := os.Readlink(path); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		path = target
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
// accepting the "~" form and the absolute path in either slash direction.
func pointsAt(entry any, claudeDir string) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	command, _ := m["command"].(string)
	return command == Command(claudeDir) ||
		filepath.ToSlash(command) == filepath.ToSlash(BinaryPath(claudeDir))
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
