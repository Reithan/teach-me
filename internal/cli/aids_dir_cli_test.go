package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aidsSetupDir creates a temp dir, writes a src file and an aids file, sets
// TM_SRC_ROOT and TM_PROBE_MIN, creates a fresh graph, and returns the dir.
func aidsSetupDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// regular source file
	if err := os.WriteFile(filepath.Join(dir, "src.txt"),
		[]byte("line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	// aids dir and file
	aidsDir := filepath.Join(dir, "aids")
	if err := os.MkdirAll(aidsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aidsDir, "ref.txt"),
		[]byte("aid content\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_PROBE_MIN", "1")
	return dir
}

// TestAidsDir_AddRefusesInsideAidsDir verifies that tm add refuses a citation
// that resolves inside the default aids directory with exit 1.
func TestAidsDir_AddRefusesInsideAidsDir(t *testing.T) {
	dir := aidsSetupDir(t)
	_ = newGraph(t, dir)

	_, errOut, code := run(t, "add", "mycon", "aids/ref.txt:1-1", "scope")
	if code != 1 {
		t.Errorf("tm add with aids citation: want exit 1, got %d", code)
	}
	if !strings.Contains(errOut, "is an aid, not a source") {
		t.Errorf("want 'is an aid, not a source' in stderr; got %q", errOut)
	}
	if !strings.Contains(errOut, "tm aid") {
		t.Errorf("want 'tm aid' in fix text; got %q", errOut)
	}
}

// TestAidsDir_AddSiblingPrefixAllowed verifies that tm add accepts a citation
// inside aids2/ (a sibling-prefix directory) without a refusal.
func TestAidsDir_AddSiblingPrefixAllowed(t *testing.T) {
	dir := aidsSetupDir(t)
	// Create aids2/src.txt as a real source file.
	aids2 := filepath.Join(dir, "aids2")
	if err := os.MkdirAll(aids2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aids2, "src.txt"),
		[]byte("line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	_ = newGraph(t, dir)

	_, errOut, code := run(t, "add", "mycon", "aids2/src.txt:1-5", "scope")
	if code != 0 {
		t.Errorf("tm add with aids2/ citation: want exit 0, got %d; stderr: %s", code, errOut)
	}
}

// TestAidsDir_QRefusesInsideAidsDir verifies that tm q refuses a citation that
// resolves inside the default aids directory with exit 1.
func TestAidsDir_QRefusesInsideAidsDir(t *testing.T) {
	dir := aidsSetupDir(t)
	_ = newGraph(t, dir)
	_, _, c1 := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "scope")
	if c1 != 0 {
		t.Fatalf("setup: tm add failed with exit %d", c1)
	}

	_, errOut, code := run(t, "q", "mycon", "aids/ref.txt:1-1", "question scope")
	if code != 1 {
		t.Errorf("tm q with aids citation: want exit 1, got %d", code)
	}
	if !strings.Contains(errOut, "is an aid, not a source") {
		t.Errorf("want 'is an aid, not a source' in stderr; got %q", errOut)
	}
}

// TestAidsDir_ReciteRefusesInsideAidsDir verifies that tm recite refuses when
// the new range resolves inside the default aids directory.
func TestAidsDir_ReciteRefusesInsideAidsDir(t *testing.T) {
	dir := aidsSetupDir(t)
	_ = newGraph(t, dir)
	_, _, c1 := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "scope")
	if c1 != 0 {
		t.Fatalf("setup: tm add failed with exit %d", c1)
	}

	_, errOut, code := run(t, "recite", "mycon", "aids/ref.txt:1-1")
	if code == 0 {
		t.Errorf("tm recite with aids citation: want non-zero exit, got 0")
	}
	if !strings.Contains(errOut, "is an aid") {
		t.Errorf("want 'is an aid' in stderr; got %q", errOut)
	}
}

// TestAidsDir_LintCheck16Reports verifies that tm lint reports a check 16
// violation when a concept cites a file inside the default aids directory.
func TestAidsDir_LintCheck16Reports(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_PROBE_MIN", "1")
	// Write an aids file so the path exists.
	aidsDir := filepath.Join(dir, "aids")
	if err := os.MkdirAll(aidsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aidsDir, "ref.txt"), []byte("aid\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a graph file directly that cites an aids path.
	// Using an unhashed citation so we don't need to pre-compute the hash.
	graphPath := filepath.Join(dir, "g.mmd")
	graphSrc := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        mycon["my concept<br/>aids/ref.txt:1-1"]
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
	if err := os.WriteFile(graphPath, []byte(graphSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_FILE", graphPath)

	out, _, _ := run(t, "lint", graphPath)
	if !strings.Contains(out, "cites aid") {
		t.Errorf("tm lint: want check 16 'cites aid' violation; got:\n%s", out)
	}
	if !strings.Contains(out, "tm aid") {
		t.Errorf("tm lint: want 'tm aid' in violation message; got:\n%s", out)
	}
}

// TestAidsDir_LintSiblingPrefixClean verifies that tm lint does NOT report a
// check 16 violation for a citation to aids2/ (sibling-prefix directory).
func TestAidsDir_LintSiblingPrefixClean(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_PROBE_MIN", "1")
	// Create aids2/ref.txt as a real source file.
	aids2 := filepath.Join(dir, "aids2")
	if err := os.MkdirAll(aids2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aids2, "ref.txt"), []byte("source\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	graphPath := filepath.Join(dir, "g.mmd")
	graphSrc := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        mycon["my concept<br/>aids2/ref.txt:1-1"]
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
	if err := os.WriteFile(graphPath, []byte(graphSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_FILE", graphPath)

	out, _, _ := run(t, "lint", graphPath)
	if strings.Contains(out, "cites aid") {
		t.Errorf("tm lint: unexpected check 16 violation for aids2/ sibling prefix; got:\n%s", out)
	}
}
