package source

import (
	"os"
	"path/filepath"
	"strings"
)

// FindGitRepo walks up from path looking for a .git directory or a gitdir file.
// It returns (repoDir, gitDir) where repoDir is the working tree root and
// gitDir is the actual .git directory (may differ for worktrees).
// Returns ("", "") when no git repo is found.
func FindGitRepo(path string) (repoDir, gitDir string) {
	dir := path
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	for {
		gitPath := filepath.Join(dir, ".git")
		info, err := os.Stat(gitPath)
		if err == nil {
			if info.IsDir() {
				return dir, gitPath
			}
			// It's a file: "gitdir: <path>" (worktree or submodule).
			data, readErr := os.ReadFile(gitPath)
			if readErr == nil {
				content := strings.TrimSpace(string(data))
				if strings.HasPrefix(content, "gitdir: ") {
					rel := strings.TrimPrefix(content, "gitdir: ")
					if !filepath.IsAbs(rel) {
						rel = filepath.Join(dir, rel)
					}
					return dir, filepath.Clean(rel)
				}
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // filesystem root
		}
		dir = parent
	}
	return "", ""
}
