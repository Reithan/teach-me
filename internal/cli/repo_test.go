package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/config"
	"github.com/reithan/teach-me/internal/eventlog"
)

// ── helpers ───────────────────────────────────────────────────────────────────

const repoFileContent = "line1\nline2\nline3\n"

// repoGetBranch returns the current branch name.
func repoGetBranch(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse --abbrev-ref HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestRepo_AddList_Rm verifies the basic add/list/rm lifecycle.
func TestRepo_AddList_Rm(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	dir := t.TempDir()
	repoDir := t.TempDir()

	t.Chdir(dir)

	// `tm repo add` with a valid directory.
	out, errOut, code := run(t, "repo", "add", "myrepo", repoDir)
	if code != 0 {
		t.Fatalf("tm repo add: exit %d; stderr: %s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("tm repo add: want 'ok', got %q", out)
	}

	// `tm repo list` should return one line.
	out, errOut, code = run(t, "repo", "list")
	if code != 0 {
		t.Fatalf("tm repo list: exit %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(out, "myrepo") || !strings.Contains(out, repoDir) {
		t.Errorf("tm repo list: want myrepo and path; got %q", out)
	}

	// `tm repo rm` should remove the alias.
	out, errOut, code = run(t, "repo", "rm", "myrepo")
	if code != 0 {
		t.Fatalf("tm repo rm: exit %d; stderr: %s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("tm repo rm: want 'ok', got %q", out)
	}

	// `tm repo list` should now be empty.
	out, _, code = run(t, "repo", "list")
	if code != 0 {
		t.Fatalf("tm repo list (after rm): exit %d", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("tm repo list after rm: want empty, got %q", out)
	}
}

// TestRepo_AddLocal writes to .tmconfig, not the user config.
func TestRepo_AddLocal(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	repoDir := t.TempDir()

	_, errOut, code := run(t, "repo", "add", "local-alias", repoDir, "--local")
	if code != 0 {
		t.Fatalf("tm repo add --local: exit %d; stderr: %s", code, errOut)
	}
	// .tmconfig should hold the key.
	data, err := os.ReadFile(config.LocalName)
	if err != nil {
		t.Fatalf("read .tmconfig: %v", err)
	}
	if !strings.Contains(string(data), "repo local-alias = "+repoDir) {
		t.Errorf(".tmconfig should hold the repo alias; got:\n%s", data)
	}
	// User config must not be written.
	if _, err := os.Stat(config.UserPath()); !os.IsNotExist(err) {
		t.Errorf("user config must not be written with --local")
	}
}

// TestRepo_AddInvalidAlias exits 3 with the errlog row.
func TestRepo_AddInvalidAlias(t *testing.T) {
	freshConfig(t)
	errlogPath := tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	repoDir := t.TempDir()

	_, errOut, code := run(t, "repo", "add", "bad.alias", repoDir)
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "invalid alias") {
		t.Errorf("want 'invalid alias' in stderr; got %q", errOut)
	}
	rows := readErrlog(t, errlogPath)
	if len(rows) == 0 {
		t.Fatal("expected errlog row")
	}
	if !strings.Contains(rows[0].Err, "invalid alias") {
		t.Errorf("errlog Err = %q; want 'invalid alias'", rows[0].Err)
	}
}

// TestRepo_AddNonExistentDir exits 3.
func TestRepo_AddNonExistentDir(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)

	_, errOut, code := run(t, "repo", "add", "r", "/nonexistent/path/xyz")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "not an existing directory") {
		t.Errorf("want 'not an existing directory' in stderr; got %q", errOut)
	}
}

// TestRepo_ListSorted verifies that list output is sorted by alias.
func TestRepo_ListSorted(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)
	d1, d2, d3 := t.TempDir(), t.TempDir(), t.TempDir()

	// Add in reverse order: z, a, m.
	for _, args := range [][]string{
		{"repo", "add", "z-repo", d3},
		{"repo", "add", "a-repo", d1},
		{"repo", "add", "m-repo", d2},
	} {
		if _, _, code := run(t, args...); code != 0 {
			t.Fatalf("tm %v: exit %d", args, code)
		}
	}

	out, _, code := run(t, "repo", "list")
	if code != 0 {
		t.Fatalf("tm repo list: exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "a-repo") {
		t.Errorf("first line should start with a-repo; got %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "m-repo") {
		t.Errorf("second line should start with m-repo; got %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "z-repo") {
		t.Errorf("third line should start with z-repo; got %q", lines[2])
	}
}

// TestRepo_AddSHARewriting verifies that `tm add` with a git: locator stores
// the SHA-pinned form and logs ref/commit fields. Requires git on PATH.
func TestRepo_AddSHARewriting(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	freshConfig(t)
	tempErrlog(t)

	gitBin, _ := exec.LookPath("git")
	repoDir := t.TempDir()
	fullSHA := srcInitGitRepo(t, repoDir, repoFileContent)
	sha12 := fullSHA[:12]
	branch := repoGetBranch(t, repoDir)

	// Set up XDG config with git + alias.
	srcSetupXDG(t, fmt.Sprintf("git=%s\nrepo r = %s\n", gitBin, repoDir))

	// Create a graph in a temp dir.
	graphDir := t.TempDir()
	t.Chdir(graphDir)
	graphFile := filepath.Join(graphDir, "g.mmd")
	if _, errOut, code := run(t, "new", graphFile); code != 0 {
		t.Fatalf("tm new: exit %d; %s", code, errOut)
	}
	t.Setenv("TM_FILE", graphFile)

	// Add a concept with a git: locator.
	citeArg := "git:r@" + branch + ":file.txt:1-2"
	_, errOut, code := run(t, "add", "c1", citeArg, "scope text")
	if code != 0 {
		t.Fatalf("tm add git: locator: exit %d; stderr: %s", code, errOut)
	}

	// Read the graph and verify the stored citation has the SHA-pinned locator.
	graphData, err := os.ReadFile(graphFile)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	wantLocator := "git:r@" + sha12 + ":file.txt"
	if !strings.Contains(string(graphData), wantLocator) {
		t.Errorf("graph does not contain SHA-pinned locator %q;\ngraph:\n%s", wantLocator, graphData)
	}

	// Check the event log for ref and commit fields.
	logData, err := os.ReadFile(eventlog.Path(graphFile))
	if err != nil {
		t.Fatalf("read eventlog: %v", err)
	}
	var addEvent map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(logData)), "\n") {
		var ev map[string]any
		if jsonErr := json.Unmarshal([]byte(line), &ev); jsonErr != nil {
			continue
		}
		if ev["ev"] == "add" {
			addEvent = ev
			break
		}
	}
	if addEvent == nil {
		t.Fatal("no add event in log")
	}
	if ref, ok := addEvent["ref"].(string); !ok || ref != branch {
		t.Errorf("add event ref = %v, want %q", addEvent["ref"], branch)
	}
	if commit, ok := addEvent["commit"].(string); !ok || commit != sha12 {
		t.Errorf("add event commit = %v, want %q", addEvent["commit"], sha12)
	}
}

// TestRepo_QGitLocator verifies that `tm q` also stores the SHA-pinned form.
func TestRepo_QGitLocator(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	freshConfig(t)
	tempErrlog(t)

	gitBin, _ := exec.LookPath("git")
	repoDir := t.TempDir()
	fullSHA := srcInitGitRepo(t, repoDir, repoFileContent)
	sha12 := fullSHA[:12]
	branch := repoGetBranch(t, repoDir)

	srcSetupXDG(t, fmt.Sprintf("git=%s\nrepo r = %s\n", gitBin, repoDir))

	graphDir := t.TempDir()
	t.Chdir(graphDir)
	graphFile := filepath.Join(graphDir, "g.mmd")
	if _, errOut, code := run(t, "new", graphFile); code != 0 {
		t.Fatalf("tm new: exit %d; %s", code, errOut)
	}
	t.Setenv("TM_FILE", graphFile)

	// Add the concept first (using the same citation style as the test).
	citeArg := "git:r@" + branch + ":file.txt:1-2"
	if _, errOut, code := run(t, "add", "c1", citeArg, "scope"); code != 0 {
		t.Fatalf("tm add: exit %d; %s", code, errOut)
	}

	// Add a question citing a commit form.
	commitCite := "git:r@" + branch + ":file.txt:1-1"
	qOut, errOut, code := run(t, "q", "c1", commitCite, "question scope")
	if code != 0 {
		t.Fatalf("tm q git: locator: exit %d; stderr: %s", code, errOut)
	}
	// qOut should be "q1\n"
	qID := strings.TrimSpace(qOut)
	if !strings.HasPrefix(qID, "q") {
		t.Errorf("tm q: want qN, got %q", qOut)
	}

	// Check the graph stores the SHA-pinned form.
	graphData, err := os.ReadFile(graphFile)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	wantLocator := "git:r@" + sha12 + ":file.txt"
	if !strings.Contains(string(graphData), wantLocator) {
		t.Errorf("graph does not contain SHA-pinned locator %q;\ngraph:\n%s", wantLocator, graphData)
	}
}

// ── error-path coverage ────────────────────────────────────────────────────────

// TestRepo_AddMissingArgs verifies that `tm repo add` with too few arguments
// exits 3 and reports the usage error.
func TestRepo_AddMissingArgs(t *testing.T) {
	freshConfig(t)
	errlogPath := tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)

	_, errOut, code := run(t, "repo", "add")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "repo add requires") {
		t.Errorf("want 'repo add requires' in stderr; got %q", errOut)
	}
	rows := readErrlog(t, errlogPath)
	if len(rows) == 0 {
		t.Fatal("expected errlog row")
	}
	if !strings.Contains(rows[0].Err, "repo add requires") {
		t.Errorf("errlog Err = %q; want 'repo add requires'", rows[0].Err)
	}
}

// TestRepo_RmMissingArg verifies that `tm repo rm` with no alias exits 3.
func TestRepo_RmMissingArg(t *testing.T) {
	freshConfig(t)
	errlogPath := tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)

	_, errOut, code := run(t, "repo", "rm")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "repo rm requires") {
		t.Errorf("want 'repo rm requires' in stderr; got %q", errOut)
	}
	rows := readErrlog(t, errlogPath)
	if len(rows) == 0 {
		t.Fatal("expected errlog row")
	}
	if !strings.Contains(rows[0].Err, "repo rm requires") {
		t.Errorf("errlog Err = %q; want 'repo rm requires'", rows[0].Err)
	}
}

// TestRepo_RmInvalidAlias verifies that `tm repo rm` with a bad alias exits 3.
func TestRepo_RmInvalidAlias(t *testing.T) {
	freshConfig(t)
	errlogPath := tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)

	_, errOut, code := run(t, "repo", "rm", "bad.alias")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "invalid alias") {
		t.Errorf("want 'invalid alias' in stderr; got %q", errOut)
	}
	rows := readErrlog(t, errlogPath)
	if len(rows) == 0 {
		t.Fatal("expected errlog row")
	}
	if !strings.Contains(rows[0].Err, "invalid alias") {
		t.Errorf("errlog Err = %q; want 'invalid alias'", rows[0].Err)
	}
}

// TestRepo_AddLoadConfigFail verifies that `tm repo add` exits 3 when the
// source config is unreadable (a .tmconfig with an invalid repo alias).
func TestRepo_AddLoadConfigFail(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	dir := t.TempDir()
	repoDir := t.TempDir()
	t.Chdir(dir)

	// Write a .tmconfig that has an invalid repo alias to trigger a parse error.
	badCfg := filepath.Join(dir, ".tmconfig")
	if err := os.WriteFile(badCfg, []byte("repo bad.alias = /some/path\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, errOut, code := run(t, "repo", "add", "r", repoDir)
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "source config") {
		t.Errorf("want 'source config' in stderr; got %q", errOut)
	}
}

// TestRepo_ListLoadConfigFail verifies that `tm repo list` exits 3 when the
// source config is unreadable.
func TestRepo_ListLoadConfigFail(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)

	// Write a .tmconfig with an invalid alias to force LoadConfig to error.
	badCfg := filepath.Join(dir, ".tmconfig")
	if err := os.WriteFile(badCfg, []byte("repo bad.alias = /some/path\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, errOut, code := run(t, "repo", "list")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "source config") {
		t.Errorf("want 'source config' in stderr; got %q", errOut)
	}
}

// TestRepo_RmConfigSetFail verifies that `tm repo rm` exits 3 when config.Set
// fails because the user config path is a directory, not a file.
func TestRepo_RmConfigSetFail(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)

	// Build the user config path and create it as a directory so config.Set
	// fails with EISDIR on ReadFile.
	xdg := os.Getenv("XDG_CONFIG_HOME")
	tmDir := filepath.Join(xdg, "tm")
	userConfigPath := filepath.Join(tmDir, "config")
	if err := os.MkdirAll(userConfigPath, 0o755); err != nil {
		t.Fatalf("MkdirAll user config dir: %v", err)
	}
	// os.Stat on the path (a directory) succeeds, so repoRM will call config.Set.

	_, errOut, code := run(t, "repo", "rm", "myrepo")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "cannot write") {
		t.Errorf("want 'cannot write' in stderr; got %q", errOut)
	}
}

// TestRepo_AddNotGitRepo verifies that `tm repo add` exits 3 when the git
// executable is configured but the target directory is not a git repo.
func TestRepo_AddNotGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	freshConfig(t)
	tempErrlog(t)

	gitBin, _ := exec.LookPath("git")
	notGitDir := t.TempDir() // plain directory, not a git repo

	dir := t.TempDir()
	t.Chdir(dir)

	// Set up user config with git configured so the git-repo check runs.
	srcSetupXDG(t, fmt.Sprintf("git=%s\n", gitBin))

	_, errOut, code := run(t, "repo", "add", "r", notGitDir)
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "not a git repo") {
		t.Errorf("want 'not a git repo' in stderr; got %q", errOut)
	}
}

// TestRepo_ListEmpty verifies that `tm repo list` with no repos outputs nothing.
func TestRepo_ListEmpty(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)

	out, _, code := run(t, "repo", "list")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("want empty output for empty repo list, got %q", out)
	}
}
