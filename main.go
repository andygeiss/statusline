// Command statusline renders a status line for Claude Code and wires itself
// into ~/.claude. Built against github.com/andygeiss/baseline
// (project-types/cli-tool.md).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"syscall"

	"github.com/andygeiss/statusline/internal/git"
	"github.com/andygeiss/statusline/internal/render"
	"github.com/andygeiss/statusline/internal/settings"
)

var errUsage = errors.New("usage error")

const usage = `usage: statusline [command]

  (no command)  read Claude Code's status JSON from stdin, print the line
  install       point ~/.claude/settings.json at the installed binary
  uninstall     remove the statusLine entry install created
  version       print the version

install/uninstall flags:
  -claude-dir   Claude Code config dir (env STATUSLINE_CLAUDE_DIR, default ~/.claude)
  -force        install only: replace a statusLine entry owned by something else
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Render blocks on stdin, which never arrives when the tool is run by
	// hand. Unregister on the first signal so a second Ctrl-C terminates.
	go func() { <-ctx.Done(); stop() }()
	err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	switch {
	case err == nil:
	case errors.Is(err, errUsage):
		os.Exit(2) // message already printed where the error was detected
	default:
		fmt.Fprintf(os.Stderr, "statusline: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return runRender(stdin, stdout)
	}
	switch args[0] {
	case "install":
		return runInstall(args[1:], stderr)
	case "uninstall":
		return runUninstall(args[1:], stderr)
	case "version":
		fmt.Fprintln(stdout, version())
		return nil
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stderr, usage)
		return nil
	default:
		fmt.Fprintf(stderr, "unknown command %q\n%s", args[0], usage)
		return errUsage
	}
}

// runRender is the mode Claude Code invokes: JSON in, one line out. The line
// deliberately carries ANSI colors — the consumer is Claude Code's terminal
// UI, which renders them (deviation from the baseline's no-ANSI rule,
// recorded in the README).
func runRender(stdin io.Reader, stdout io.Writer) error {
	var in render.Input
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		return fmt.Errorf("decoding status JSON from stdin: %w", err)
	}
	opts := render.Options{Home: homeDir()}
	if dir := in.Workspace.CurrentDir; dir != "" {
		opts.Branch = git.Branch(dir)
	}
	// Claude Code exports the terminal size; tput is unavailable here.
	if columns, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil {
		opts.Width = columns
	}
	fmt.Fprintln(stdout, render.Line(in, opts))
	return nil
}

func runInstall(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	claudeDir := fs.String("claude-dir", defaultClaudeDir(), "Claude Code config dir (env STATUSLINE_CLAUDE_DIR)")
	force := fs.Bool("force", false, "replace a statusLine entry owned by something else")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errUsage
	}
	if err := settings.Install(*claudeDir, *force); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "statusline registered in %s\n", filepath.Join(*claudeDir, "settings.json"))
	return nil
}

func runUninstall(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	claudeDir := fs.String("claude-dir", defaultClaudeDir(), "Claude Code config dir (env STATUSLINE_CLAUDE_DIR)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errUsage
	}
	removed, err := settings.Uninstall(*claudeDir)
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintf(stderr, "statusline removed from %s\n", filepath.Join(*claudeDir, "settings.json"))
	} else {
		fmt.Fprintf(stderr, "no statusline entry of ours in %s; nothing to do\n", filepath.Join(*claudeDir, "settings.json"))
	}
	return nil
}

func defaultClaudeDir() string {
	return envOr("STATUSLINE_CLAUDE_DIR", filepath.Join(homeDir(), ".claude"))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// homeDir returns "" on failure; callers treat that as "no ~ abbreviation".
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return "unknown"
}
