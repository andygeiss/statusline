// Package git reads the current branch straight from .git — no subprocess,
// so the statusline stays fast even in huge repositories.
package git

import (
	"os"
	"path/filepath"
	"strings"
)

// Branch reports the branch checked out in dir or any parent, walking up
// like git itself does. Detached HEAD reports the abbreviated commit hash.
// It returns "" when dir is not inside a repository or on any read error —
// the statusline then simply omits the segment.
func Branch(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		gitPath := filepath.Join(d, ".git")
		if fi, err := os.Stat(gitPath); err == nil {
			if !fi.IsDir() {
				// Worktrees and submodules use a ".git" file pointing at
				// the real git directory.
				gitPath = linkedGitDir(gitPath, d)
				if gitPath == "" {
					return ""
				}
			}
			return headBranch(gitPath)
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// linkedGitDir resolves a ".git" file's "gitdir: <path>" line, which may be
// relative to the directory containing the file.
func linkedGitDir(gitFile, dir string) string {
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return ""
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
	if !ok {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	return target
}

func headBranch(gitDir string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(data))
	if branch, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
		return branch
	}
	// Detached HEAD: the file holds a full commit hash.
	if len(head) >= 7 {
		return head[:7]
	}
	return ""
}
