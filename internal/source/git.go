package source

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
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

// CommitForPath reads the HEAD commit SHA for the git repo containing path.
// It resolves HEAD by reading .git/HEAD, then the ref under refs/heads/ or
// in packed-refs, following commondir for worktrees. No git process is exec'd.
// Returns "" when the path is not inside a repo or the ref cannot be resolved.
func CommitForPath(path string) string {
	repoDir, gitDir := FindGitRepo(path)
	if repoDir == "" {
		return ""
	}
	return resolveHEAD(gitDir)
}

// resolveHEAD resolves HEAD in gitDir to a commit SHA.
func resolveHEAD(gitDir string) string {
	// Follow commondir for worktrees (the common dir holds the actual refs).
	commonDir := resolveCommonDir(gitDir)

	// Read HEAD — prefer the worktree's HEAD (which holds the checked-out branch).
	headData, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		headData, err = os.ReadFile(filepath.Join(commonDir, "HEAD"))
		if err != nil {
			return ""
		}
	}

	headStr := strings.TrimSpace(string(headData))

	// Symbolic ref: "ref: refs/heads/<branch>"
	if strings.HasPrefix(headStr, "ref: ") {
		ref := strings.TrimPrefix(headStr, "ref: ")
		return resolveRef(gitDir, commonDir, ref)
	}

	// Detached HEAD: the SHA itself.
	if isHexSHA(headStr) {
		return headStr
	}
	return ""
}

// resolveCommonDir returns the "common" git directory for a worktree.
// For a regular repo, this returns gitDir unchanged.
func resolveCommonDir(gitDir string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	common := strings.TrimSpace(string(data))
	if common == "" {
		return gitDir
	}
	if filepath.IsAbs(common) {
		return filepath.Clean(common)
	}
	return filepath.Clean(filepath.Join(gitDir, common))
}

// resolveRef resolves a fully-qualified ref (e.g. "refs/heads/main") to a SHA.
// It searches loose refs in gitDir and commonDir, then packed-refs.
func resolveRef(gitDir, commonDir, ref string) string {
	for _, dir := range uniqueDirs(gitDir, commonDir) {
		refPath := filepath.Join(dir, filepath.FromSlash(ref))
		data, err := os.ReadFile(refPath)
		if err == nil {
			sha := strings.TrimSpace(string(data))
			if isHexSHA(sha) {
				return sha
			}
		}
	}
	// Fall back to packed-refs.
	for _, dir := range uniqueDirs(gitDir, commonDir) {
		if sha := findInPackedRefs(dir, ref); sha != "" {
			return sha
		}
	}
	return ""
}

// findInPackedRefs searches <gitDir>/packed-refs for ref and returns its SHA.
func findInPackedRefs(gitDir, ref string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		// Format: "<sha> <refname>"
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == ref && isHexSHA(fields[0]) {
			return fields[0]
		}
	}
	return ""
}

// HeadBlob runs the configured git command to fetch a blob at HEAD:<relpath>
// inside repoDir. Returns raw bytes on success.
func HeadBlob(gitCmd, repoDir, relpath string) ([]byte, error) {
	cmd := exec.Command(gitCmd, "-C", repoDir, "cat-file", "-p", "HEAD:"+relpath)
	return cmd.Output()
}

// isHexSHA reports whether s is a 40-character hexadecimal string.
func isHexSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// uniqueDirs returns the directories as a deduplicated slice preserving order.
func uniqueDirs(a, b string) []string {
	if a == b {
		return []string{a}
	}
	return []string{a, b}
}
