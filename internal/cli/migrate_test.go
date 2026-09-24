package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// minimalFormat1Graph returns a minimal format-1 graph (no %% tm:format line,
// no citations). It lints clean and is suitable as a migrate input.
const minimalFormat1Graph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
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

// minimalFormat2Graph is the same graph but already migrated (format 2).
const minimalFormat2Graph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
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

// citeF1Graph is a minimal format-1 graph with one concept citation and one
// question citation (no answer, so batch-minimum lint check does not apply).
// The citations use the hash f5ca3875b379 for src.txt lines 1-5.
// TM_SRC_ROOT must point to a directory containing that src.txt.
const citeF1Graph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        c1["Concept one<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph reserve["Concepts held in reserve"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        c1 --> q1
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// readMigrateEvent reads and returns all "migrate" event rows from a
// <graph>.mmd.jsonl file. Each row is a map[string]any.
func readMigrateEvent(t *testing.T, graphPath string) []map[string]any {
	t.Helper()
	logPath := graphPath + ".jsonl"
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read event log: %v", err)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("parse event log row: %v", err)
		}
		if row["ev"] == "migrate" {
			rows = append(rows, row)
		}
	}
	return rows
}

// ── TestMigrate table-driven suite ───────────────────────────────────────────

func TestMigrate(t *testing.T) {
	tests := []struct {
		name        string
		graphCont   string
		args        []string
		wantExit    int
		wantOut     string // substring expected in stdout
		wantErr     string // substring expected in stderr (for refusals)
		wantFix     string // substring expected in stderr fix line
		wantFormat2 bool   // file should have FormatN()==2 after run
		wantEvent   bool   // a migrate event should be appended
		checkErrlog bool   // verify an errlog row was written
		dryRun      bool   // --dry-run: file must remain unchanged
	}{
		{
			name:        "empty format-1 graph gets marker",
			graphCont:   minimalFormat1Graph,
			args:        []string{},
			wantExit:    0,
			wantOut:     "format 1 -> 2: 0 rewritten, 0 left",
			wantFormat2: true,
			wantEvent:   true,
		},
		{
			name:        "dry-run leaves file unchanged",
			graphCont:   minimalFormat1Graph,
			args:        []string{"--dry-run"},
			wantExit:    0,
			wantOut:     "format 1 -> 2: 0 rewritten, 0 left",
			wantFormat2: false,
			wantEvent:   false,
			dryRun:      true,
		},
		{
			name:        "already format 2 refuses",
			graphCont:   minimalFormat2Graph,
			args:        []string{},
			wantExit:    1,
			wantErr:     "is already format 2",
			wantFormat2: true, // file unchanged
			wantEvent:   false,
			checkErrlog: true,
		},
		{
			// format-3 graph is above binary; migrate must refuse with upgrade fix.
			name:      "format-3 graph refuses with upgrade fix",
			graphCont: format3Graph,
			args:      []string{},
			wantExit:  1,
			wantErr:   "is format 3, this is tm format 2",
			wantFix:   "upgrade tm",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			errlogPath := tempErrlog(t)

			// Write the graph file.
			gPath := filepath.Join(dir, "g.mmd")
			if err := os.WriteFile(gPath, []byte(tc.graphCont), 0o644); err != nil {
				t.Fatalf("write graph: %v", err)
			}
			t.Setenv("TM_FILE", gPath)

			// Build args: cmd + positional + flags.
			args := append([]string{"migrate"}, tc.args...)

			out, errOut, code := run(t, args...)

			if code != tc.wantExit {
				t.Errorf("exit: want %d, got %d\nstdout: %s\nstderr: %s", tc.wantExit, code, out, errOut)
			}

			if tc.wantOut != "" && !strings.Contains(out, tc.wantOut) {
				t.Errorf("stdout: want %q in output; got:\n%s", tc.wantOut, out)
			}

			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("stderr: want %q in output; got:\n%s", tc.wantErr, errOut)
			}

			if tc.wantFix != "" && !strings.Contains(errOut, tc.wantFix) {
				t.Errorf("stderr fix: want %q in output; got:\n%s", tc.wantFix, errOut)
			}

			// Check file format after run.
			afterData, err := os.ReadFile(gPath)
			if err != nil {
				t.Fatalf("read graph after run: %v", err)
			}
			afterG, err := graph.Parse(afterData)
			if err != nil {
				t.Fatalf("parse graph after run: %v", err)
			}

			switch {
			case tc.dryRun:
				// File must be unchanged: same bytes as original.
				if string(afterData) != tc.graphCont {
					t.Errorf("dry-run: file was modified")
				}
			case tc.wantFormat2:
				if afterG.FormatN() != 2 {
					t.Errorf("format: want 2, got %d", afterG.FormatN())
				}
			default:
				if afterG.FormatN() == 2 {
					t.Errorf("format: should not be 2; got %d", afterG.FormatN())
				}
			}

			// Check event log.
			if tc.wantEvent {
				evRows := readMigrateEvent(t, gPath)
				if len(evRows) == 0 {
					t.Errorf("expected migrate event in event log; got none")
				} else {
					ev := evRows[0]
					if ev["from"] != float64(1) {
						t.Errorf("event from: want 1, got %v", ev["from"])
					}
					if ev["to"] != float64(2) {
						t.Errorf("event to: want 2, got %v", ev["to"])
					}
				}
			}

			// Dry-run must not write an event.
			if tc.dryRun {
				logPath := gPath + ".jsonl"
				if _, err := os.Stat(logPath); err == nil {
					t.Errorf("dry-run: event log was created")
				}
			}

			// Check errlog row on refusal.
			if tc.checkErrlog {
				rows := readErrlog(t, errlogPath)
				if len(rows) == 0 {
					t.Errorf("expected errlog row; got none")
				} else if !strings.Contains(rows[0].Err, "already format") {
					t.Errorf("errlog err: want 'already format', got %q", rows[0].Err)
				}
			}
		})
	}
}

// TestMigrate_CitationsBecomeLeft verifies that a format-1 graph with citations
// lists each citation in the "left" output with reason "plain path".
func TestMigrate_CitationsBecomeLeft(t *testing.T) {
	dir := t.TempDir()
	tempErrlog(t)

	// Write the source file so lint check 11 can verify hashes.
	srcContent := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(srcContent), 0o644); err != nil {
		t.Fatalf("write src.txt: %v", err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	// Write the format-1 graph with citations.
	gPath := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(gPath, []byte(citeF1Graph), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	t.Setenv("TM_FILE", gPath)

	out, errOut, code := run(t, "migrate")
	if code != 0 {
		t.Fatalf("exit: want 0, got %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}

	// Both citations should appear as left lines.
	if !strings.Contains(out, "left c1") {
		t.Errorf("expected 'left c1' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "left q1") {
		t.Errorf("expected 'left q1' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "plain path") {
		t.Errorf("expected 'plain path' reason in output; got:\n%s", out)
	}

	// Summary should show 2 left.
	if !strings.Contains(out, "2 left") {
		t.Errorf("expected '2 left' in summary; got:\n%s", out)
	}
	if !strings.Contains(out, "0 rewritten") {
		t.Errorf("expected '0 rewritten' in summary; got:\n%s", out)
	}

	// Event log should record 2 left, 0 rewritten.
	evRows := readMigrateEvent(t, gPath)
	if len(evRows) == 0 {
		t.Fatal("expected migrate event in event log; got none")
	}
	ev := evRows[0]
	if ev["left"] != float64(2) {
		t.Errorf("event left: want 2, got %v", ev["left"])
	}
	if ev["rewritten"] != float64(0) {
		t.Errorf("event rewritten: want 0, got %v", ev["rewritten"])
	}
	// §10: unresolved must list the left IDs in the order they were gathered.
	unresolved, ok := ev["unresolved"].([]any)
	if !ok || len(unresolved) != 2 {
		t.Errorf("event unresolved: want [c1 q1], got %v", ev["unresolved"])
	} else {
		for i, want := range []string{"c1", "q1"} {
			if got, _ := unresolved[i].(string); got != want {
				t.Errorf("event unresolved[%d]: want %q, got %q", i, want, got)
			}
		}
	}

	// File should now be format 2.
	afterData, _ := os.ReadFile(gPath)
	afterG, _ := graph.Parse(afterData)
	if afterG.FormatN() != 2 {
		t.Errorf("format after migrate: want 2, got %d", afterG.FormatN())
	}
}

// TestMigrate_PositionalArgBeatsConfig verifies that the positional <file>
// argument takes precedence over TM_FILE.
func TestMigrate_PositionalArgBeatsConfig(t *testing.T) {
	dir := t.TempDir()
	tempErrlog(t)

	// Write a format-1 graph for the positional path.
	gPath := filepath.Join(dir, "actual.mmd")
	if err := os.WriteFile(gPath, []byte(minimalFormat1Graph), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}

	// TM_FILE points to a nonexistent file; the positional arg should win.
	t.Setenv("TM_FILE", filepath.Join(dir, "does-not-exist.mmd"))

	out, errOut, code := run(t, "migrate", gPath)
	if code != 0 {
		t.Fatalf("exit: want 0, got %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}

	// The actual file should be format 2.
	afterData, _ := os.ReadFile(gPath)
	afterG, _ := graph.Parse(afterData)
	if afterG.FormatN() != 2 {
		t.Errorf("format: want 2, got %d", afterG.FormatN())
	}
}
