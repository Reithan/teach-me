package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

// reportFixture builds a two-concept graph in a fresh temp dir:
//
//	base ─"requires"─► dep
//
// Both concepts cite src.txt:1-3.  Sets TM_FILE, TM_SRC_ROOT, TM_PROBE_MIN=1.
// Returns (graphFile, srcDir).
func reportFixture(t *testing.T) (graphFile, srcDir string) {
	t.Helper()
	dir := t.TempDir()
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_PROBE_MIN", "1")
	t.Chdir(dir)

	gfile := filepath.Join(dir, "rep.mmd")
	if _, _, code := run(t, "new", gfile); code != 0 {
		t.Fatalf("new: exit %d", code)
	}
	t.Setenv("TM_FILE", gfile)
	if _, _, code := run(t, "add", "base", "src.txt:1-3", "Base scope"); code != 0 {
		t.Fatalf("add base: exit %d", code)
	}
	if _, _, code := run(t, "add", "dep", "src.txt:1-3", "Dep scope", "--parent", `base:"requires"`); code != 0 {
		t.Fatalf("add dep: exit %d", code)
	}
	return gfile, dir
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestReport_Outline_RaftLifecycleFixture(t *testing.T) {
	// Outline of commit_rules over the raft lifecycle fixture.
	// Expected topo order: replicated_log, leader_election, log_matching, commit_rules.
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupRaftSrcRoot(t)
	raftPath := raftFixture(t)

	out, _, code := run(t, "report", "--file", raftPath, "commit_rules")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}

	// All four headings present in topo order.
	idxRL := strings.Index(out, "## replicated_log:")
	idxLE := strings.Index(out, "## leader_election:")
	idxLM := strings.Index(out, "## log_matching:")
	idxCR := strings.Index(out, "## commit_rules:")
	if idxRL < 0 || idxLE < 0 || idxLM < 0 || idxCR < 0 {
		t.Fatalf("missing headings; got:\n%s", out)
	}
	if !(idxRL < idxLE && idxLE < idxLM && idxLM < idxCR) {
		t.Errorf("topo order wrong: replicated_log=%d leader_election=%d log_matching=%d commit_rules=%d",
			idxRL, idxLE, idxLM, idxCR)
	}

	// State annotations.
	if !strings.Contains(out, "state: passed") {
		t.Errorf("expected 'state: passed' for passed concepts; got:\n%s", out)
	}
	if !strings.Contains(out, "state: blocked") {
		t.Errorf("expected 'state: blocked' for commit_rules; got:\n%s", out)
	}
	if !strings.Contains(out, "state: open") {
		t.Errorf("expected 'state: open' for log_matching; got:\n%s", out)
	}

	// GAP from log_matching.
	if !strings.Contains(out, "GAP: treats index match as sufficient, ignores term") {
		t.Errorf("expected GAP annotation; got:\n%s", out)
	}

	// Fail summary from a2.
	if !strings.Contains(out, "- Says matching index is enough; never mentions term") {
		t.Errorf("expected fail summary; got:\n%s", out)
	}

	// Footnote definitions present.
	if !strings.Contains(out, "[^") {
		t.Errorf("expected footnote references; got:\n%s", out)
	}
}

func TestReport_Hops1_Truncation(t *testing.T) {
	// --hops 1 from commit_rules: includes commit_rules + direct parents
	// (leader_election, log_matching) but NOT replicated_log (2 hops away).
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupRaftSrcRoot(t)
	raftPath := raftFixture(t)

	out, _, code := run(t, "report", "--file", raftPath, "commit_rules", "--hops", "1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}

	if strings.Contains(out, "## replicated_log:") {
		t.Errorf("--hops 1 should exclude replicated_log (2 hops); got:\n%s", out)
	}
	if !strings.Contains(out, "## leader_election:") {
		t.Errorf("--hops 1 should include leader_election; got:\n%s", out)
	}
	if !strings.Contains(out, "## log_matching:") {
		t.Errorf("--hops 1 should include log_matching; got:\n%s", out)
	}
	if !strings.Contains(out, "## commit_rules:") {
		t.Errorf("--hops 1 should include start concept commit_rules; got:\n%s", out)
	}
}

func TestReport_PassedOnly(t *testing.T) {
	// --passed-only should drop open/blocked concepts, keep passed ones.
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupRaftSrcRoot(t)
	raftPath := raftFixture(t)

	out, _, code := run(t, "report", "--file", raftPath, "commit_rules", "--passed-only")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}

	// Passed concepts should appear.
	if !strings.Contains(out, "## replicated_log:") {
		t.Errorf("--passed-only should keep replicated_log; got:\n%s", out)
	}
	if !strings.Contains(out, "## leader_election:") {
		t.Errorf("--passed-only should keep leader_election; got:\n%s", out)
	}

	// Untested concepts should be dropped.
	if strings.Contains(out, "## log_matching:") {
		t.Errorf("--passed-only should drop log_matching (open); got:\n%s", out)
	}
	if strings.Contains(out, "## commit_rules:") {
		t.Errorf("--passed-only should drop commit_rules (blocked); got:\n%s", out)
	}
}

func TestReport_URICiteFootnote(t *testing.T) {
	// A URI citation should render as a Markdown link in the footnote.
	// Write the graph file directly since tm add cannot fetch URI citations.
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_FILE", "")
	t.Chdir(dir)

	// Minimal graph with one concept and a hashed URI citation.
	const mmdContent = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        webconcept["Web concept<br/>3f9a1c2b7e0d@https://example.com/page:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef probe_1,probe_2 stroke:#4aa3ff
    classDef teach_3 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
	gfile := filepath.Join(dir, "uri.mmd")
	if err := os.WriteFile(gfile, []byte(mmdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, code := run(t, "report", "--file", gfile, "webconcept")
	if code != 0 {
		t.Fatalf("exit %d; output=%s", code, out)
	}

	// The footnote should be a Markdown link.
	wantFootnote := "[^1]: [3f9a1c2b7e0d@https://example.com/page:1-5](https://example.com/page)"
	if !strings.Contains(out, wantFootnote) {
		t.Errorf("URI footnote: want %q in:\n%s", wantFootnote, out)
	}
}

func TestReport_Fulltext_RepeatedCitationAndDrift(t *testing.T) {
	// Build a graph where two concepts share a citation.  After building,
	// overwrite the source file so the stored hash no longer matches → drift.
	tempErrlog(t)
	gfile, srcDir := reportFixture(t)

	// Corrupt the source to trigger drift on the stored hash.
	if err := os.WriteFile(filepath.Join(srcDir, "src.txt"),
		[]byte("CHANGED\nCHANGED\nCHANGED\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, code := run(t, "report", "--file", gfile, "dep", "--fulltext")
	if code != 0 {
		t.Fatalf("exit %d; output=%s", code, out)
	}

	// base heading has an anchor.
	if !strings.Contains(out, `<a id="base">`) {
		t.Errorf("expected anchor for base; got:\n%s", out)
	}

	// The base section should have a Source line and a fenced block.
	// tm add stores a hash, so the cite is hash@src.txt:1-3.
	if !strings.Contains(out, "Source:") || !strings.Contains(out, "src.txt:1-3") {
		t.Errorf("expected Source line for base; got:\n%s", out)
	}
	if !strings.Contains(out, "\x60\x60\x60") {
		t.Errorf("expected fenced block in fulltext; got:\n%s", out)
	}

	// The dep section should show the back-link to base.
	if !strings.Contains(out, "see [#base](#base)") {
		t.Errorf("dep should back-link to base; got:\n%s", out)
	}
}

func TestReport_Fulltext_DriftLine(t *testing.T) {
	// Build a passed concept with a hashed citation, then corrupt the source
	// so CheckDrift fires.  Run --fulltext and confirm DRIFT line appears.
	tempErrlog(t)
	gfile, srcDir := driftPassedFixture(t)

	// The fixture concept is now passed.  Run report on it.
	out, _, code := run(t, "report", "--file", gfile, "mycon", "--fulltext")
	if code != 0 {
		t.Fatalf("exit %d; output=%s", code, out)
	}
	// No drift yet — text should be inlined, no DRIFT line.
	if strings.Contains(out, "DRIFT") {
		t.Errorf("no drift yet, unexpected DRIFT line; got:\n%s", out)
	}
	if !strings.Contains(out, "```") {
		t.Errorf("expected fenced block in fulltext; got:\n%s", out)
	}

	// Now corrupt the source to trigger drift.
	if err := os.WriteFile(filepath.Join(srcDir, "src.txt"),
		[]byte("X\nX\nX\nX\nX\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out2, _, code2 := run(t, "report", "--file", gfile, "mycon", "--fulltext")
	if code2 != 0 {
		t.Fatalf("exit %d after drift; output=%s", code2, out2)
	}
	if !strings.Contains(out2, "DRIFT") {
		t.Errorf("expected DRIFT line after source change; got:\n%s", out2)
	}
	// Text still inlined (spec: text is still printed on DRIFT).
	if !strings.Contains(out2, "```") {
		t.Errorf("text should still be inlined on drift; got:\n%s", out2)
	}
}

func TestReport_UnknownConcept_Exit3(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupRaftSrcRoot(t)
	raftPath := raftFixture(t)

	_, _, code := run(t, "report", "--file", raftPath, "nonexistent_concept")
	if code != 3 {
		t.Errorf("want exit 3 for unknown concept, got %d", code)
	}
}

func TestReport_BadHopsValue_Exit3(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupRaftSrcRoot(t)
	raftPath := raftFixture(t)

	_, _, code := run(t, "report", "--file", raftPath, "commit_rules", "--hops", "notanumber")
	if code != 3 {
		t.Errorf("want exit 3 for bad --hops, got %d", code)
	}
}

func TestReport_ForbidGrader(t *testing.T) {
	// tm report must be forbidden for grader role.
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_ROLE", "grader")
	setupRaftSrcRoot(t)
	raftPath := raftFixture(t)

	_, _, code := run(t, "report", "--file", raftPath, "commit_rules")
	if code != 1 {
		t.Errorf("grader should be forbidden from report, want exit 1, got %d", code)
	}
}
