package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

// raftFixtureWithSrc sets up TM_FILE to the raft.mmd fixture and configures
// TM_SRC_ROOT with a raft.txt covering the line ranges used in citations.
func raftFixtureWithSrc(t *testing.T) {
	t.Helper()
	setupRaftSrcRoot(t)
	path := raftFixture(t)
	t.Setenv("TM_FILE", path)
}

// ── tm status (summary) ───────────────────────────────────────────────────────

func TestStatus_Summary_Raft(t *testing.T) {
	// Reproduces §6 sample lines 229-232; raft.mmd has one reserve concept.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "status")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 output lines, got %d:\n%s", len(lines), out)
	}

	want := []string{
		"passed 2  open 1  blocked 1  reserve 1",
		"log_matching  failed 1/2  teach_3 open",
		"commit_rules  blocked by log_matching",
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d: want %q, got %q", i, w, lines[i])
		}
	}
}

func TestStatus_Summary_WithPassed(t *testing.T) {
	// --passed additionally lists passed concepts.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "status", "--passed")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Counts line must still be present (reserve 1 appended since raft.mmd has one).
	if !strings.Contains(out, "passed 2  open 1  blocked 1  reserve 1") {
		t.Errorf("want counts line; got:\n%s", out)
	}
	// Both passed concepts must appear.
	if !strings.Contains(out, "leader_election  passed") {
		t.Errorf("want leader_election  passed in output; got:\n%s", out)
	}
	if !strings.Contains(out, "replicated_log  passed") {
		t.Errorf("want replicated_log  passed in output; got:\n%s", out)
	}
}

func TestStatus_Concept_Reserve(t *testing.T) {
	// Reserve concept prints a single-line "<id>  reserve" summary.
	// raft.mmd has log_compaction in the reserve block.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "status", "--concept", "log_compaction")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 output line for reserve concept, got %d:\n%s", len(lines), out)
	}
	want := "log_compaction  reserve"
	if lines[0] != want {
		t.Errorf("want %q, got %q", want, lines[0])
	}
}

func TestStatus_Summary_NoFile_Exit3(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Chdir(t.TempDir())

	_, errOut, code := run(t, "status")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
}

// ── tm status --concept (passed concept) ─────────────────────────────────────

func TestStatus_Concept_Passed_WithUnblocked(t *testing.T) {
	// replicated_log is passed; its child log_matching is on the frontier.
	// Expected: "replicated_log passed  unblocked log_matching"
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "status", "--concept", "replicated_log")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 output line for passed concept, got %d:\n%s", len(lines), out)
	}
	want := "replicated_log passed  unblocked log_matching"
	if lines[0] != want {
		t.Errorf("want %q, got %q", want, lines[0])
	}
}

func TestStatus_Concept_Passed_NoUnblocked(t *testing.T) {
	// leader_election is passed; its only child commit_rules is blocked (not frontier).
	// Expected: "leader_election passed  unblocked" with nothing after.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "status", "--concept", "leader_election")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 output line, got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "leader_election passed  unblocked") {
		t.Errorf("want prefix 'leader_election passed  unblocked', got %q", lines[0])
	}
	// No extra concept IDs after "unblocked".
	suffix := strings.TrimPrefix(lines[0], "leader_election passed  unblocked")
	if strings.TrimSpace(suffix) != "" {
		t.Errorf("want no unblocked ids for leader_election, got extra: %q", suffix)
	}
}

// ── tm status --concept (open concept) ────────────────────────────────────────

func TestStatus_Concept_Open_LogMatching(t *testing.T) {
	// Reproduces §6 sample lines 234-239 and the chained ask (lines 240-242).
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "status", "--concept", "log_matching")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Verify header line.
	wantHeader := "log_matching  failed 1/2  GAP: treats index match as sufficient, ignores term"
	if !strings.Contains(out, wantHeader) {
		t.Errorf("missing header line %q; got:\n%s", wantHeader, out)
	}

	// Verify batch lines.
	wantLines := []string{
		"  probe_1 resolved  q1 pass  q2 fail",
		"    target q2 | Same index and term implies identical prefix | fbb0469c4812@raft.txt:202-215 | Says matching index is enough; never mentions term",
		"  probe_2 locked  q3 q4",
		"  teach_3 open  q5 pass  q6 -",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("missing line %q; got:\n%s", want, out)
		}
	}

	// Verify chained ask.
	if !strings.Contains(out, "> tm ask log_matching") {
		t.Errorf("missing chain separator; got:\n%s", out)
	}
	if !strings.Contains(out, "teach_3") {
		t.Errorf("missing teach_3 in chain output; got:\n%s", out)
	}
	wantQ6 := "q6 | Why a follower rejects on term mismatch | be8d8fe59060@raft.txt:216-228 | re q2"
	if !strings.Contains(out, wantQ6) {
		t.Errorf("missing %q in chain output; got:\n%s", wantQ6, out)
	}
}

func TestStatus_Concept_Open_LogMatching_ExactChain(t *testing.T) {
	// Verify the chain appears in the right order: detail then chain separator
	// then ask output.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "status", "--concept", "log_matching")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	chainIdx := strings.Index(out, "> tm ask log_matching")
	if chainIdx < 0 {
		t.Fatalf("no chain separator found; got:\n%s", out)
	}
	// Detail should come before chain.
	detailIdx := strings.Index(out, "probe_1 resolved")
	if detailIdx < 0 || detailIdx >= chainIdx {
		t.Errorf("detail must appear before chain; chain at %d, detail at %d", chainIdx, detailIdx)
	}
	// teach_3 and q6 should appear after chain separator.
	afterChain := out[chainIdx:]
	if !strings.Contains(afterChain, "teach_3\n") {
		t.Errorf("teach_3 must be first line after chain separator; got:\n%s", afterChain)
	}
}

func TestStatus_Concept_Unknown_Exit3(t *testing.T) {
	tempErrlog(t)
	raftFixtureWithSrc(t)

	_, errOut, code := run(t, "status", "--concept", "nonexistent_xyz")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, `err: unknown concept "nonexistent_xyz"`) {
		t.Errorf("want unknown concept err; got:\n%s", errOut)
	}
}

func TestStatus_Concept_Blocked_NoBatches(t *testing.T) {
	// commit_rules is blocked and has no batches.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "status", "--concept", "commit_rules")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	// Should print failed 0/0 header with no chain.
	if !strings.Contains(out, "commit_rules  failed 0/0") {
		t.Errorf("want failed 0/0 header; got:\n%s", out)
	}
	if strings.Contains(out, "> tm ask") {
		t.Errorf("no chain expected for blocked concept; got:\n%s", out)
	}
}

// ── New small fixture: passed concept test (belt-and-suspenders) ──────────────

// passedConceptFixture creates a minimal .mmd in a temp dir where concept "alpha"
// is passed and "beta" is its only child on the frontier. Returns the path.
func passedConceptFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	content := `---
config:
  look: classic
---
flowchart TB
    subgraph passed["Concepts User understands"]
        alpha["Alpha concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        beta["Beta concept<br/>6aa0757910fd@src.txt:6-10"]
        alpha --"requires"--> beta
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
`
	mmdPath := filepath.Join(dir, "mini.mmd")
	if err := os.WriteFile(mmdPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a src.txt so citations resolve.
	srcContent := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_FILE", mmdPath)
	return mmdPath
}

func TestStatus_Concept_Passed_SmallFixture(t *testing.T) {
	// alpha is passed; beta is its only untested child on the frontier.
	tempErrlog(t)
	passedConceptFixture(t)

	out, errOut, code := run(t, "status", "--concept", "alpha")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 output line, got %d:\n%s", len(lines), out)
	}
	want := "alpha passed  unblocked beta"
	if lines[0] != want {
		t.Errorf("want %q, got %q", want, lines[0])
	}
}
