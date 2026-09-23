package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// ── tm lint --drift tests ─────────────────────────────────────────────────────

// TestLintDrift_NoDrift_Exit0 verifies exit 0 and "ok" when all citations match
// the current file content.
func TestLintDrift_NoDrift_Exit0(t *testing.T) {
	tempErrlog(t)
	setupCheckSrcRoot(t)
	fixture := checkProbeFixture(t)
	t.Setenv("TM_FILE", "")

	out, errOut, code := run(t, "lint", "--drift", fixture)
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}
}

// TestLintDrift_DriftedCitation_Exit1 verifies exit 1 and DRIFT lines when the
// source file has changed since the citation was hashed.
func TestLintDrift_DriftedCitation_Exit1(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_FILE", "")

	// Write original src.txt content so the hash in check_probe.mmd is correct.
	originalContent := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	srcPath := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(srcPath, []byte(originalContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	// Verify no drift before modifying the file.
	fixture := checkProbeFixture(t)
	out, _, code := run(t, "lint", "--drift", fixture)
	if code != 0 {
		t.Fatalf("pre-drift: want exit 0, got %d; output:\n%s", code, out)
	}

	// Modify src.txt so the stored hash no longer matches.
	modifiedContent := "line 1 modified\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(srcPath, []byte(modifiedContent), 0o644); err != nil {
		t.Fatal(err)
	}

	out, errOut, code := run(t, "lint", "--drift", fixture)
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	// check_probe.mmd has citations for mycon (src.txt:1-5), q1 (src.txt:1-3),
	// q2 (src.txt:2-4). All three include line 1, so all three should DRIFT.
	if !strings.Contains(out, "DRIFT ") {
		t.Errorf("want DRIFT lines in output; got:\n%s", out)
	}
}

// ── tm rehash tests ───────────────────────────────────────────────────────────

// TestRehash_AllAlreadyHashed_NoOp verifies exit 0 and "ok" when all citations
// are already hashed and no rewrite is needed.
func TestRehash_AllAlreadyHashed_NoOp(t *testing.T) {
	tempErrlog(t)
	setupCheckSrcRoot(t)
	fixture := checkProbeFixture(t)
	t.Setenv("TM_FILE", "")

	originalBytes, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}

	out, errOut, code := run(t, "rehash", fixture)
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// File must not be rewritten on a no-op.
	afterBytes, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if string(originalBytes) != string(afterBytes) {
		t.Error("rehash should not modify a file that already has all citations hashed")
	}
}

// TestRehash_HashlessToHashed verifies that tm rehash rewrites hashless
// citations to the hashed form and exits 0.
func TestRehash_HashlessToHashed(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)

	// src.txt with 10 lines (same as setupSrcFile).
	srcContent := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	// Write a graph with a hashless citation. We write directly (bypassing
	// ops.Mutate) to simulate a legacy graph created before M9.
	graphContent := `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pending stroke-dasharray:4 3
`
	graphPath := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(graphPath, []byte(graphContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_FILE", graphPath)

	out, errOut, code := run(t, "rehash", graphPath)
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Verify the graph now has a hashed citation.
	data, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse after rehash: %v", parseErr)
	}
	if len(g.UntestedConcepts) == 0 {
		t.Fatal("no untested concepts after rehash")
	}
	mycon := g.UntestedConcepts[0]
	if len(mycon.Cites) != 1 {
		t.Fatalf("want 1 cite, got %d", len(mycon.Cites))
	}
	// The hash for "line 1\nline 2\nline 3\nline 4\nline 5" (lines 1-5) is f5ca3875b379.
	wantCite := "f5ca3875b379@src.txt:1-5"
	if mycon.Cites[0] != wantCite {
		t.Errorf("want cite %q, got %q", wantCite, mycon.Cites[0])
	}
}

// ── DRIFT in tm check output ──────────────────────────────────────────────────

// TestCheck_DRIFT_HashMismatch verifies that tm check prints a DRIFT line when
// the source file has changed since the citation was hashed.
func TestCheck_DRIFT_HashMismatch(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()

	// Write original src.txt so check_probe.mmd hashes are valid.
	originalContent := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	srcPath := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(srcPath, []byte(originalContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	fixture := checkProbeFixture(t)
	t.Setenv("TM_FILE", fixture)

	// Add a pending answer so check has a q1 to check.
	// check_probe.mmd already has a1:::pending under q1.
	// Get the first question's answer to check.
	out, _, code := run(t, "check", "q1")
	if code != 0 {
		t.Fatalf("check before drift: want exit 0, got %d; output:\n%s", code, out)
	}
	if strings.Contains(out, "DRIFT") {
		t.Errorf("no drift expected before file modification; got:\n%s", out)
	}

	// Modify src.txt so the stored hash no longer matches.
	modifiedContent := "line 1 modified\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(srcPath, []byte(modifiedContent), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, code = run(t, "check", "q1")
	if code != 0 {
		t.Fatalf("check after drift: want exit 0 (no exit-status change in M9), got %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, "DRIFT ") {
		t.Errorf("want DRIFT line in output after file modification; got:\n%s", out)
	}
}

// ── additional rehash error paths ────────────────────────────────────────────

// TestRehash_NoFile_Exit3 verifies exit 3 when no graph file can be resolved.
func TestRehash_NoFile_Exit3(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")

	_, errOut, code := run(t, "rehash")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
}

// TestRehash_ParseError_Exit3 verifies exit 3 when the graph file is not valid Mermaid.
func TestRehash_ParseError_Exit3(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_FILE", "")

	bad := filepath.Join(dir, "bad.mmd")
	if err := os.WriteFile(bad, []byte("this is not valid mermaid content at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, errOut, code := run(t, "rehash", bad)
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
}

// TestRehash_HashlessPassedAndQuestion verifies that tm rehash hashes citations
// in PassedConcepts (lines 88-99) and TestingItems.Q (lines 129-131).
func TestRehash_HashlessPassedAndQuestion(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)

	srcContent := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	// Legacy graph: passed concept + question both have hashless citations.
	graphContent := `flowchart TB
    subgraph passed["Concepts User understands"]
        pc1["Passed concept<br/>src.txt:1-3"]
    end
    subgraph untested["Concepts User has not been tested on"]
        uc1["Untested concept"]
        pc1 --> uc1
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["Question text<br/>src.txt:1-5"]:::probe_1
        uc1 --> q1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pending stroke-dasharray:4 3
`
	graphPath := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(graphPath, []byte(graphContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_FILE", graphPath)

	out, errOut, code := run(t, "rehash", graphPath)
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	data, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse after rehash: %v", parseErr)
	}

	// Passed concept must now have a hashed citation.
	if len(g.PassedConcepts) == 0 {
		t.Fatal("no passed concepts after rehash")
	}
	// src.txt:1-3 → "line 1\nline 2\nline 3" → cd3f27ccd149
	wantPassedCite := "cd3f27ccd149@src.txt:1-3"
	if got := g.PassedConcepts[0].Cites[0]; got != wantPassedCite {
		t.Errorf("passed concept cite: want %q, got %q", wantPassedCite, got)
	}

	// Question must now have a hashed citation.
	// src.txt:1-5 → "line 1\nline 2\nline 3\nline 4\nline 5" → f5ca3875b379
	wantQCite := "f5ca3875b379@src.txt:1-5"
	var foundQCite string
	for _, item := range g.TestingItems {
		if item.Q != nil && item.Q.ID == "q1" {
			foundQCite = item.Q.Cite
			break
		}
	}
	if foundQCite != wantQCite {
		t.Errorf("question cite: want %q, got %q", wantQCite, foundQCite)
	}
}

// ── additional lint --drift error paths ──────────────────────────────────────

// TestLintDrift_ParseError_Exit3 verifies exit 3 when --drift is passed a file
// that is not valid Mermaid (covers lint.go lines 97-101).
func TestLintDrift_ParseError_Exit3(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_FILE", "")

	bad := filepath.Join(dir, "bad.mmd")
	if err := os.WriteFile(bad, []byte("this is not valid mermaid"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, errOut, code := run(t, "lint", "--drift", bad)
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
}

// TestLintDrift_CitationFileNotFound_Exit3 verifies exit 3 when a hashed
// citation points to a file that does not exist under the src root
// (covers lint.go lines 123-127: CheckDrift error path in lintDrift).
func TestLintDrift_CitationFileNotFound_Exit3(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_FILE", "")
	// Point TM_SRC_ROOT to a directory that has no source files.
	t.Setenv("TM_SRC_ROOT", dir)

	// Write a graph with a hashed citation to a file that does not exist.
	graphContent := `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>f5ca3875b379@missing.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pending stroke-dasharray:4 3
`
	graphPath := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(graphPath, []byte(graphContent), 0o644); err != nil {
		t.Fatal(err)
	}

	_, errOut, code := run(t, "lint", "--drift", graphPath)
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
}

// TestLintDrift_WithPassedConcepts verifies that citations in PassedConcepts
// are checked for drift (lint.go lines 109-110). Uses raft.mmd which has
// hashed citations in the passed subgraph.
func TestLintDrift_WithPassedConcepts(t *testing.T) {
	tempErrlog(t)
	setupRaftSrcRoot(t)
	raft := raftFixture(t)
	t.Setenv("TM_FILE", "")

	out, errOut, code := run(t, "lint", "--drift", raft)
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}
}

// ── DRIFT in tm ask --src-text ────────────────────────────────────────────────

// TestAsk_DRIFT_SrcText verifies that tm ask --src-text prints DRIFT when the
// source file has changed since the citation was hashed (ask.go lines 287-288).
func TestAsk_DRIFT_SrcText(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()

	// Write original src.txt so check_probe.mmd hashes are valid.
	originalContent := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(originalContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	fixture := checkProbeFixture(t)
	t.Setenv("TM_FILE", fixture)

	// Verify no drift before modifying the file.
	out, errOut, code := run(t, "ask", "--src-text", "mycon")
	if code != 0 {
		t.Fatalf("ask before drift: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.Contains(out, "DRIFT") {
		t.Errorf("no drift expected before file modification; got:\n%s", out)
	}

	// Modify line 2 so src.txt:2-4 (cited by q2, the unanswered question) drifts.
	modifiedContent := "line 1\nline 2 modified\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(modifiedContent), 0o644); err != nil {
		t.Fatal(err)
	}

	out, errOut, code = run(t, "ask", "--src-text", "mycon")
	if code != 0 {
		t.Fatalf("ask after drift: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "DRIFT ") {
		t.Errorf("want DRIFT line after file modification; got:\n%s", out)
	}
}

// ── DRIFT in tm show ──────────────────────────────────────────────────────────

// TestShow_DRIFT_ConceptAndQuestion verifies that tm show prints DRIFT for a
// concept (show.go lines 183-184) and a question (show.go lines 260-261).
func TestShow_DRIFT_ConceptAndQuestion(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()

	// Write original src.txt matching check_probe.mmd hashes.
	originalContent := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(originalContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	fixture := checkProbeFixture(t)
	t.Setenv("TM_FILE", fixture)

	// Verify no drift before modifying the file.
	out, errOut, code := run(t, "show", "mycon")
	if code != 0 {
		t.Fatalf("show mycon before drift: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.Contains(out, "DRIFT") {
		t.Errorf("no drift expected before file modification; got:\n%s", out)
	}

	// Modify src.txt so stored hashes no longer match.
	modifiedContent := "line 1 modified\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(modifiedContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// tm show mycon → concept DRIFT (show.go lines 183-184).
	out, errOut, code = run(t, "show", "mycon")
	if code != 0 {
		t.Fatalf("show mycon after drift: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "DRIFT ") {
		t.Errorf("want DRIFT line for concept; got:\n%s", out)
	}

	// tm show q1 → question DRIFT (show.go lines 260-261).
	out, errOut, code = run(t, "show", "q1")
	if code != 0 {
		t.Fatalf("show q1 after drift: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "DRIFT ") {
		t.Errorf("want DRIFT line for question; got:\n%s", out)
	}
}
