package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ── tm src tests ──────────────────────────────────────────────────────────────

// srcMakeGraph creates a new graph file in dir and sets TM_FILE.
// Returns the graph file path.
func srcMakeGraph(t *testing.T, dir string) string {
	t.Helper()
	mmdFile := filepath.Join(dir, "g.mmd")
	if _, errOut, code := run(t, "new", mmdFile); code != 0 {
		t.Fatalf("tm new: exit %d; %s", code, errOut)
	}
	t.Setenv("TM_FILE", mmdFile)
	return mmdFile
}

// srcWriteFile writes content to filepath.Join(dir, "src.txt") and returns the path.
func srcWriteFile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("srcWriteFile: %v", err)
	}
	return p
}

// TestTmSrc_NumberedOutput verifies that plain-path numbered output is
// N\t<text> starting from line 1.
func TestTmSrc_NumberedOutput(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	srcMakeGraph(t, dir)
	srcSetupXDG(t, "")

	f := srcWriteFile(t, dir, "alpha\nbeta\ngamma\n")

	out, errOut, code := run(t, "src", f)
	if code != 0 {
		t.Fatalf("tm src exit %d; stderr: %s", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %v", len(lines), lines)
	}
	wantLines := []string{"1\talpha", "2\tbeta", "3\tgamma"}
	for i, want := range wantLines {
		if lines[i] != want {
			t.Errorf("line %d: got %q, want %q", i+1, lines[i], want)
		}
	}
}

// TestTmSrc_RangeOutput verifies START-END restricts printed lines and
// out-of-range exits 3 with an error message.
func TestTmSrc_RangeOutput(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	srcMakeGraph(t, dir)
	srcSetupXDG(t, "")

	f := srcWriteFile(t, dir, "one\ntwo\nthree\nfour\n")

	tests := []struct {
		name      string
		rangeArg  string
		wantCode  int
		wantLines []string
		wantInErr string
	}{
		{
			name:      "valid range 2-3",
			rangeArg:  "2-3",
			wantCode:  0,
			wantLines: []string{"2\ttwo", "3\tthree"},
		},
		{
			name:      "single-line range 1-1",
			rangeArg:  "1-1",
			wantCode:  0,
			wantLines: []string{"1\tone"},
		},
		{
			name:      "out of bounds",
			rangeArg:  "3-10",
			wantCode:  3,
			wantInErr: "out of bounds",
		},
		{
			name:      "invalid format",
			rangeArg:  "notarange",
			wantCode:  3,
			wantInErr: "START-END",
		},
		{
			name:      "zero start rejects s<1",
			rangeArg:  "0-3",
			wantCode:  3,
			wantInErr: "START <= END",
		},
		{
			name:      "reversed range rejects e<s",
			rangeArg:  "4-2",
			wantCode:  3,
			wantInErr: "START <= END",
		},
		{
			name:      "empty end segment",
			rangeArg:  "3-",
			wantCode:  3,
			wantInErr: "START-END",
		},
		{
			name:      "non-numeric start",
			rangeArg:  "abc-3",
			wantCode:  3,
			wantInErr: "START-END",
		},
		{
			name:      "non-numeric end",
			rangeArg:  "3-abc",
			wantCode:  3,
			wantInErr: "START-END",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := run(t, "src", f, tc.rangeArg)
			if code != tc.wantCode {
				t.Fatalf("exit %d, want %d; stderr=%s", code, tc.wantCode, errOut)
			}
			if tc.wantInErr != "" {
				if !strings.Contains(errOut, tc.wantInErr) {
					t.Errorf("stderr=%q; want %q", errOut, tc.wantInErr)
				}
				return
			}
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if len(lines) != len(tc.wantLines) {
				t.Fatalf("want %d lines, got %d: %v", len(tc.wantLines), len(lines), lines)
			}
			for i, want := range tc.wantLines {
				if lines[i] != want {
					t.Errorf("line %d: got %q, want %q", i, lines[i], want)
				}
			}
		})
	}
}

// TestTmSrc_Find verifies --find filters to matching lines, returns empty on no
// match (exit 0), and exits 3 for an invalid regex.
func TestTmSrc_Find(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	srcMakeGraph(t, dir)
	srcSetupXDG(t, "")

	f := srcWriteFile(t, dir, "foo bar\nbaz\nfoo baz\n")

	tests := []struct {
		name      string
		args      []string
		wantCode  int
		wantLines []string
		wantInErr string
	}{
		{
			name:      "matching lines only",
			args:      []string{"src", f, "--find", "foo"},
			wantCode:  0,
			wantLines: []string{"1\tfoo bar", "3\tfoo baz"},
		},
		{
			name:     "no match is exit 0 empty output",
			args:     []string{"src", f, "--find", "zzz"},
			wantCode: 0,
		},
		{
			name:      "invalid regex exits 3",
			args:      []string{"src", f, "--find", "[invalid"},
			wantCode:  3,
			wantInErr: "bad --find",
		},
		{
			name:      "find within range",
			args:      []string{"src", f, "1-2", "--find", "baz"},
			wantCode:  0,
			wantLines: []string{"2\tbaz"},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := run(t, tc.args...)
			if code != tc.wantCode {
				t.Fatalf("exit %d, want %d; stderr=%s", code, tc.wantCode, errOut)
			}
			if tc.wantInErr != "" {
				if !strings.Contains(errOut, tc.wantInErr) {
					t.Errorf("stderr=%q; want %q", errOut, tc.wantInErr)
				}
				return
			}
			if len(tc.wantLines) == 0 {
				if strings.TrimSpace(out) != "" {
					t.Errorf("want empty output, got %q", out)
				}
				return
			}
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if len(lines) != len(tc.wantLines) {
				t.Fatalf("want %d lines, got %d: %v", len(tc.wantLines), len(lines), lines)
			}
			for i, want := range tc.wantLines {
				if lines[i] != want {
					t.Errorf("line %d: got %q, want %q", i, lines[i], want)
				}
			}
		})
	}
}

// TestTmSrc_HashPrefixRefused verifies that a locator with a hash@ prefix is
// rejected with exit 3 — tm src expects a raw locator, not a hash-prefixed citation.
func TestTmSrc_HashPrefixRefused(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	srcMakeGraph(t, dir)
	srcSetupXDG(t, "")

	// 12-char hex + '@' triggers the hash-prefix guard (locator[12] == '@').
	locator := "abcdefabcdef@file.txt"
	_, errOut, code := run(t, "src", locator)
	if code != 3 {
		t.Fatalf("exit %d, want 3; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "hash prefix") {
		t.Errorf("stderr=%q; want \"hash prefix\"", errOut)
	}
}

// TestTmSrc_HashPrefixGuardSkipsGit verifies that a git: locator whose alias
// name is exactly 8 characters (making locator[12] == '@') is NOT refused by
// the hash-prefix guard — the guard must skip git: locators entirely.
func TestTmSrc_HashPrefixGuardSkipsGit(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	srcMakeGraph(t, dir)
	srcSetupXDG(t, "")

	// "git:myalias1@main:f.txt": alias "myalias1" is 8 chars, so locator[12]=='@'.
	// The old guard would fire here; with the fix it must not.
	_, errOut, code := run(t, "src", "git:myalias1@main:f.txt")
	if code == 3 && strings.Contains(errOut, "hash prefix") {
		t.Error("git: locator with 8-char alias was wrongly refused as hash-prefixed")
	}
}

// TestTmSrc_GitLocatorHeader verifies that tm src on a git locator prints a
// header line with the resolved SHA form that matches what tm add stores.
func TestTmSrc_GitLocatorHeader(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)

	// Init git repo and commit file.txt.
	sha := srcInitGitRepo(t, dir, "line1\nline2\nline3\n")
	short12 := sha[:12]

	mmdFile := srcMakeGraph(t, dir)
	gitBin, _ := exec.LookPath("git")
	xdgCfg := fmt.Sprintf("git=%s\nrepo r = %s\n", gitBin, dir)
	srcSetupXDG(t, xdgCfg)

	// Run tm src with a branch-ref locator.
	branch, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	branchName := strings.TrimSpace(string(branch))
	locator := fmt.Sprintf("git:r@%s:file.txt", branchName)

	out, errOut, code := run(t, "src", locator)
	if code != 0 {
		t.Fatalf("tm src exit %d; stderr: %s", code, errOut)
	}

	// Header must be "src: git:r@<sha12>:file.txt".
	outLines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(outLines) < 1 {
		t.Fatal("no output")
	}
	wantHeader := fmt.Sprintf("src: git:r@%s:file.txt", short12)
	if outLines[0] != wantHeader {
		t.Errorf("header = %q, want %q", outLines[0], wantHeader)
	}

	// Add a concept using the same branch locator and verify the stored cite
	// uses the same short-SHA form.
	citeArg := fmt.Sprintf("git:r@%s:file.txt:1-2", branchName)
	if _, errOut2, code2 := run(t, "add", "c1", citeArg, "scope"); code2 != 0 {
		t.Fatalf("tm add exit %d; stderr: %s", code2, errOut2)
	}
	showOut, _, _ := run(t, "show", "c1", "--file", mmdFile)
	// The stored src line must contain the short12 SHA.
	if !strings.Contains(showOut, short12) {
		t.Errorf("show output %q; want to contain SHA %s", showOut, short12)
	}
	// The header's locator must appear in the show output (file portion without hash/range).
	headerLocator := strings.TrimPrefix(outLines[0], "src: ")
	if !strings.Contains(showOut, headerLocator) {
		t.Errorf("stored cite does not contain locator %q from header; show=%q", headerLocator, showOut)
	}
}

// TestTmSrc_URICacheSharing verifies that a URI fetched by tm src is served
// from cache when tm add follows within the same TTL (request count stays 1).
func TestTmSrc_URICacheSharing(t *testing.T) {
	fetches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches++
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "line one\nline two\n")
	}))
	t.Cleanup(srv.Close)

	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	srcMakeGraph(t, dir)
	cacheDir := t.TempDir()
	t.Setenv("TM_CACHE_DIR", cacheDir)
	// Set a long cache TTL so tm add hits the cache.
	srcSetupXDG(t, "cache-ttl = 5m\n")

	url := srv.URL + "/doc.txt"

	// tm src: fetches and populates the cache.
	if _, errOut, code := run(t, "src", url); code != 0 {
		t.Fatalf("tm src exit %d; stderr: %s", code, errOut)
	}
	if fetches != 1 {
		t.Errorf("after tm src: fetches=%d, want 1", fetches)
	}

	// tm add: must hit cache, no new fetch.
	if _, errOut, code := run(t, "add", "c1", url+":1-1", "scope"); code != 0 {
		t.Fatalf("tm add exit %d; stderr: %s", code, errOut)
	}
	if fetches != 1 {
		t.Errorf("after tm add: fetches=%d, want 1 (cache hit)", fetches)
	}
}

// TestTmSrc_GraderRefused verifies that TM_ROLE=grader refuses tm src.
func TestTmSrc_GraderRefused(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	srcMakeGraph(t, dir)

	t.Setenv("TM_ROLE", "grader")
	f := srcWriteFile(t, dir, "content\n")

	_, errOut, code := run(t, "src", f)
	if code != 1 {
		t.Fatalf("want exit 1 for grader, got %d", code)
	}
	if !strings.Contains(errOut, "grader") {
		t.Errorf("stderr=%q; want 'grader'", errOut)
	}
}

// TestTmSrc_FetchFailureErrlog verifies that a fetch failure exits 1 and
// writes an errlog row.
func TestTmSrc_FetchFailureErrlog(t *testing.T) {
	errlogPath := tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	srcMakeGraph(t, dir)
	srcSetupXDG(t, "")

	// Port 1 is reserved; connect will be refused immediately.
	_, errOut, code := run(t, "src", "http://127.0.0.1:1/doc")
	if code != 1 {
		t.Fatalf("want exit 1 for fetch failure, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "fetch") {
		t.Errorf("stderr=%q; want 'fetch'", errOut)
	}
	// Verify errlog row was written.
	rows := readErrlog(t, errlogPath)
	if len(rows) == 0 {
		t.Fatal("want errlog row, got none")
	}
	found := false
	for _, row := range rows {
		if strings.Contains(row.Err, "fetch") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no errlog row with 'fetch'; rows=%+v", rows)
	}
}

// TestTmSrc_AidsDirRefusalShortForm verifies that when tm src resolves a
// locator under aids-dir the fix line is just "cite the primary source" —
// no "<id>" placeholder, since tm src has no concept ID context (§7).
func TestTmSrc_AidsDirRefusalShortForm(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	srcMakeGraph(t, dir)
	srcSetupXDG(t, "")

	// Create aids/ref.txt under the graph directory.
	aidsDir := filepath.Join(dir, "aids")
	if err := os.MkdirAll(aidsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aidsDir, "ref.txt"), []byte("aid content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, errOut, code := run(t, "src", "aids/ref.txt")
	if code != 1 {
		t.Fatalf("want exit 1 for aids-dir refusal, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "cite the primary source") {
		t.Errorf("stderr=%q; want 'cite the primary source'", errOut)
	}
	if strings.Contains(errOut, "<id>") {
		t.Errorf("stderr=%q; must not contain '<id>' placeholder (tm src has no concept id)", errOut)
	}
}
