package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repo builds a minimal .git directory by hand — the package reads files
// directly, so no git binary is needed.
func repo(t *testing.T, head string) string {
	t.Helper()
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBranch(t *testing.T) {
	t.Parallel()

	t.Run("branch checkout", func(t *testing.T) {
		t.Parallel()
		dir := repo(t, "ref: refs/heads/main\n")
		if got := Branch(dir); got != "main" {
			t.Errorf("Branch = %q, want main", got)
		}
	})

	t.Run("subdirectory walks up to the repository root", func(t *testing.T) {
		t.Parallel()
		dir := repo(t, "ref: refs/heads/feature/x\n")
		sub := filepath.Join(dir, "a", "b")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if got := Branch(sub); got != "feature/x" {
			t.Errorf("Branch = %q, want feature/x", got)
		}
	})

	t.Run("detached HEAD reports short hash", func(t *testing.T) {
		t.Parallel()
		dir := repo(t, "0123456789abcdef0123456789abcdef01234567\n")
		if got := Branch(dir); got != "0123456" {
			t.Errorf("Branch = %q, want 0123456", got)
		}
	})

	t.Run("worktree gitdir file with relative path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		real := filepath.Join(dir, "real-gitdir")
		if err := os.Mkdir(real, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(real, "HEAD"), []byte("ref: refs/heads/wt\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: real-gitdir\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := Branch(dir); got != "wt" {
			t.Errorf("Branch = %q, want wt", got)
		}
	})

	t.Run("outside any repository", func(t *testing.T) {
		t.Parallel()
		if got := Branch(t.TempDir()); got != "" {
			t.Errorf("Branch = %q, want empty", got)
		}
	})

	t.Run("corrupt HEAD yields empty", func(t *testing.T) {
		t.Parallel()
		dir := repo(t, "??\n")
		if got := Branch(dir); got != "" {
			t.Errorf("Branch = %q, want empty", got)
		}
	})

	t.Run("long non-hash HEAD content yields empty", func(t *testing.T) {
		t.Parallel()
		dir := repo(t, "this is not a commit hash at all!\n")
		if got := Branch(dir); got != "" {
			t.Errorf("Branch = %q, want empty", got)
		}
	})

	t.Run("sha256 detached HEAD reports short hash", func(t *testing.T) {
		t.Parallel()
		dir := repo(t, strings.Repeat("0123456789abcdef", 4)+"\n")
		if got := Branch(dir); got != "0123456" {
			t.Errorf("Branch = %q, want 0123456", got)
		}
	})
}
