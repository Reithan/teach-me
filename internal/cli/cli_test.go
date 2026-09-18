package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/cli"
	"github.com/reithan/teach-me/internal/docver"
	"github.com/reithan/teach-me/internal/version"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

// run is a test helper that invokes RunWithWriters and returns the captured
// stdout, stderr, and exit code.
func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = cli.RunWithWriters(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

// errlogRow is the minimal subset of errlog.Row fields we assert on.
type errlogRow struct {
	V          string   `json:"v"`
	Role       *string  `json:"role"`
	File       *string  `json:"file"`
	Argv       []string `json:"argv"`
	Exit       int      `json:"exit"`
	Err        string   `json:"err"`
	Fix        *string  `json:"fix"`
	Violations []string `json:"violations,omitempty"`
}

// readErrlog reads and JSON-decodes all lines in the ERRORS.jsonl file at path.
func readErrlog(t *testing.T, path string) []errlogRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read errlog: %v", err)
	}
	var rows []errlogRow
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var r errlogRow
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("parse errlog row %q: %v", line, err)
		}
		rows = append(rows, r)
	}
	return rows
}

// tempErrlog creates a temp dir, sets TM_ERRORS to a path inside it, and
// returns the path. Cleanup restores TM_ERRORS via t.Setenv.
func tempErrlog(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ERRORS.jsonl")
	t.Setenv("TM_ERRORS", path)
	return path
}

// ── Baseline help tests ───────────────────────────────────────────────────────

func TestBaselineHelp_NoTMDOC_NoArgs(t *testing.T) {
	t.Setenv("TM_DOC", "")
	out, _, code := run(t)
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	// Should contain usage lines for known commands.
	for _, name := range []string{"lint", "status", "grade", "check", "show"} {
		if !strings.Contains(out, "tm "+name) {
			t.Errorf("baseline help missing usage for %q; got:\n%s", name, out)
		}
	}
}

func TestBaselineHelp_NoTMDOC_HelpFlag(t *testing.T) {
	t.Setenv("TM_DOC", "")
	out, _, code := run(t, "--help")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(out, "tm lint") {
		t.Errorf("expected usage lines, got:\n%s", out)
	}
}

func TestBaselineHelp_NoTMDOC_ShortHelp(t *testing.T) {
	t.Setenv("TM_DOC", "")
	out, _, code := run(t, "-h")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(out, "tm lint") {
		t.Errorf("expected usage lines, got:\n%s", out)
	}
}

func TestBaselineHelp_WithTMDOC_VersionMatch(t *testing.T) {
	dir := t.TempDir()
	docPath := filepath.Join(dir, "SKILL.md")
	marker := docver.Marker()
	// Write a fake skill file with matching marker.
	content := fmt.Sprintf("---\nmetadata:\n  tm-version: %q\n---\n# skill\n", marker)
	if err := os.WriteFile(docPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_DOC", docPath)

	out, _, code := run(t, "--help")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	want := fmt.Sprintf("see %s (tm %s)", docPath, version.Version())
	if !strings.Contains(out, want) {
		t.Errorf("want %q in output; got:\n%s", want, out)
	}
	if strings.Contains(out, "err:") {
		t.Errorf("no err line expected for matching version; got:\n%s", out)
	}
}

func TestBaselineHelp_WithTMDOC_VersionMismatch(t *testing.T) {
	tempErrlog(t) // prevent ERRORS.jsonl pollution in the test working dir
	dir := t.TempDir()
	docPath := filepath.Join(dir, "SKILL.md")
	marker := docver.Marker()
	// Write a fake skill file with a DIFFERENT version.
	wrongVer := "99.99"
	content := fmt.Sprintf("---\nmetadata:\n  tm-version: %q\n---\n# skill\n", wrongVer)
	if err := os.WriteFile(docPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_DOC", docPath)

	out, _, code := run(t, "--help")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	wantErr := fmt.Sprintf("err: %s is for tm %s, this is tm %s", docPath, wrongVer, marker)
	if !strings.Contains(out, wantErr) {
		t.Errorf("want %q in output; got:\n%s", wantErr, out)
	}
}

func TestBaselineHelp_WithTMDOC_MissingMarker(t *testing.T) {
	tempErrlog(t) // prevent ERRORS.jsonl pollution in the test working dir
	dir := t.TempDir()
	docPath := filepath.Join(dir, "SKILL.md")
	// Write a file with NO tm-version in frontmatter.
	content := "---\nmetadata:\n  other-key: value\n---\n# skill\n"
	if err := os.WriteFile(docPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_DOC", docPath)
	marker := docver.Marker()

	out, _, code := run(t, "--help")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	wantErr := fmt.Sprintf("err: %s is for tm ?, this is tm %s", docPath, marker)
	if !strings.Contains(out, wantErr) {
		t.Errorf("want %q in output; got:\n%s", wantErr, out)
	}
}

// ── Docver mismatch logging tests (§10.1) ─────────────────────────────────────

// writeMismatchDoc writes a TM_DOC file whose tm-version differs from the
// CLI's marker, sets TM_DOC, and returns the expected err text (without the
// "err: " prefix) that the CLI will print and log.
func writeMismatchDoc(t *testing.T, wrongVer string) (wantErrText string) {
	t.Helper()
	dir := t.TempDir()
	docPath := filepath.Join(dir, "SKILL.md")
	content := fmt.Sprintf("---\nmetadata:\n  tm-version: %q\n---\n# skill\n", wrongVer)
	if err := os.WriteFile(docPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_DOC", docPath)
	marker := docver.Marker()
	return fmt.Sprintf("%s is for tm %s, this is tm %s", docPath, wrongVer, marker)
}

func TestDocverMismatch_Logging_VersionDiff(t *testing.T) {
	// `tm --help` with a drifted TM_DOC: exit 0, stdout has see+err lines,
	// ERRORS.jsonl gets exactly ONE row with exit 0 and err = mismatch text,
	// file = null (no graph resolved in baseline help).
	errlogPath := tempErrlog(t)
	wantErrText := writeMismatchDoc(t, "99.99")

	out, _, code := run(t, "--help")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(out, "err: "+wantErrText) {
		t.Errorf("want err line in stdout; got:\n%s", out)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 errlog row, got %d", len(rows))
	}
	r := rows[0]
	if r.Exit != 0 {
		t.Errorf("row exit: want 0, got %d", r.Exit)
	}
	if r.Err != wantErrText {
		t.Errorf("row err: want %q, got %q", wantErrText, r.Err)
	}
	if r.File != nil {
		t.Errorf("row file: want null, got %v", *r.File)
	}
	if r.Fix != nil {
		t.Errorf("row fix: want null, got %v", *r.Fix)
	}
	if len(r.Argv) < 1 || r.Argv[0] != "tm" {
		t.Errorf("row argv: want [tm ...], got %v", r.Argv)
	}
}

func TestDocverMismatch_Logging_MissingMarker(t *testing.T) {
	// `tm --help` with a TM_DOC missing tm-version: exit 0, err uses "?"
	// sentinel, exactly one row in ERRORS.jsonl.
	errlogPath := tempErrlog(t)
	dir := t.TempDir()
	docPath := filepath.Join(dir, "SKILL.md")
	content := "---\nmetadata:\n  other-key: value\n---\n# skill\n"
	if err := os.WriteFile(docPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_DOC", docPath)
	marker := docver.Marker()
	wantErrText := fmt.Sprintf("%s is for tm ?, this is tm %s", docPath, marker)

	out, _, code := run(t, "--help")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(out, "err: "+wantErrText) {
		t.Errorf("want err line in stdout; got:\n%s", out)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 errlog row, got %d", len(rows))
	}
	r := rows[0]
	if r.Exit != 0 {
		t.Errorf("row exit: want 0, got %d", r.Exit)
	}
	if r.Err != wantErrText {
		t.Errorf("row err: want %q, got %q", wantErrText, r.Err)
	}
	if r.File != nil {
		t.Errorf("row file: want null, got %v", *r.File)
	}
}

func TestDocverMismatch_Logging_UnknownSubcommand(t *testing.T) {
	// Unknown subcommand with a drifted TM_DOC: exit 3, ERRORS.jsonl gets
	// exactly TWO rows — the unknown-command row and the mismatch row, both
	// with exit 3 (§10.1: each err: line produces its own row).
	errlogPath := tempErrlog(t)
	wantMismatch := writeMismatchDoc(t, "99.99")

	_, _, code := run(t, "frobnicate")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 2 {
		t.Fatalf("want exactly 2 errlog rows (unknown-cmd + mismatch), got %d", len(rows))
	}

	// Both rows must carry exit 3.
	for i, r := range rows {
		if r.Exit != 3 {
			t.Errorf("row %d exit: want 3, got %d", i, r.Exit)
		}
	}

	// One row must be the unknown-command row.
	var hasUnknown, hasMismatch bool
	for _, r := range rows {
		if strings.Contains(r.Err, "unknown command") {
			hasUnknown = true
		}
		if r.Err == wantMismatch {
			hasMismatch = true
		}
	}
	if !hasUnknown {
		t.Errorf("want an unknown-command row; rows: %v", rows)
	}
	if !hasMismatch {
		t.Errorf("want a mismatch row with err=%q; rows: %v", wantMismatch, rows)
	}
}

func TestDocverMismatch_NoLog_VersionMatch(t *testing.T) {
	// Matching TM_DOC: no err line in stdout, no ERRORS.jsonl row created.
	errlogPath := tempErrlog(t)
	dir := t.TempDir()
	docPath := filepath.Join(dir, "SKILL.md")
	marker := docver.Marker()
	content := fmt.Sprintf("---\nmetadata:\n  tm-version: %q\n---\n# skill\n", marker)
	if err := os.WriteFile(docPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_DOC", docPath)

	out, _, code := run(t, "--help")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if strings.Contains(out, "err:") {
		t.Errorf("no err line expected for matching version; got:\n%s", out)
	}

	// No ERRORS.jsonl file should be created.
	if _, err := os.Stat(errlogPath); !os.IsNotExist(err) {
		t.Errorf("no errlog row expected for matching version; file exists: %v", err)
	}
}

// ── Specific help tests ───────────────────────────────────────────────────────

func TestSpecificHelp_HelpThenCommand(t *testing.T) {
	t.Setenv("TM_DOC", "")
	out, _, code := run(t, "--help", "lint")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(out, "tm lint") {
		t.Errorf("want lint usage; got:\n%s", out)
	}
	// Should be exactly one line (the usage), not the full table.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Errorf("want exactly 1 line for specific help; got %d: %q", len(lines), out)
	}
}

func TestSpecificHelp_CommandThenHelp(t *testing.T) {
	t.Setenv("TM_DOC", "")
	out, _, code := run(t, "lint", "--help")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(out, "tm lint") {
		t.Errorf("want lint usage; got:\n%s", out)
	}
}

func TestSpecificHelp_CommandFlagHelp(t *testing.T) {
	t.Setenv("TM_DOC", "")
	// ask has --format flag; test <command> --help <flag>
	out, _, code := run(t, "ask", "--help", "--format")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(out, "--format") {
		t.Errorf("want --format in flag help; got:\n%s", out)
	}
}

func TestSpecificHelp_HelpCommandFlag(t *testing.T) {
	t.Setenv("TM_DOC", "")
	// --help ask --format
	out, _, code := run(t, "--help", "ask", "--format")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(out, "--format") {
		t.Errorf("want --format in flag help; got:\n%s", out)
	}
}

func TestSpecificHelp_UnknownFlag(t *testing.T) {
	t.Setenv("TM_DOC", "")
	out, _, code := run(t, "lint", "--help", "--nonexistent")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(out, "unknown flag") {
		t.Errorf("want 'unknown flag' message; got:\n%s", out)
	}
}

// ── Version tests ─────────────────────────────────────────────────────────────

func TestVersion_DoubleDash(t *testing.T) {
	out, _, code := run(t, "--version")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if strings.TrimSpace(out) != version.Version() {
		t.Errorf("want %q, got %q", version.Version(), strings.TrimSpace(out))
	}
}

func TestVersion_SingleDash(t *testing.T) {
	out, _, code := run(t, "-version")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if strings.TrimSpace(out) != version.Version() {
		t.Errorf("want %q, got %q", version.Version(), strings.TrimSpace(out))
	}
}

func TestVersion_NoDash(t *testing.T) {
	out, _, code := run(t, "version")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if strings.TrimSpace(out) != version.Version() {
		t.Errorf("want %q, got %q", version.Version(), strings.TrimSpace(out))
	}
}

// ── Unknown command tests ─────────────────────────────────────────────────────

func TestUnknownCommand_Exit3(t *testing.T) {
	t.Setenv("TM_DOC", "")
	errlogPath := tempErrlog(t)

	_, errOut, code := run(t, "frobnicate")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, `err: unknown command "frobnicate"`) {
		t.Errorf("want err: unknown command in stderr; got:\n%s", errOut)
	}

	// Verify errlog row.
	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
	r := rows[0]
	if r.Exit != 3 {
		t.Errorf("errlog row: want exit 3, got %d", r.Exit)
	}
	if !strings.Contains(r.Err, "unknown command") {
		t.Errorf("errlog row: want 'unknown command' in Err, got %q", r.Err)
	}
	if len(r.Argv) < 2 || r.Argv[0] != "tm" {
		t.Errorf("errlog row: want argv starting with 'tm', got %v", r.Argv)
	}
}

func TestUnknownCommand_ShowsBaselineHelp(t *testing.T) {
	t.Setenv("TM_DOC", "")
	tempErrlog(t)
	out, _, _ := run(t, "frobnicate")
	// Baseline help (the command list) should appear on stdout.
	if !strings.Contains(out, "tm lint") {
		t.Errorf("want baseline help on stdout for unknown command; got:\n%s", out)
	}
}

// ── Nil-Run placeholder tests ─────────────────────────────────────────────────

func TestNilRunPlaceholder_Exit3(t *testing.T) {
	errlogPath := tempErrlog(t)

	// "status" has nil Run in this build.
	_, errOut, code := run(t, "status")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err: status is not implemented in this build") {
		t.Errorf("want 'not implemented' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm status") {
		t.Errorf("want 'fix:' with usage; got:\n%s", errOut)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
	if rows[0].Exit != 3 {
		t.Errorf("want exit 3 in row, got %d", rows[0].Exit)
	}
}

func TestNilRunPlaceholder_GradeNotImplemented(t *testing.T) {
	// grade has nil Run but is also exempt from grader ban.
	// With no TM_ROLE set, the nil-Run placeholder should fire.
	t.Setenv("TM_ROLE", "")
	tempErrlog(t)

	// grade needs 3 positionals; give them to get past parse.
	_, errOut, code := run(t, "grade", "q1", "pass", "summary")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "grade is not implemented in this build") {
		t.Errorf("want grade not implemented; got:\n%s", errOut)
	}
}

// ── Role guard tests ──────────────────────────────────────────────────────────

func TestRoleGuard_GraderForbidden_Status(t *testing.T) {
	t.Setenv("TM_ROLE", "grader")
	errlogPath := tempErrlog(t)

	_, errOut, code := run(t, "status")
	if code != 1 {
		t.Fatalf("want exit 1 (invariant refusal), got %d", code)
	}
	if !strings.Contains(errOut, "err: status is not available when TM_ROLE=grader") {
		t.Errorf("want role refusal err; got:\n%s", errOut)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
	r := rows[0]
	if r.Exit != 1 {
		t.Errorf("want exit 1 in errlog row, got %d", r.Exit)
	}
	if r.Role == nil || *r.Role != "grader" {
		t.Errorf("want role='grader' in errlog row, got %v", r.Role)
	}
}

func TestRoleGuard_TeacherForbidden_Grade(t *testing.T) {
	t.Setenv("TM_ROLE", "teacher")
	errlogPath := tempErrlog(t)

	_, errOut, code := run(t, "grade", "q1", "pass", "summary")
	if code != 1 {
		t.Fatalf("want exit 1 (invariant refusal), got %d", code)
	}
	if !strings.Contains(errOut, "err: grade is not available when TM_ROLE=teacher") {
		t.Errorf("want teacher role refusal err; got:\n%s", errOut)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
	r := rows[0]
	if r.Exit != 1 {
		t.Errorf("want exit 1 in errlog row, got %d", r.Exit)
	}
}

func TestRoleGuard_GraderAllowed_Show(t *testing.T) {
	// show is exempt from the grader ban; it has nil Run, so exits 3 (not
	// implemented), but the role guard must NOT fire for grader.
	t.Setenv("TM_ROLE", "grader")
	tempErrlog(t)

	_, errOut, code := run(t, "show", "some_id")
	// Role guard passes; nil-Run fires → exit 3 "not implemented".
	if code != 3 {
		t.Fatalf("want exit 3 (not implemented, not role refusal), got %d", code)
	}
	if strings.Contains(errOut, "TM_ROLE") {
		t.Errorf("role guard should NOT fire for 'show' with grader; got:\n%s", errOut)
	}
}

func TestRoleGuard_GraderAllowed_Check(t *testing.T) {
	t.Setenv("TM_ROLE", "grader")
	tempErrlog(t)

	_, errOut, code := run(t, "check", "q1")
	if code != 3 {
		t.Fatalf("want exit 3 (not implemented), got %d", code)
	}
	if strings.Contains(errOut, "TM_ROLE") {
		t.Errorf("role guard should NOT fire for 'check' with grader; got:\n%s", errOut)
	}
}

func TestRoleGuard_GraderAllowed_Grade(t *testing.T) {
	t.Setenv("TM_ROLE", "grader")
	tempErrlog(t)

	_, errOut, code := run(t, "grade", "q1", "pass", "summary")
	if code != 3 {
		t.Fatalf("want exit 3 (not implemented), got %d", code)
	}
	if strings.Contains(errOut, "TM_ROLE") {
		t.Errorf("role guard should NOT fire for 'grade' with grader; got:\n%s", errOut)
	}
}

// ── Lint handler tests ────────────────────────────────────────────────────────

// makeLines generates n numbered text lines for use as a citation source file.
func makeLines(n int) []byte {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return []byte(b.String())
}

// setupRaftSrcRoot creates a temp dir with raft.txt (≥300 lines) and sets
// TM_SRC_ROOT so raft.mmd citations resolve correctly.
func setupRaftSrcRoot(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "raft.txt"), makeLines(300), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
}

func TestLint_Success(t *testing.T) {
	errlogPath := tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupRaftSrcRoot(t)

	// raft.mmd is the clean fixture used throughout the codebase.
	raftPath, err := filepath.Abs(filepath.Join("..", "..", "testdata", "raft.mmd"))
	if err != nil {
		t.Fatal(err)
	}

	out, errOut, code := run(t, "lint", raftPath)
	if code != 0 {
		t.Fatalf("lint clean file: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("lint success: want 'ok', got %q", out)
	}

	// No errlog row on success.
	if _, err := os.Stat(errlogPath); !os.IsNotExist(err) {
		t.Errorf("no errlog row expected on lint success; file exists: %v", err)
	}
}

func TestLint_Success_ViaTMFILE(t *testing.T) {
	setupRaftSrcRoot(t)

	raftPath := filepath.Join("..", "..", "testdata", "raft.mmd")
	// Resolve to absolute path for TM_FILE.
	abs, err := filepath.Abs(raftPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_FILE", abs)

	out, _, code := run(t, "lint")
	if code != 0 {
		t.Fatalf("lint via TM_FILE: want exit 0, got %d", code)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}
}

func TestLint_Violations_Exit2(t *testing.T) {
	errlogPath := tempErrlog(t)
	t.Setenv("TM_FILE", "")

	// Write a minimal file that fails lint (not a valid flowchart).
	dir := t.TempDir()
	badFile := filepath.Join(dir, "bad.mmd")
	if err := os.WriteFile(badFile, []byte("not a valid mermaid file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, code := run(t, "lint", badFile)
	if code != 2 {
		t.Fatalf("lint violations: want exit 2, got %d", code)
	}
	// At least one violation printed to stdout.
	if strings.TrimSpace(out) == "" {
		t.Errorf("want violations on stdout, got empty output")
	}

	// Verify errlog row has Violations populated.
	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
	r := rows[0]
	if r.Exit != 2 {
		t.Errorf("want exit 2 in errlog row, got %d", r.Exit)
	}
	if len(r.Violations) == 0 {
		t.Errorf("want violations in errlog row, got none")
	}
	if r.File == nil || *r.File != badFile {
		t.Errorf("want file %q in errlog row, got %v", badFile, r.File)
	}
	if len(r.Argv) < 2 || r.Argv[0] != "tm" || r.Argv[1] != "lint" {
		t.Errorf("want argv=[tm lint ...], got %v", r.Argv)
	}
}

func TestLint_NoFile_Exit3(t *testing.T) {
	errlogPath := tempErrlog(t)
	t.Setenv("TM_FILE", "")
	// Change to an empty temp dir so there is no .tmconfig to pick up.
	t.Chdir(t.TempDir())

	_, errOut, code := run(t, "lint")
	if code != 3 {
		t.Fatalf("lint no file: want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err: no graph file") {
		t.Errorf("want 'err: no graph file'; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm lint <file>") {
		t.Errorf("want fix line; got:\n%s", errOut)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
	if rows[0].Exit != 3 {
		t.Errorf("want exit 3 in errlog row, got %d", rows[0].Exit)
	}
}

func TestLint_MissingFile_Exit3(t *testing.T) {
	errlogPath := tempErrlog(t)
	t.Setenv("TM_FILE", "")

	_, errOut, code := run(t, "lint", "/nonexistent/path/to/file.mmd")
	if code != 3 {
		t.Fatalf("lint missing file: want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err: cannot read") {
		t.Errorf("want 'err: cannot read'; got:\n%s", errOut)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
}

// ── Parse error tests ─────────────────────────────────────────────────────────

func TestParseError_UnknownFlag(t *testing.T) {
	errlogPath := tempErrlog(t)

	_, errOut, code := run(t, "lint", "--verbose")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err: unknown flag --verbose") {
		t.Errorf("want 'unknown flag' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm lint") {
		t.Errorf("want fix line with usage; got:\n%s", errOut)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
	r := rows[0]
	if r.Exit != 3 {
		t.Errorf("want exit 3 in errlog row, got %d", r.Exit)
	}
}

func TestParseError_MissingRequiredArg(t *testing.T) {
	errlogPath := tempErrlog(t)

	// 'drop' requires <concept>; call with no args.
	t.Setenv("TM_ROLE", "")
	_, errOut, code := run(t, "drop")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err: missing argument") {
		t.Errorf("want 'missing argument' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm drop") {
		t.Errorf("want fix line with 'drop' usage; got:\n%s", errOut)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
}

func TestParseError_TooManyArgs(t *testing.T) {
	errlogPath := tempErrlog(t)
	t.Setenv("TM_ROLE", "")

	// 'drop' takes exactly 1 positional; give it 2.
	_, errOut, code := run(t, "drop", "concept1", "extra")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err: unexpected argument") {
		t.Errorf("want 'unexpected argument' err; got:\n%s", errOut)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
}

func TestParseError_BadEnumFlag(t *testing.T) {
	errlogPath := tempErrlog(t)
	t.Setenv("TM_ROLE", "")

	// 'find' has --kind with values: concept|q|a.
	_, errOut, code := run(t, "find", "text", "--kind", "invalid")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err: --kind must be one of: concept|q|a") {
		t.Errorf("want enum error; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm find") {
		t.Errorf("want fix line; got:\n%s", errOut)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
}

func TestParseError_BadEnumPositional(t *testing.T) {
	errlogPath := tempErrlog(t)
	t.Setenv("TM_ROLE", "")

	// 'grade' has pass|fail|unclear as second positional.
	_, errOut, code := run(t, "grade", "q1", "maybe", "summary")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want 1 errlog row, got %d", len(rows))
	}
}

// ── Usage line generation tests ───────────────────────────────────────────────

func TestUsageLines(t *testing.T) {
	cases := []struct {
		name    string
		wantSub string
	}{
		{"lint", "tm lint [<file>]"},
		{"grade", "tm grade <qid> pass|fail|unclear"},
		{"ask", "tm ask <concept> [--format lines|json]"},
		{"q", "tm q <concept> <cite>"},
		{"status", "tm status [--passed] [--concept <id>]"},
		{"add", "tm add <id> <cite>"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cmd := cli.FindCommand(tc.name)
			if cmd == nil {
				t.Fatalf("command %q not found in table", tc.name)
			}
			usage := cmd.Usage()
			if !strings.Contains(usage, tc.wantSub) {
				t.Errorf("command %q usage = %q; want substring %q", tc.name, usage, tc.wantSub)
			}
		})
	}
}

// ── Errlog row completeness test ──────────────────────────────────────────────

func TestErrlogRow_Completeness_Lint(t *testing.T) {
	errlogPath := tempErrlog(t)
	t.Setenv("TM_ROLE", "grader-tester")
	t.Setenv("TM_FILE", "")

	// Write a file that fails lint.
	dir := t.TempDir()
	badFile := filepath.Join(dir, "fail.mmd")
	if err := os.WriteFile(badFile, []byte("not mermaid\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, code := run(t, "lint", badFile)
	if code != 2 {
		t.Fatalf("want exit 2, got %d", code)
	}

	rows := readErrlog(t, errlogPath)
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 row, got %d", len(rows))
	}
	r := rows[0]

	if r.V == "" {
		t.Error("V (version) must be non-empty")
	}
	if r.Exit != 2 {
		t.Errorf("Exit: want 2, got %d", r.Exit)
	}
	if r.Role == nil || *r.Role != "grader-tester" {
		t.Errorf("Role: want 'grader-tester', got %v", r.Role)
	}
	if r.File == nil {
		t.Error("File must not be nil for a resolved lint file")
	}
	if len(r.Argv) == 0 || r.Argv[0] != "tm" {
		t.Errorf("Argv must start with 'tm', got %v", r.Argv)
	}
	if len(r.Violations) == 0 {
		t.Error("Violations must be non-empty for lint exit 2")
	}
}
