package errlog_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/version"
)

// fixedClock returns a fixed UTC time for deterministic tests.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

var (
	testTime  = time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	testClock = fixedClock{t: testTime}
)

func TestPath_EnvOverride(t *testing.T) {
	t.Setenv("TM_ERRORS", "/custom/errors.jsonl")
	got := errlog.Path("/some/dir")
	if got != "/custom/errors.jsonl" {
		t.Errorf("Path() = %q, want /custom/errors.jsonl", got)
	}
}

func TestPath_DefaultInDir(t *testing.T) {
	t.Setenv("TM_ERRORS", "")
	got := errlog.Path("/my/graph")
	want := filepath.Join("/my/graph", "ERRORS.jsonl")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPath_EmptyDirUsesDot(t *testing.T) {
	t.Setenv("TM_ERRORS", "")
	got := errlog.Path("")
	want := filepath.Join(".", "ERRORS.jsonl")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestAppend_FieldOrderAndNullHandling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ERRORS.jsonl")
	log := errlog.New(path, testClock)

	log.Append(errlog.Row{
		Role: nil,
		File: nil,
		Argv: []string{"tm", "load", "foo.mmd"},
		Exit: 2,
		Err:  "foo.mmd: file not found",
		Fix:  nil,
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	line := strings.TrimRight(string(data), "\n")

	// Verify field order by checking raw JSON positions.
	fields := []string{`"t":`, `"v":`, `"role":`, `"file":`, `"argv":`, `"exit":`, `"err":`, `"fix":`}
	prev := 0
	for _, f := range fields {
		pos := strings.Index(line, f)
		if pos < prev {
			t.Errorf("field %q out of order in JSON: %s", f, line)
		}
		prev = pos
	}

	// Null fields
	if !strings.Contains(line, `"role":null`) {
		t.Errorf("expected role:null, got: %s", line)
	}
	if !strings.Contains(line, `"file":null`) {
		t.Errorf("expected file:null, got: %s", line)
	}
	if !strings.Contains(line, `"fix":null`) {
		t.Errorf("expected fix:null, got: %s", line)
	}
	// violations absent for non-lint row
	if strings.Contains(line, `"violations"`) {
		t.Errorf("expected violations absent, got: %s", line)
	}

	// t and v values
	wantT := testTime.Format(time.RFC3339)
	if !strings.Contains(line, `"t":"`+wantT+`"`) {
		t.Errorf("expected t=%q in: %s", wantT, line)
	}
	wantV := version.Version()
	if !strings.Contains(line, `"v":"`+wantV+`"`) {
		t.Errorf("expected v=%q in: %s", wantV, line)
	}
}

func TestAppend_GoldenLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ERRORS.jsonl")
	log := errlog.New(path, testClock)

	role := "teacher"
	file := "/graph/state.mmd"
	fix := "tm load <file>"
	log.Append(errlog.Row{
		Role: &role,
		File: &file,
		Argv: []string{"tm", "load"},
		Exit: 1,
		Err:  "no graph loaded",
		Fix:  &fix,
	})

	data, _ := os.ReadFile(path)
	line := strings.TrimRight(string(data), "\n")

	// Round-trip parse to verify all fields present and correct.
	var row struct {
		T    string   `json:"t"`
		V    string   `json:"v"`
		Role *string  `json:"role"`
		File *string  `json:"file"`
		Argv []string `json:"argv"`
		Exit int      `json:"exit"`
		Err  string   `json:"err"`
		Fix  *string  `json:"fix"`
	}
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if row.Role == nil || *row.Role != "teacher" {
		t.Errorf("role = %v, want 'teacher'", row.Role)
	}
	if row.File == nil || *row.File != file {
		t.Errorf("file = %v, want %q", row.File, file)
	}
	if row.Fix == nil || *row.Fix != fix {
		t.Errorf("fix = %v, want %q", row.Fix, fix)
	}
}

func TestAppend_ViolationsPresentForLint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ERRORS.jsonl")
	log := errlog.New(path, testClock)

	log.Append(errlog.Row{
		Argv:       []string{"tm", "lint", "foo.mmd"},
		Exit:       2,
		Err:        "lint failed",
		Violations: []string{"line 3: bad node", "line 7: missing edge"},
	})

	data, _ := os.ReadFile(path)
	line := strings.TrimRight(string(data), "\n")

	if !strings.Contains(line, `"violations":`) {
		t.Errorf("expected violations present, got: %s", line)
	}
	if !strings.Contains(line, "bad node") {
		t.Errorf("expected violation text, got: %s", line)
	}
}

func TestAppend_TwoAppendsTwoLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ERRORS.jsonl")
	log := errlog.New(path, testClock)

	log.Append(errlog.Row{Argv: []string{"tm", "a"}, Exit: 1, Err: "err1"})
	log.Append(errlog.Row{Argv: []string{"tm", "b"}, Exit: 2, Err: "err2"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), string(data))
	}
	if !strings.Contains(lines[0], "err1") {
		t.Errorf("line 0 missing err1: %s", lines[0])
	}
	if !strings.Contains(lines[1], "err2") {
		t.Errorf("line 1 missing err2: %s", lines[1])
	}
}

func TestAppend_ExistingContentPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ERRORS.jsonl")

	// Write existing content.
	existing := `{"existing":true}` + "\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	log := errlog.New(path, testClock)
	log.Append(errlog.Row{Argv: []string{"tm"}, Exit: 1, Err: "new"})

	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), existing) {
		t.Errorf("existing content not preserved: %q", string(data))
	}
	if !strings.Contains(string(data), "new") {
		t.Errorf("new content not appended: %q", string(data))
	}
}

func TestAppend_SilentFailureUnwritablePath(t *testing.T) {
	dir := t.TempDir()
	// Make the "parent" a file rather than a directory, so Open fails.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	impossiblePath := filepath.Join(blocker, "ERRORS.jsonl")

	log := errlog.New(impossiblePath, testClock)
	// Must not panic.
	log.Append(errlog.Row{Argv: []string{"tm"}, Exit: 1, Err: "test"})
}

func TestAppend_SilentFailureReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	roDir := filepath.Join(dir, "ro")
	if err := os.Mkdir(roDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(roDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

	// In some environments (e.g. running as root) read-only dirs are writable.
	// We skip rather than fail in that case.
	testPath := filepath.Join(roDir, "ERRORS.jsonl")
	f, err := os.OpenFile(testPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		_ = f.Close()
		t.Skip("running as root or OS allows write to chmod 555 dir; skip")
	}

	log := errlog.New(testPath, testClock)
	// Must not panic.
	log.Append(errlog.Row{Argv: []string{"tm"}, Exit: 1, Err: "test"})
}

func TestRealClock_IsUTC(t *testing.T) {
	now := errlog.RealClock.Now()
	if now.Location() != time.UTC {
		t.Errorf("RealClock.Now() not UTC: %v", now.Location())
	}
}
