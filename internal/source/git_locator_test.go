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
		// A ref that begins with "-" must be refused before reaching git so it
		// cannot be interpreted as a flag (one case per form).
		{
			name:      "dash ref: file-at-ref form",
			cfg:       "git=" + gitBin + "\nrepo r = " + dir + "\n",
			locator:   "git:r@-bad:file.txt",
			wantInErr: "ref must not start with -",
		},
		{
			name:      "dash ref: commit form",
			cfg:       "git=" + gitBin + "\nrepo r = " + dir + "\n",
			locator:   "git:r@-bad",
			wantInErr: "ref must not start with -",
		},
		{
			name:      "dash ref: diff form",
			cfg:       "git=" + gitBin + "\nrepo r = " + dir + "\n",
			locator:   "git:r@-bad..main",
			wantInErr: "ref must not start with -",
		},
		{
			name:      "dash ref: diff-for-path form",
			cfg:       "git=" + gitBin + "\nrepo r = " + dir + "\n",
			locator:   "git:r@-bad..main:file.txt",
			wantInErr: "ref must not start with -",
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

// TestGitLocator_SHARewriting verifies that HashCitation rewrites refs to
// 12-hex SHAs in the ResolvedLocator for all four git: forms.
func TestGitLocator_SHARewriting(t *testing.T) {
	gitBin := skipIfNoGit(t)
	dir := t.TempDir()

	const initial = "line1\nline2\nline3\n"
	const updated = "line1\nline2 changed\nline3\n"
	sha1, sha2 := initGitRepo2Commits(t, dir, initial, updated)
	s1 := short12(sha1)
	s2 := short12(sha2)
	branch := getDefaultBranch(t, dir) // points to sha2

	cfgContent := "git=" + gitBin + "\nrepo r = " + dir + "\n"
	cfg, err := source.LoadConfigPaths(writeConfigFile(t, cfgContent), "")
	if err != nil {
		t.Fatalf("LoadConfigPaths: %v", err)
	}
	r := source.NewResolverWithConfig(cfg, dir)

	tests := []struct {
		name         string
		citeStr      string
		wantResolved string // exact expected meta.ResolvedLocator
	}{
		{
			name:         "file at ref: branch → 12-hex SHA",
			citeStr:      "git:r@" + branch + ":file.txt:1-2",
			wantResolved: "git:r@" + s2 + ":file.txt",
		},
		{
			name:         "commit: branch → 12-hex SHA",
			citeStr:      "git:r@" + branch + ":1-3",
			wantResolved: "git:r@" + s2,
		},
		{
			name:         "diff: full SHAs → 12-hex",
			citeStr:      "git:r@" + sha1 + ".." + sha2 + ":1-1",
			wantResolved: "git:r@" + s1 + ".." + s2,
		},
		{
			name:         "diff for path: full SHAs → 12-hex",
			citeStr:      "git:r@" + sha1 + ".." + sha2 + ":file.txt:1-1",
			wantResolved: "git:r@" + s1 + ".." + s2 + ":file.txt",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hashedCite, meta, err := r.HashCitation(tc.citeStr)
			if err != nil {
				t.Fatalf("HashCitation(%q): %v", tc.citeStr, err)
			}
			if meta.ResolvedLocator != tc.wantResolved {
				t.Errorf("meta.ResolvedLocator = %q; want %q", meta.ResolvedLocator, tc.wantResolved)
			}
			if !strings.Contains(hashedCite, tc.wantResolved) {
				t.Errorf("hashedCite %q does not contain SHA-pinned locator %q", hashedCite, tc.wantResolved)
			}
		})
	}

	// File-at-ref additionally populates Meta.Ref (original ref) and Meta.Commit.
	t.Run("file at ref: meta fields", func(t *testing.T) {
		citeStr := "git:r@" + branch + ":file.txt:1-2"
		_, meta, err := r.HashCitation(citeStr)
		if err != nil {
			t.Fatalf("HashCitation: %v", err)
		}
		if meta.Ref != branch {
			t.Errorf("Meta.Ref = %q, want %q", meta.Ref, branch)
		}
		if meta.Commit != s2 {
			t.Errorf("Meta.Commit = %q, want %q", meta.Commit, s2)
		}
	})
}

// TestGitLocator_ConverterCases verifies that a git: file-at-ref citation with a
// mapped extension runs the converter (the sliced text is the converted output),
// while a diff-for-path citation with the same extension does NOT run the converter.
func TestGitLocator_ConverterCases(t *testing.T) {
	gitBin := skipIfNoGit(t)
	dir := t.TempDir()
	cvDir := t.TempDir()

	// Content that changes visibly after HTML-tag stripping.
	const initial = "<p>alpha</p>\n<p>beta</p>\n"
	const updated = "<p>alpha</p>\n<p>beta changed</p>\n"
	sha1, sha2 := initGitRepo2Commits(t, dir, initial, updated)
	s1 := short12(sha1)
	s2 := short12(sha2)

	// Build a tag-stripping converter; map ".txt" → text/html so the git
	// repo's file.txt is dispatched through it.
	conv := writeScript(t, cvDir, "conv", "sed 's/<[^>]*>//g'")
	ver := writeScript(t, cvDir, "conv_ver", `printf "1.0"`)

	cfgContent := "git=" + gitBin + "\nrepo r = " + dir + "\n" +
		"convert text/html=" + conv + "\n" +
		"version " + conv + "=1.0\n" +
		"version-cmd " + conv + "=" + ver + "\n" +
		"ext .txt=text/html\n"
	cfg, err := source.LoadConfigPaths(writeConfigFile(t, cfgContent), "")
	if err != nil {
		t.Fatalf("LoadConfigPaths: %v", err)
	}
	r := source.NewResolverWithConfig(cfg, dir)

	// Case 1: file-at-ref → converter applied; sliced text is the stripped output.
	t.Run("file at ref with converter: text is converted", func(t *testing.T) {
		c := cite.Citation{File: "git:r@" + s2 + ":file.txt", Start: 1, End: 2}
		text, meta, err := r.Read(c)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		// Strip "<p>alpha</p>\n<p>beta changed</p>\n" → "alpha\nbeta changed".
		if text != "alpha\nbeta changed" {
			t.Errorf("text = %q; want converted output %q", text, "alpha\nbeta changed")
		}
		if meta.Converter == "" {
			t.Errorf("meta.Converter should be set; got empty")
		}
	})

	// Case 2: diff-for-path with the same ".txt" extension → NOT converted.
	t.Run("diff for path: not converted despite mapped extension", func(t *testing.T) {
		c := cite.Citation{File: "git:r@" + s1 + ".." + s2 + ":file.txt", Start: 1, End: 1}
		text, meta, err := r.Read(c)
		if err != nil {
			t.Fatalf("Read diff: %v", err)
		}
		// Diff output starts with the git diff header, not converted text.
		if !strings.Contains(text, "diff") {
			t.Errorf("diff text %q should contain 'diff'", text)
		}
		if meta.Converter != "" {
			t.Errorf("meta.Converter should be empty for diff form; got %q", meta.Converter)
		}
	})
}
