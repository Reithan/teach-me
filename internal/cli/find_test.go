package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── tm find ───────────────────────────────────────────────────────────────────

func TestFind_ConceptHit(t *testing.T) {
	// Search for "matching" should hit log_matching concept scope.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "find", "matching")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "log_matching") {
		t.Errorf("want log_matching in output; got:\n%s", out)
	}
}

func TestFind_QuestionHit(t *testing.T) {
	// Search for "prefix" should hit q2 (scope: "Same index and term implies identical prefix").
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "find", "prefix")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "q2") {
		t.Errorf("want q2 in output; got:\n%s", out)
	}
}

func TestFind_AnswerHit(t *testing.T) {
	// Search for "matching index" should hit a2 (label: "Says matching index is enough; never mentions term").
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "find", "matching index")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "a2") {
		t.Errorf("want a2 in output; got:\n%s", out)
	}
}

func TestFind_KindConcept(t *testing.T) {
	// --kind concept restricts output to concepts only.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "find", "log", "--kind", "concept")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "log_matching") {
		t.Errorf("want log_matching in output; got:\n%s", out)
	}
	// Questions must not appear.
	if strings.Contains(out, " q1") || strings.Contains(out, " q2") {
		t.Errorf("questions must not appear with --kind concept; got:\n%s", out)
	}
}

func TestFind_KindQ(t *testing.T) {
	// --kind q restricts to questions only.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "find", "index", "--kind", "q")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	// Should find questions with "index" in scope (q1, q2, q4 all have "index").
	if !strings.Contains(out, "q") {
		t.Errorf("want at least one question in output; got:\n%s", out)
	}
	// Concepts must not appear.
	if strings.Contains(out, "log_matching  ") || strings.Contains(out, "commit_rules  ") {
		t.Errorf("concepts must not appear with --kind q; got:\n%s", out)
	}
	// Answers must not appear (answer IDs start with 'a' followed by digits).
	for _, line := range strings.Split(out, "\n") {
		if len(line) >= 2 && line[0] == 'a' && line[1] >= '0' && line[1] <= '9' {
			t.Errorf("answer line must not appear with --kind q; got line: %q", line)
		}
	}
}

func TestFind_KindA(t *testing.T) {
	// --kind a restricts to answers only.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "find", "stored", "--kind", "a")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	// a1 label: "Matching index and term means the same command is stored".
	if !strings.Contains(out, "a1") {
		t.Errorf("want a1 in output; got:\n%s", out)
	}
	// Concepts and questions must not appear.
	if strings.Contains(out, "log_matching") {
		t.Errorf("concepts must not appear with --kind a; got:\n%s", out)
	}
}

func TestFind_CaseInsensitive(t *testing.T) {
	// Search is case-insensitive.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "find", "LOG MATCHING")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "log_matching") {
		t.Errorf("want case-insensitive hit for log_matching; got:\n%s", out)
	}
}

func TestFind_NoHit_EmptyOutput(t *testing.T) {
	// No matching text → empty output, exit 0.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "find", "xyzzyquuxfrobnicate12345")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("want empty output for no hits; got:\n%s", out)
	}
}

func TestFind_OutputLineFormat(t *testing.T) {
	// Each hit line must use two-space separators: "<id>  <state>  <scope>".
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "find", "log matching", "--kind", "concept")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	// log_matching scope is "Log matching property".
	if !strings.Contains(out, "log_matching  open  Log matching property") {
		t.Errorf("want 'log_matching  open  Log matching property'; got:\n%s", out)
	}
}

func TestFind_TruncatesLongScope(t *testing.T) {
	// Scopes longer than 60 runes must be truncated with "…" (U+2026 ellipsis).
	tempErrlog(t)

	dir := t.TempDir()
	// Construct a scope that is exactly 70 characters long (> 60).
	longScope := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyz01234567"
	// 70 chars

	content := "---\nconfig:\n  look: classic\n---\nflowchart TB\n    subgraph passed[\"Concepts\"]\n    end\n    subgraph untested[\"Untested\"]\n        longcon[\"" + longScope + "<br/>f5ca3875b379@src.txt:1-5\"]\n    end\n    subgraph testing[\"Testing\"]\n    end\n"
	mmdPath := filepath.Join(dir, "long.mmd")
	if err := os.WriteFile(mmdPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	srcPath := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(srcPath, []byte("line1\nline2\nline3\nline4\nline5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_FILE", mmdPath)

	out, errOut, code := run(t, "find", "ABCDEF")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("want a hit, got empty output")
	}
	// The line should contain the ellipsis character since scope > 60 runes.
	if !strings.Contains(out, "…") {
		t.Errorf("want ellipsis in truncated scope; got:\n%s", out)
	}
	// The line must not contain the full scope (truncated at 60).
	if strings.Contains(out, longScope) {
		t.Errorf("scope must be truncated; got full scope in output:\n%s", out)
	}
}

func TestFind_NoFile_Exit3(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Chdir(t.TempDir())

	_, errOut, code := run(t, "find", "anything")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
}

func TestFind_PassedConceptState(t *testing.T) {
	// Passed concepts appear with state "passed".
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "find", "leader election", "--kind", "concept")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	// leader_election is passed; its scope contains "Leader election".
	if !strings.Contains(out, "leader_election  passed  ") {
		t.Errorf("want 'leader_election  passed  ...' ; got:\n%s", out)
	}
}

func TestFind_ReserveConceptState(t *testing.T) {
	// Reserve concepts appear with state "reserve".
	// raft.mmd has log_compaction in the reserve block.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "find", "compaction", "--kind", "concept")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "log_compaction  reserve  ") {
		t.Errorf("want 'log_compaction  reserve  ...' ; got:\n%s", out)
	}
}
