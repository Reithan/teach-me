package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

const origSrc5 = "line 1\nline 2\nline 3\nline 4\nline 5\n"

// driftSetup creates a temp dir, writes src.txt with origSrc5, sets
// TM_SRC_ROOT, and returns srcPath. The caller sets TM_FILE as needed.
func driftSetup(t *testing.T) (srcPath string) {
	t.Helper()
	dir := t.TempDir()
	srcPath = filepath.Join(dir, "src.txt")
	if err := os.WriteFile(srcPath, []byte(origSrc5), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	return srcPath
}

// ── tm lint --drift ───────────────────────────────────────────────────────────

func TestLintDrift(t *testing.T) {
	type tc struct {
		name     string
		setup    func(t *testing.T) (graphPath string, mutateSrc func())
		wantCode int
		wantOut  string // substring match; use exactOut for TrimSpace equality
		exactOut bool
		wantErr  string // substring in stderr; "" = no check
	}

	tests := []tc{
		{
			name: "clean exit0",
			setup: func(t *testing.T) (string, func()) {
				setupCheckSrcRoot(t)
				t.Setenv("TM_FILE", "")
				return checkProbeFixture(t), nil
			},
			wantCode: 0,
			wantOut:  "ok",
			exactOut: true,
		},
		{
			name: "drifted citation exit1",
			setup: func(t *testing.T) (string, func()) {
				src := driftSetup(t)
				t.Setenv("TM_FILE", "")
				return checkProbeFixture(t), func() {
					if err := os.WriteFile(src, []byte("line 1 modified\nline 2\nline 3\nline 4\nline 5\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			},
			wantCode: 1,
			wantOut:  "DRIFT ",
		},
		{
			name: "read error continues exit1",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				t.Setenv("TM_SRC_ROOT", dir)
				t.Setenv("TM_FILE", "")
				content := "flowchart TB\n" +
					"    subgraph passed[\"Concepts User understands\"]\n    end\n" +
					"    subgraph untested[\"Concepts User has not been tested on\"]\n" +
					"        mycon[\"My concept<br/>f5ca3875b379@missing.txt:1-5\"]\n    end\n" +
					"    subgraph testing[\"Open tests validating and teaching User understanding\"]\n    end\n" +
					"    classDef pending stroke-dasharray:4 3\n"
				p := filepath.Join(dir, "g.mmd")
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				return p, nil
			},
			wantCode: 1,
			wantErr:  "err: ",
		},
		{
			name: "parse error exit3",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				t.Setenv("TM_FILE", "")
				p := filepath.Join(dir, "bad.mmd")
				if err := os.WriteFile(p, []byte("not valid mermaid"), 0o644); err != nil {
					t.Fatal(err)
				}
				return p, nil
			},
			wantCode: 3,
		},
		{
			name: "passed concepts exit0",
			setup: func(t *testing.T) (string, func()) {
				setupRaftSrcRoot(t)
				t.Setenv("TM_FILE", "")
				return raftFixture(t), nil
			},
			wantCode: 0,
			wantOut:  "ok",
			exactOut: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			graphPath, mutateSrc := tc.setup(t)
			if mutateSrc != nil {
				mutateSrc()
			}
			out, errOut, code := run(t, "lint", "--drift", graphPath)
			if code != tc.wantCode {
				t.Fatalf("want exit %d, got %d\nstdout:\n%s\nstderr:\n%s", tc.wantCode, code, out, errOut)
			}
			if tc.wantOut != "" {
				if tc.exactOut {
					if strings.TrimSpace(out) != tc.wantOut {
						t.Errorf("output: want %q, got %q", tc.wantOut, strings.TrimSpace(out))
					}
				} else if !strings.Contains(out, tc.wantOut) {
					t.Errorf("output: want %q; got:\n%s", tc.wantOut, out)
				}
			}
			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("stderr: want %q; got:\n%s", tc.wantErr, errOut)
			}
		})
	}
}

// ── tm rehash ────────────────────────────────────────────────────────────────

func TestRehash(t *testing.T) {
	type tc struct {
		name        string
		setup       func(t *testing.T) (graphPath string)
		wantCode    int
		wantOut     string // substring
		wantErr     string // substring in stderr
		extraAssert func(t *testing.T, graphPath, out string)
	}

	tests := []tc{
		{
			name: "no graph file exit3",
			setup: func(t *testing.T) string {
				t.Setenv("TM_FILE", "")
				t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no pointer
				t.Chdir(t.TempDir())                     // no .tmconfig
				return ""
			},
			wantCode: 3,
		},
		{
			// Unresolvable hashless citations: all errors collected then refused.
			// Covers the hashOne error path and the errs-collection loop.
			name: "unresolvable citations RefusalError exit1",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				t.Setenv("TM_SRC_ROOT", dir) // no src files in dir
				t.Setenv("TM_FILE", "")
				content := "flowchart TB\n" +
					"    subgraph passed[\"Concepts User understands\"]\n    end\n" +
					"    subgraph untested[\"Concepts User has not been tested on\"]\n" +
					"        mycon[\"My concept<br/>missing.txt:1-5\"]\n    end\n" +
					"    subgraph testing[\"Open tests validating and teaching User understanding\"]\n    end\n" +
					"    classDef pending stroke-dasharray:4 3\n"
				p := filepath.Join(dir, "g.mmd")
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantCode: 1,
			wantErr:  "err: ",
		},
		{
			name: "already hashed noop",
			setup: func(t *testing.T) string {
				driftSetup(t)
				t.Setenv("TM_FILE", "")
				return checkProbeFixture(t)
			},
			wantCode: 0,
			wantOut:  "ok",
			extraAssert: func(t *testing.T, graphPath, _ string) {
				before, err := os.ReadFile(graphPath)
				if err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(graphPath)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Error("rehash must not modify a file that is already fully hashed")
				}
			},
		},
		{
			// Legacy graph: untested concept with hashless citation.
			// hash of "line 1\nline 2\nline 3\nline 4\nline 5" = f5ca3875b379
			name: "hashless untested concept",
			setup: func(t *testing.T) string {
				src := driftSetup(t)
				dir := filepath.Dir(src)
				t.Chdir(dir)
				content := "flowchart TB\n" +
					"    subgraph passed[\"Concepts User understands\"]\n    end\n" +
					"    subgraph untested[\"Concepts User has not been tested on\"]\n" +
					"        %% tm:format 2\n" +
					"        mycon[\"My concept<br/>src.txt:1-5\"]\n    end\n" +
					"    subgraph testing[\"Open tests validating and teaching User understanding\"]\n    end\n" +
					"    classDef pending stroke-dasharray:4 3\n"
				p := filepath.Join(dir, "g.mmd")
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("TM_FILE", p)
				return p
			},
			wantCode: 0,
			extraAssert: func(t *testing.T, graphPath, out string) {
				const wantCite = "f5ca3875b379@src.txt:1-5"
				if !strings.Contains(out, "mycon src.txt:1-5 -> "+wantCite) {
					t.Errorf("want change line; got:\n%s", out)
				}
				if !strings.Contains(out, "ok") {
					t.Errorf("want ok in output; got:\n%s", out)
				}
				data, err := os.ReadFile(graphPath)
				if err != nil {
					t.Fatal(err)
				}
				g, err := graph.Parse(data)
				if err != nil {
					t.Fatalf("parse after rehash: %v", err)
				}
				if len(g.UntestedConcepts) == 0 || g.UntestedConcepts[0].Cites[0] != wantCite {
					t.Errorf("concept cite after rehash: want %q", wantCite)
				}
			},
		},
		{
			// Legacy graph: passed concept + question both have hashless citations.
			// hash of lines 1-3 = cd3f27ccd149; hash of lines 1-5 = f5ca3875b379
			name: "passed concept and question",
			setup: func(t *testing.T) string {
				src := driftSetup(t)
				dir := filepath.Dir(src)
				t.Chdir(dir)
				content := "flowchart TB\n" +
					"    subgraph passed[\"Concepts User understands\"]\n" +
					"        pc1[\"Passed concept<br/>src.txt:1-3\"]\n    end\n" +
					"    subgraph untested[\"Concepts User has not been tested on\"]\n" +
					"        %% tm:format 2\n" +
					"        uc1[\"Untested concept\"]\n" +
					"        pc1 --\"enables\"--> uc1\n    end\n" +
					"    subgraph testing[\"Open tests validating and teaching User understanding\"]\n" +
					"        q1[\"Question text<br/>src.txt:1-5\"]:::probe_1\n" +
					"        uc1 --> q1\n    end\n" +
					"    classDef probe_1 stroke:#4aa3ff\n" +
					"    classDef pending stroke-dasharray:4 3\n"
				p := filepath.Join(dir, "g.mmd")
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("TM_FILE", p)
				return p
			},
			wantCode: 0,
			extraAssert: func(t *testing.T, graphPath, out string) {
				const wantPC = "cd3f27ccd149@src.txt:1-3"
				const wantQ = "f5ca3875b379@src.txt:1-5"
				if !strings.Contains(out, "pc1 src.txt:1-3 -> "+wantPC) {
					t.Errorf("want passed concept change line; got:\n%s", out)
				}
				if !strings.Contains(out, "q1 src.txt:1-5 -> "+wantQ) {
					t.Errorf("want question change line; got:\n%s", out)
				}
				if !strings.Contains(out, "ok") {
					t.Errorf("want ok in output; got:\n%s", out)
				}
				data, err := os.ReadFile(graphPath)
				if err != nil {
					t.Fatal(err)
				}
				g, err := graph.Parse(data)
				if err != nil {
					t.Fatalf("parse after rehash: %v", err)
				}
				if len(g.PassedConcepts) == 0 || g.PassedConcepts[0].Cites[0] != wantPC {
					t.Errorf("passed concept cite: want %q", wantPC)
				}
				var foundQ string
				for _, item := range g.TestingItems {
					if item.Q != nil && item.Q.ID == "q1" {
						foundQ = item.Q.Cite
					}
				}
				if foundQ != wantQ {
					t.Errorf("question cite: want %q, got %q", wantQ, foundQ)
				}
			},
		},
		{
			name: "parse error exit3",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				t.Setenv("TM_FILE", "")
				p := filepath.Join(dir, "bad.mmd")
				if err := os.WriteFile(p, []byte("not valid mermaid"), 0o644); err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantCode: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			graphPath := tc.setup(t)
			args := []string{"rehash"}
			if graphPath != "" {
				args = append(args, graphPath)
			}
			out, errOut, code := run(t, args...)
			if code != tc.wantCode {
				t.Fatalf("want exit %d, got %d\nstdout:\n%s\nstderr:\n%s", tc.wantCode, code, out, errOut)
			}
			if tc.wantOut != "" && !strings.Contains(out, tc.wantOut) {
				t.Errorf("output: want %q; got:\n%s", tc.wantOut, out)
			}
			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("stderr: want %q; got:\n%s", tc.wantErr, errOut)
			}
			if tc.extraAssert != nil {
				tc.extraAssert(t, graphPath, out)
			}
		})
	}
}

// ── DRIFT in read commands ────────────────────────────────────────────────────

// TestDRIFT_Reads verifies that ask --src-text and show print a DRIFT line
// when the source file has changed since the citation was hashed.
// (tm check refuses with exit 1 on drift in M10; that is covered by
// TestCheck_DRIFT_HashMismatch.)
func TestDRIFT_Reads(t *testing.T) {
	tests := []struct {
		name    string
		cmdArgs []string
		modSrc  string // full replacement content for src.txt
	}{
		{
			name:    "ask --src-text prints DRIFT",
			cmdArgs: []string{"ask", "--src-text", "mycon"},
			// Modify line 2 so src.txt:2-4 (cited by q2, the unanswered question) drifts.
			modSrc: "line 1\nline 2 modified\nline 3\nline 4\nline 5\n",
		},
		{
			name:    "show concept prints DRIFT",
			cmdArgs: []string{"show", "mycon"},
			modSrc:  "line 1 modified\nline 2\nline 3\nline 4\nline 5\n",
		},
		{
			name:    "show question prints DRIFT",
			cmdArgs: []string{"show", "q1"},
			modSrc:  "line 1 modified\nline 2\nline 3\nline 4\nline 5\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			srcPath := driftSetup(t)
			fixture := checkProbeFixture(t)
			t.Setenv("TM_FILE", fixture)

			// Verify no DRIFT before mutation.
			out, _, code := run(t, tc.cmdArgs...)
			if code != 0 {
				t.Fatalf("before drift: want exit 0, got %d; out:\n%s", code, out)
			}
			if strings.Contains(out, "DRIFT") {
				t.Errorf("no DRIFT expected before mutation; got:\n%s", out)
			}

			// Mutate src.txt so stored hashes no longer match.
			if err := os.WriteFile(srcPath, []byte(tc.modSrc), 0o644); err != nil {
				t.Fatal(err)
			}

			out, errOut, code := run(t, tc.cmdArgs...)
			if code != 0 {
				t.Fatalf("after drift: want exit 0, got %d; stderr:\n%s", code, errOut)
			}
			if !strings.Contains(out, "DRIFT ") {
				t.Errorf("want DRIFT in output after mutation; got:\n%s", out)
			}
		})
	}
}
