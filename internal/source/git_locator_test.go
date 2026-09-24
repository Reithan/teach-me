package source_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/source"
)

// initGitRepo2Commits creates a git repo with two commits:
//   - commit 1: file.txt with initialContent
//   - commit 2: file.txt with updatedContent
//
// Returns (sha1, sha2) as full 40-hex SHAs.
func initGitRepo2Commits(t *testing.T, dir, initialContent, updatedContent string) (sha1, sha2 string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "T"},
	} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeAndCommit := func(content, msg string) string {
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"add", "file.txt"}, {"commit", "-m", msg}} {
			out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
			if err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatalf("rev-parse HEAD: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	sha1 = writeAndCommit(initialContent, "init")
	sha2 = writeAndCommit(updatedContent, "update")
	return sha1, sha2
}

// resolverForGit builds a Resolver configured for git: locators using the
// alias "r" pointing at repoDir and the given gitBin.
func resolverForGit(t *testing.T, repoDir, gitBin string) *source.Resolver {
	t.Helper()
	cfgContent := "git=" + gitBin + "\nrepo r = " + repoDir + "\n"
	cfg, err := source.LoadConfigPaths(writeConfigFile(t, cfgContent), "")
	if err != nil {
		t.Fatalf("LoadConfigPaths: %v", err)
	}
	return source.NewResolverWithConfig(cfg, repoDir)
}

// writeConfigFile writes content to a temp file and returns its path.
func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// short12 returns the first 12 hex chars of a full SHA.
func short12(sha string) string {
	if len(sha) >= 12 {
		return sha[:12]
	}
	return sha
}

// getDefaultBranch returns the current branch name for the repo in dir.
func getDefaultBranch(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse --abbrev-ref HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestGitLocator_FourForms tests the four git: locator forms.
func TestGitLocator_FourForms(t *testing.T) {
	gitBin := skipIfNoGit(t)
	dir := t.TempDir()

	const initial = "line1\nline2\nline3\n"
	const updated = "line1\nline2 changed\nline3\n"
	sha1, sha2 := initGitRepo2Commits(t, dir, initial, updated)
	s1 := short12(sha1)
	s2 := short12(sha2)

	resolver := resolverForGit(t, dir, gitBin)

	tests := []struct {
		name       string
		locator    string
		start, end int
		wantSubstr string
	}{
		{
			name:       "file at ref (commit 1)",
			locator:    "git:r@" + s1 + ":file.txt",
			start:      1,
			end:        3,
			wantSubstr: "line1",
		},
		{
			name:       "file at ref (commit 2 has changed line)",
			locator:    "git:r@" + s2 + ":file.txt",
			start:      2,
			end:        2,
			wantSubstr: "line2 changed",
		},
		{
			name:       "commit show",
			locator:    "git:r@" + s2,
			start:      1,
			end:        1,
			wantSubstr: "commit",
		},
		{
			name:       "diff",
			locator:    "git:r@" + s1 + ".." + s2,
			start:      1,
			end:        1,
			wantSubstr: "diff",
		},
		{
			name:       "diff for path",
			locator:    "git:r@" + s1 + ".." + s2 + ":file.txt",
			start:      1,
			end:        1,
			wantSubstr: "diff",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := cite.Citation{File: tc.locator, Start: tc.start, End: tc.end}
			text, _, err := resolver.Read(c)
			if err != nil {
				t.Fatalf("Read(%q): %v", tc.locator, err)
			}
			if !strings.Contains(text, tc.wantSubstr) {
				t.Errorf("Read(%q) text = %q; want to contain %q", tc.locator, text, tc.wantSubstr)
			}
		})
	}
}

// TestGitLocator_Refusals tests unknown alias, no git, and bad ref.
func TestGitLocator_Refusals(t *testing.T) {
	gitBin := skipIfNoGit(t)
	dir := t.TempDir()
	initGitRepo(t, dir, "content\n")

	tests := []struct {
		name      string
		cfg       string
		locator   string
		wantInErr string
	}{
		{
			name:      "unknown alias",
			cfg:       "git=" + gitBin + "\nrepo other = " + dir + "\n",
			locator:   "git:r@main:file.txt",
			wantInErr: "unknown repo alias r",
		},
		{
			name:      "no git configured",
			cfg:       "repo r = " + dir + "\n",
			locator:   "git:r@main:file.txt",
			wantInErr: "git: locator needs git",
		},
		{
			name:      "bad ref",
			cfg:       "git=" + gitBin + "\nrepo r = " + dir + "\n",
			locator:   "git:r@nonexistent-branch-xyz:file.txt",
			wantInErr: "does not resolve",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := source.LoadConfigPaths(writeConfigFile(t, tc.cfg), "")
			if err != nil {
				t.Fatalf("LoadConfigPaths: %v", err)
			}
			r := source.NewResolverWithConfig(cfg, dir)
			c := cite.Citation{File: tc.locator, Start: 1, End: 1}
			_, _, readErr := r.Read(c)
			if readErr == nil {
				t.Fatalf("Read(%q): want error, got nil", tc.locator)
			}
			if !strings.Contains(readErr.Error(), tc.wantInErr) {
				t.Errorf("Read(%q) error = %q; want to contain %q", tc.locator, readErr.Error(), tc.wantInErr)
			}
		})
	}
}

// TestGitLocator_PercentEncodedPath verifies that %3A in path is decoded correctly.
func TestGitLocator_PercentEncodedPath(t *testing.T) {
	// Verify ParseGit decodes %3A → ":" in the Path field.
	gl, err := cite.ParseGit("git:r@main:path%3Awith%3Acolon.txt")
	if err != nil {
		t.Fatalf("ParseGit: %v", err)
	}
	if gl.Path != "path:with:colon.txt" {
		t.Errorf("Path = %q, want %q", gl.Path, "path:with:colon.txt")
	}
	// FormatGit re-encodes the colon as %3A.
	if got := cite.FormatGit(gl); got != "git:r@main:path%3Awith%3Acolon.txt" {
		t.Errorf("FormatGit round-trip = %q, want %q", got, "git:r@main:path%3Awith%3Acolon.txt")
	}
}

// TestGitLocator_SHARewriting verifies that HashCitation replaces the ref with
// a 12-hex SHA and that Meta carries Ref (original) and Commit (resolved SHA).
func TestGitLocator_SHARewriting(t *testing.T) {
	gitBin := skipIfNoGit(t)
	dir := t.TempDir()

	const content = "line1\nline2\nline3\n"
	fullSHA := initGitRepo(t, dir, content)
	sha12 := short12(fullSHA)

	branch := getDefaultBranch(t, dir)

	cfgContent := "git=" + gitBin + "\nrepo r = " + dir + "\n"
	cfg, err := source.LoadConfigPaths(writeConfigFile(t, cfgContent), "")
	if err != nil {
		t.Fatalf("LoadConfigPaths: %v", err)
	}
	r := source.NewResolverWithConfig(cfg, dir)

	citeStr := "git:r@" + branch + ":file.txt:1-2"
	hashedCite, meta, err := r.HashCitation(citeStr)
	if err != nil {
		t.Fatalf("HashCitation(%q): %v", citeStr, err)
	}

	// The returned citation must contain the SHA-pinned locator.
	wantLocator := "git:r@" + sha12 + ":file.txt"
	if !strings.Contains(hashedCite, wantLocator) {
		t.Errorf("HashCitation(%q) = %q; want locator %q", citeStr, hashedCite, wantLocator)
	}
	// Meta must carry ref (original branch) and commit (the SHA).
	if meta.Ref != branch {
		t.Errorf("Meta.Ref = %q, want %q", meta.Ref, branch)
	}
	if meta.Commit != sha12 {
		t.Errorf("Meta.Commit = %q, want %q", meta.Commit, sha12)
	}
}
