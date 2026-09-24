package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/cli"
	"github.com/reithan/teach-me/internal/config"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/lint"
)

// ── tm new tests ──────────────────────────────────────────────────────────────

// TestNew_Creates verifies the full happy path for tm new: the created file
// round-trips parse/write unchanged, passes lint, the user config is written
// with file=, exactly one "new" event lands in the event log, and stdout is "ok".
func TestNew_Creates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_SRC_ROOT", dir)

	file := filepath.Join(dir, "test.mmd")
	out, errOut, code := run(t, "new", file)
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want stdout 'ok', got %q", out)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("cannot read created file: %v", err)
	}

	// (a) round-trip: parse then write produces identical bytes.
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse created file: %v", parseErr)
	}
	roundTripped := graph.Write(g)
	if !bytes.Equal(data, roundTripped) {
		t.Errorf("round-trip mismatch:\ncreated:\n%s\nre-written:\n%s", data, roundTripped)
	}

	// (b) lint passes.
	viols := lint.Check(data, lint.Config{
		SrcRoot: dir, ProbeMin: 2, ProbeMax: 5, TeachMin: 1, TeachMax: 3,
	})
	if len(viols) > 0 {
		t.Errorf("skeleton fails lint: %v", viols)
	}

	// (c) user config written with file= key; no .tmconfig in the cwd.
	cfgData, cfgErr := os.ReadFile(config.UserPath())
	if cfgErr != nil {
		t.Fatalf("cannot read user config: %v", cfgErr)
	}
	if !strings.Contains(string(cfgData), "file = "+file) {
		t.Errorf("user config does not contain expected file= key; got:\n%s", cfgData)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".tmconfig")); !os.IsNotExist(statErr) {
		t.Errorf(".tmconfig must not be written without --local")
	}

	// (d) exactly one "new" event in the event log.
	logPath := file + ".jsonl"
	logData, logErr := os.ReadFile(logPath)
	if logErr != nil {
		t.Fatalf("cannot read event log: %v", logErr)
	}
	logLines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	if len(logLines) != 1 {
		t.Fatalf("want 1 event log line, got %d", len(logLines))
	}
	var ev map[string]any
	if err := json.Unmarshal([]byte(logLines[0]), &ev); err != nil {
		t.Fatalf("parse event log line: %v", err)
	}
	if ev["ev"] != "new" {
		t.Errorf("event ev: want 'new', got %v", ev["ev"])
	}
	if ev["file"] != file {
		t.Errorf("event file: want %q, got %v", file, ev["file"])
	}
}

// TestNew_AlreadyExists verifies that `tm new <file>` on an existing path:
//   - exits 1,
//   - emits "err: <file> already exists" and "fix: tm load <file>",
//   - does NOT overwrite the existing file,
//   - does NOT write .tmconfig.
func TestNew_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")

	file := filepath.Join(dir, "exists.mmd")
	const existing = "existing content\n"
	if err := os.WriteFile(file, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	_, errOut, code := run(t, "new", file)
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: "+file+" already exists") {
		t.Errorf("want 'already exists' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm load "+file) {
		t.Errorf("want 'fix: tm load <file>'; got:\n%s", errOut)
	}

	// File must not be overwritten.
	content, _ := os.ReadFile(file)
	if string(content) != existing {
		t.Errorf("file was overwritten; got:\n%s", content)
	}

	// No .tmconfig written.
	if _, statErr := os.Stat(filepath.Join(dir, ".tmconfig")); !os.IsNotExist(statErr) {
		t.Errorf(".tmconfig must not be written on conflict")
	}
}

// TestNew_WithTitle verifies that --title inserts the title before config:
// and that the resulting file still passes lint.
func TestNew_WithTitle(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_SRC_ROOT", dir)

	file := filepath.Join(dir, "titled.mmd")
	_, errOut, code := run(t, "new", file, "--title", "Raft consensus")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("cannot read created file: %v", err)
	}

	// Title must appear as a plain scalar (no quoting needed here).
	if !strings.Contains(string(data), "title: Raft consensus\n") {
		t.Errorf("title not found in frontmatter; got:\n%s", data)
	}
	// config: must follow the title line.
	if !strings.Contains(string(data), "title: Raft consensus\nconfig:") {
		t.Errorf("title must immediately precede config:; got:\n%s", data)
	}

	// Still passes lint.
	viols := lint.Check(data, lint.Config{
		SrcRoot: dir, ProbeMin: 2, ProbeMax: 5, TeachMin: 1, TeachMax: 3,
	})
	if len(viols) > 0 {
		t.Errorf("titled skeleton fails lint: %v", viols)
	}
}

// TestNew_WithTitle_RequiresQuoting verifies that a title containing ':' is
// wrapped in double quotes in the YAML frontmatter.
func TestNew_WithTitle_RequiresQuoting(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")

	file := filepath.Join(dir, "colontest.mmd")
	_, errOut, code := run(t, "new", file, "--title", "Key: Value")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("cannot read created file: %v", err)
	}
	// Colon in title must be double-quoted.
	if !strings.Contains(string(data), `title: "Key: Value"`) {
		t.Errorf("colon title not double-quoted; got:\n%s", data)
	}
}

// ── tm load tests ─────────────────────────────────────────────────────────────

// TestLoad_ValidGraph verifies that `tm load <file>`:
//   - writes the user config with file= key,
//   - appends one "load" event to the event log,
//   - stdout byte-equals `tm status --file <file>`.
func TestLoad_ValidGraph(t *testing.T) {
	// Resolve raftFixture before Chdir; the helper uses filepath.Abs relative to cwd.
	raftSrc := raftFixture(t)

	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")

	// Copy raft.mmd to a temp dir so the event log is not written into testdata/.
	raftData, err := os.ReadFile(raftSrc)
	if err != nil {
		t.Fatal(err)
	}
	raftCopy := filepath.Join(dir, "raft.mmd")
	if err := os.WriteFile(raftCopy, raftData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Provide raft.txt so citation checks in status pass.
	if err := os.WriteFile(filepath.Join(dir, "raft.txt"), makeLines(300), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	// Run load; capture its output.
	outLoad, errOut, code := run(t, "load", raftCopy)
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// The user config must be written with file= key.
	cfgData, cfgErr := os.ReadFile(config.UserPath())
	if cfgErr != nil {
		t.Fatalf("cannot read user config: %v", cfgErr)
	}
	if !strings.Contains(string(cfgData), "file = "+raftCopy) {
		t.Errorf("user config missing expected file= key; got:\n%s", cfgData)
	}

	// Exactly one "load" event must appear in the event log.
	logPath := raftCopy + ".jsonl"
	logData, logErr := os.ReadFile(logPath)
	if logErr != nil {
		t.Fatalf("cannot read event log: %v", logErr)
	}
	logLines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	if len(logLines) != 1 {
		t.Fatalf("want 1 event log line, got %d", len(logLines))
	}
	var ev map[string]any
	if err := json.Unmarshal([]byte(logLines[0]), &ev); err != nil {
		t.Fatalf("parse event log line: %v", err)
	}
	if ev["ev"] != "load" {
		t.Errorf("event ev: want 'load', got %v", ev["ev"])
	}
	if ev["file"] != raftCopy {
		t.Errorf("event file: want %q, got %v", raftCopy, ev["file"])
	}

	// Output must byte-equal `tm status --file <raftCopy>`.
	var statusOut bytes.Buffer
	cli.RunWithWriters([]string{"status", "--file", raftCopy}, &statusOut, &bytes.Buffer{})
	if outLoad != statusOut.String() {
		t.Errorf("load output does not byte-equal status output:\nload:\n%s\nstatus:\n%s",
			outLoad, statusOut.String())
	}
}

// TestLoad_MissingFile verifies that `tm load <nonexistent>` exits 3 with an
// error message and does NOT write .tmconfig.
func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")

	missing := filepath.Join(dir, "nonexistent.mmd")
	_, errOut, code := run(t, "load", missing)
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: cannot load") {
		t.Errorf("want 'cannot load' err; got:\n%s", errOut)
	}

	// No .tmconfig written on failure.
	if _, statErr := os.Stat(filepath.Join(dir, ".tmconfig")); !os.IsNotExist(statErr) {
		t.Errorf(".tmconfig must not be written when load fails")
	}
}
