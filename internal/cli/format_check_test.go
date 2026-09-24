package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// format3Graph is a minimal graph claiming format 3, above CurrentFormat.
const format3Graph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 3
    end
    subgraph reserve["Concepts held in reserve"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// writeFormatGraph writes content to <dir>/g.mmd and returns the path.
func writeFormatGraph(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	return p
}

// TestFormatCheck_CentralRefusal verifies that commands without SkipFormatCheck
// refuse format-1 and format-3 graphs and accept format-2.
func TestFormatCheck_CentralRefusal(t *testing.T) {
	tests := []struct {
		name     string
		graph    string
		args     []string
		wantExit int
		wantErr  string
		wantFix  string
	}{
		{
			name:     "status refuses format-1 with migrate fix",
			graph:    minimalFormat1Graph,
			args:     []string{"status"},
			wantExit: 1,
			wantErr:  "g.mmd is format 1, this is tm format 2",
			wantFix:  "tm migrate",
		},
		{
			name:     "status refuses format-3 with upgrade fix",
			graph:    format3Graph,
			args:     []string{"status"},
			wantExit: 1,
			wantErr:  "g.mmd is format 3, this is tm format 2",
			wantFix:  "upgrade tm",
		},
		{
			name:     "status accepts format-2",
			graph:    minimalFormat2Graph,
			args:     []string{"status"},
			wantExit: 0,
		},
		{
			name:     "add refuses format-1",
			graph:    minimalFormat1Graph,
			args:     []string{"add", "mycon", "src.txt:1-2", "My concept"},
			wantExit: 1,
			wantErr:  "g.mmd is format 1, this is tm format 2",
			wantFix:  "tm migrate",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			errlogPath := tempErrlog(t)
			gPath := writeFormatGraph(t, dir, tc.graph)
			t.Setenv("TM_FILE", gPath)

			_, errOut, code := run(t, tc.args...)

			if code != tc.wantExit {
				t.Fatalf("exit: want %d, got %d; stderr:\n%s", tc.wantExit, code, errOut)
			}
			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("err: want %q; got:\n%s", tc.wantErr, errOut)
			}
			if tc.wantFix != "" && !strings.Contains(errOut, tc.wantFix) {
				t.Errorf("fix: want %q; got:\n%s", tc.wantFix, errOut)
			}
			// Every refusal must write an errlog row.
			if tc.wantExit == 1 && tc.wantErr != "" {
				rows := readErrlog(t, errlogPath)
				if len(rows) == 0 {
					t.Errorf("expected errlog row; got none")
				} else if !strings.Contains(rows[0].Err, tc.wantErr) {
					t.Errorf("errlog err: want %q; got %q", tc.wantErr, rows[0].Err)
				}
			}
		})
	}
}

// TestFormatCheck_Load verifies that load refuses format-1 and format-3 graph
// files passed as a positional argument, and that the fix line names the exact
// positional file path so it cannot be confused with the central check's fix.
func TestFormatCheck_Load(t *testing.T) {
	tests := []struct {
		name        string
		graph       string
		wantExit    int
		wantErr     string
		wantUpgrade bool // true → fix should say "upgrade tm"; false → fix should name the file
	}{
		{
			name:        "load refuses format-1 with migrate fix naming the file",
			graph:       minimalFormat1Graph,
			wantExit:    1,
			wantErr:     "g.mmd is format 1, this is tm format 2",
			wantUpgrade: false,
		},
		{
			name:        "load refuses format-3 with upgrade fix",
			graph:       format3Graph,
			wantExit:    1,
			wantErr:     "g.mmd is format 3, this is tm format 2",
			wantUpgrade: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			errlogPath := tempErrlog(t)
			gPath := writeFormatGraph(t, dir, tc.graph)
			// Clear TM_FILE so load uses only the positional arg.
			t.Setenv("TM_FILE", "")
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Chdir(t.TempDir())

			_, errOut, code := run(t, "load", gPath)

			if code != tc.wantExit {
				t.Fatalf("exit: want %d, got %d; stderr:\n%s", tc.wantExit, code, errOut)
			}
			if !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("err: want %q; got:\n%s", tc.wantErr, errOut)
			}
			// Assert the fix line names the positional file path specifically,
			// so the test distinguishes loadRun's check from the central check.
			if tc.wantUpgrade {
				if !strings.Contains(errOut, "upgrade tm") {
					t.Errorf("fix: want 'upgrade tm'; got:\n%s", errOut)
				}
			} else {
				wantFix := "tm migrate " + gPath
				if !strings.Contains(errOut, wantFix) {
					t.Errorf("fix: want %q; got:\n%s", wantFix, errOut)
				}
			}
			rows := readErrlog(t, errlogPath)
			if len(rows) == 0 {
				t.Errorf("expected errlog row; got none")
			}
		})
	}
}

// TestFormatCheck_LoadSkipsCentralCheck is the regression test for the
// SkipFormatCheck: true fix on the load command: when TM_FILE points to a
// format-1 graph, `tm load <format-2 file>` must succeed and write the pointer.
func TestFormatCheck_LoadSkipsCentralCheck(t *testing.T) {
	dir := t.TempDir()
	tempErrlog(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(dir)

	// TM_FILE points to a format-1 graph (the central check's target).
	f1Path := writeFormatGraph(t, dir, minimalFormat1Graph)
	t.Setenv("TM_FILE", f1Path)

	// The file to load is a fresh format-2 graph created by tm new.
	f2Path := filepath.Join(dir, "target.mmd")
	if _, _, code := run(t, "new", f2Path); code != 0 {
		t.Fatalf("new: want exit 0, got %d", code)
	}

	// load <format-2 file> must succeed even though TM_FILE is format-1.
	_, errOut, code := run(t, "load", f2Path)
	if code != 0 {
		t.Fatalf("load: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.Contains(errOut, "is format") {
		t.Errorf("load emitted a format error: %s", errOut)
	}
}

// TestFormatCheck_SkippedCommands verifies that new, migrate, lint, --help, and
// --version do not refuse when TM_FILE points to a format-1 graph.
func TestFormatCheck_SkippedCommands(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantExit int
		wantOut  string
	}{
		{
			name:     "lint accepts format-1",
			args:     []string{"lint"},
			wantExit: 0,
		},
		{
			name:     "migrate accepts format-1 and upgrades it",
			args:     []string{"migrate"},
			wantExit: 0,
			wantOut:  "format 1 -> 2",
		},
		{
			name:     "--help accepts format-1",
			args:     []string{"--help"},
			wantExit: 0,
		},
		{
			name:     "--version accepts format-1",
			args:     []string{"--version"},
			wantExit: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tempErrlog(t)
			gPath := writeFormatGraph(t, dir, minimalFormat1Graph)
			t.Setenv("TM_FILE", gPath)
			t.Setenv("TM_DOC", "")

			out, errOut, code := run(t, tc.args...)

			if code != tc.wantExit {
				t.Fatalf("exit: want %d, got %d; stdout: %s; stderr: %s", tc.wantExit, code, out, errOut)
			}
			if tc.wantOut != "" && !strings.Contains(out, tc.wantOut) {
				t.Errorf("stdout: want %q; got:\n%s", tc.wantOut, out)
			}
			if strings.Contains(errOut, "is format") {
				t.Errorf("unexpected format error emitted: %s", errOut)
			}
		})
	}
}

// TestFormatCheck_NewSkipped verifies that new creates a format-2 graph and is
// not blocked by TM_FILE pointing to a format-1 graph.
func TestFormatCheck_NewSkipped(t *testing.T) {
	dir := t.TempDir()
	tempErrlog(t)
	existingF1 := writeFormatGraph(t, dir, minimalFormat1Graph)
	t.Setenv("TM_FILE", existingF1) // format-1 in TM_FILE; new must not refuse

	newPath := filepath.Join(dir, "new.mmd")
	_, errOut, code := run(t, "new", newPath)
	if code != 0 {
		t.Fatalf("new: want exit 0, got %d; stderr: %s", code, errOut)
	}
	if strings.Contains(errOut, "is format") {
		t.Errorf("new emitted a format error: %s", errOut)
	}
}

// TestFormatCheck_Lifecycle verifies that new → add → q → ask → answer → grade
// all succeed: the %% tm:format 2 marker written by new survives every mutation.
func TestFormatCheck_Lifecycle(t *testing.T) {
	dir := t.TempDir()
	tempErrlog(t)
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_PROBE_MIN", "1")
	t.Setenv("TM_MAX_FAILS", "2")

	// Source file required for citations.
	if err := os.WriteFile(filepath.Join(dir, "src.txt"),
		[]byte("line 1\nline 2\nline 3\nline 4\nline 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	gPath := filepath.Join(dir, "g.mmd")
	t.Chdir(dir)

	mustRun := func(op string, args ...string) {
		t.Helper()
		_, errOut, code := run(t, args...)
		if code != 0 {
			t.Fatalf("%s: want exit 0, got %d; stderr: %s", op, code, errOut)
		}
		if strings.Contains(errOut, "is format") {
			t.Errorf("%s: unexpected format error: %s", op, errOut)
		}
	}

	mustRun("new", "new", gPath)
	t.Setenv("TM_FILE", gPath)
	mustRun("add", "add", "mycon", "src.txt:1-5", "My concept")
	mustRun("q", "q", "mycon", "src.txt:1-5", "Narrow scope")

	// ask: get the question ID (first field on line 2: line 1 is the batch header).
	askOut, _, askCode := run(t, "ask", "mycon")
	if askCode != 0 {
		t.Fatalf("ask: want exit 0, got %d", askCode)
	}
	askLines := strings.Split(strings.TrimSpace(askOut), "\n")
	if len(askLines) < 2 || askLines[1] == "" {
		t.Fatalf("ask: expected ≥2 lines; got:\n%s", askOut)
	}
	qid := strings.Fields(askLines[1])[0]

	mustRun("answer", "answer", qid, "My answer")
	mustRun("grade", "grade", qid, "pass", "Correct")

	// Format marker must survive all mutations.
	data, err := os.ReadFile(gPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "%% tm:format 2") {
		t.Errorf("lifecycle: %% tm:format 2 missing after all mutations")
	}
}
