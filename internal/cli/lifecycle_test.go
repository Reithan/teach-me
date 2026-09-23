package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/lint"
)

// makeRaftSrcRoot creates a temp dir with raft.txt (≥300 numbered lines) and
// sets TM_SRC_ROOT to it. Returns the directory path.
func makeRaftSrcRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "raft.txt"), makeLines(300), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	return dir
}

// lintFileWithSrcRoot checks that the graph at file passes lint using the
// given srcRoot.
func lintFileWithSrcRoot(t *testing.T, file, srcRoot string) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read file for lint: %v", err)
	}
	viols := lint.Check(data, lint.Config{
		SrcRoot: srcRoot, ProbeMin: 2, ProbeMax: 5, TeachMin: 1, TeachMax: 3,
	})
	if len(viols) > 0 {
		t.Errorf("graph fails lint: %v", viols)
	}
}

// TestM5Lifecycle drives the full M5 concept-mutation surface in order:
// new → add (with edges) → link → gap → edit → drop → reopen (on fixture).
//
// Each step asserts: exit 0, graph round-trips, lint-clean, correct event
// appended to the event log.
func TestM5Lifecycle(t *testing.T) {
	// Resolve the raft fixture path BEFORE any t.Chdir call.
	raftFixturePath := raftFixture(t)

	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir) // writes src.txt and sets TM_SRC_ROOT

	// ── 1. tm new ─────────────────────────────────────────────────────────────
	g1 := filepath.Join(dir, "g1.mmd")
	{
		out, errOut, code := run(t, "new", g1)
		if code != 0 {
			t.Fatalf("new: want exit 0, got %d; stderr:\n%s", code, errOut)
		}
		if strings.TrimSpace(out) != "ok" {
			t.Errorf("new: want 'ok', got %q", out)
		}
		t.Setenv("TM_FILE", g1)
		lintFile(t, g1, dir)
		rows := readEventLog(t, g1)
		if len(rows) != 1 || rows[0]["ev"] != "new" {
			t.Fatalf("after new: want 1 'new' event, got %v", rows)
		}
	}

	// ── 2. tm add a ───────────────────────────────────────────────────────────
	{
		_, errOut, code := run(t, "add", "a", "f5ca3875b379@src.txt:1-5", "concept a scope")
		if code != 0 {
			t.Fatalf("add a: want exit 0, got %d; stderr:\n%s", code, errOut)
		}
		lintFile(t, g1, dir)
		rows := readEventLog(t, g1)
		if rows[len(rows)-1]["ev"] != "add" || rows[len(rows)-1]["id"] != "a" {
			t.Errorf("after add a: want last event {ev:add, id:a}, got %v", rows[len(rows)-1])
		}
	}

	// ── 3. tm add b --parent a:requires ──────────────────────────────────────
	{
		_, errOut, code := run(t, "add", "b", "f5ca3875b379@src.txt:1-5", "concept b scope", "--parent", "a:requires")
		if code != 0 {
			t.Fatalf("add b: want exit 0, got %d; stderr:\n%s", code, errOut)
		}
		lintFile(t, g1, dir)

		// Verify edge a→b exists.
		data, _ := os.ReadFile(g1)
		g, _ := graph.Parse(data)
		edgeFound := false
		for _, e := range g.Edges {
			if e.From == "a" && e.To == "b" && e.Label == "requires" {
				edgeFound = true
			}
		}
		if !edgeFound {
			t.Error("add b --parent a:requires: edge a→b not found")
		}
	}

	// ── 4. tm add c ───────────────────────────────────────────────────────────
	{
		_, errOut, code := run(t, "add", "c", "f5ca3875b379@src.txt:1-5", "concept c scope")
		if code != 0 {
			t.Fatalf("add c: want exit 0, got %d; stderr:\n%s", code, errOut)
		}
		lintFile(t, g1, dir)
	}

	// ── 5. tm link a c "also needed for" ─────────────────────────────────────
	{
		_, errOut, code := run(t, "link", "a", "c", "also needed for")
		if code != 0 {
			t.Fatalf("link: want exit 0, got %d; stderr:\n%s", code, errOut)
		}
		lintFile(t, g1, dir)
		rows := readEventLog(t, g1)
		last := rows[len(rows)-1]
		if last["ev"] != "link" || last["from"] != "a" || last["to"] != "c" {
			t.Errorf("after link: want {ev:link,from:a,to:c}, got %v", last)
		}
	}

	// ── 6. tm gap b "missed details" ─────────────────────────────────────────
	{
		_, errOut, code := run(t, "gap", "b", "missed details")
		if code != 0 {
			t.Fatalf("gap: want exit 0, got %d; stderr:\n%s", code, errOut)
		}
		lintFile(t, g1, dir)
		rows := readEventLog(t, g1)
		last := rows[len(rows)-1]
		if last["ev"] != "gap" || last["concept"] != "b" {
			t.Errorf("after gap: want {ev:gap,concept:b}, got %v", last)
		}
	}

	// ── 7. tm edit b "updated concept b scope" ───────────────────────────────
	{
		_, errOut, code := run(t, "edit", "b", "updated concept b scope")
		if code != 0 {
			t.Fatalf("edit b scope: want exit 0, got %d; stderr:\n%s", code, errOut)
		}
		lintFile(t, g1, dir)
		rows := readEventLog(t, g1)
		last := rows[len(rows)-1]
		if last["ev"] != "edit" || last["id"] != "b" {
			t.Errorf("after edit b: want {ev:edit,id:b}, got %v", last)
		}
		if last["before"] != "concept b scope" || last["after"] != "updated concept b scope" {
			t.Errorf("edit b: unexpected before/after: %v", last)
		}
	}

	// ── 8. tm edit c --src b8ea715cd2ec@src.txt:3-7 ───────────────────────────────────────
	{
		_, errOut, code := run(t, "edit", "c", "revised c scope", "--src", "b8ea715cd2ec@src.txt:3-7")
		if code != 0 {
			t.Fatalf("edit c src: want exit 0, got %d; stderr:\n%s", code, errOut)
		}
		lintFile(t, g1, dir)
		data, _ := os.ReadFile(g1)
		g, _ := graph.Parse(data)
		for _, cn := range g.UntestedConcepts {
			if cn.ID == "c" {
				if len(cn.Cites) != 1 || cn.Cites[0] != "b8ea715cd2ec@src.txt:3-7" {
					t.Errorf("edit c --src: cites want [b8ea715cd2ec@src.txt:3-7], got %v", cn.Cites)
				}
				if cn.Scope != "revised c scope" {
					t.Errorf("edit c --src: scope want 'revised c scope', got %q", cn.Scope)
				}
			}
		}
	}

	// ── 9. tm drop c ─────────────────────────────────────────────────────────
	{
		_, errOut, code := run(t, "drop", "c")
		if code != 0 {
			t.Fatalf("drop c: want exit 0, got %d; stderr:\n%s", code, errOut)
		}
		lintFile(t, g1, dir)
		data, _ := os.ReadFile(g1)
		g, _ := graph.Parse(data)
		for _, cn := range g.UntestedConcepts {
			if cn.ID == "c" {
				t.Error("concept c should be removed from untested")
			}
		}
		rows := readEventLog(t, g1)
		last := rows[len(rows)-1]
		if last["ev"] != "drop" || last["id"] != "c" {
			t.Errorf("after drop c: want {ev:drop,id:c}, got %v", last)
		}
	}

	// ── Verify complete event sequence on g1 ──────────────────────────────────
	{
		rows := readEventLog(t, g1)
		wantEvs := []string{"new", "add", "add", "add", "link", "gap", "edit", "edit", "drop"}
		if len(rows) != len(wantEvs) {
			t.Fatalf("g1 event log: want %d events, got %d: %v", len(wantEvs), len(rows), rows)
		}
		for i, want := range wantEvs {
			if rows[i]["ev"] != want {
				t.Errorf("event[%d]: want %q, got %v", i, want, rows[i]["ev"])
			}
		}
	}

	// ── Final round-trip check for g1 ────────────────────────────────────────
	{
		data, err := os.ReadFile(g1)
		if err != nil {
			t.Fatalf("read g1: %v", err)
		}
		g, err := graph.Parse(data)
		if err != nil {
			t.Fatalf("parse g1: %v", err)
		}
		roundTripped := graph.Write(g)
		if string(data) != string(roundTripped) {
			t.Errorf("g1 round-trip mismatch:\ngot:\n%s\nwant:\n%s", data, roundTripped)
		}
	}

	// ── 10. tm reopen on raft fixture ─────────────────────────────────────────
	// Switch to a new temp dir so raft.txt can coexist cleanly.
	raftDir := t.TempDir()
	raftSrcRoot := makeRaftSrcRoot(t) // sets TM_SRC_ROOT
	// makeRaftSrcRoot returns the dir where raft.txt lives, but TM_SRC_ROOT is
	// already set via t.Setenv inside it.

	// Copy raft.mmd to the raft temp dir.
	g2 := filepath.Join(raftDir, "g2.mmd")
	{
		data, err := os.ReadFile(raftFixturePath)
		if err != nil {
			t.Fatalf("read raft fixture: %v", err)
		}
		if err := os.WriteFile(g2, data, 0o644); err != nil {
			t.Fatalf("write raft copy: %v", err)
		}
	}
	t.Setenv("TM_FILE", g2)

	// Verify raft fixture lints clean before we do anything.
	lintFileWithSrcRoot(t, g2, raftSrcRoot)

	// Reopen leader_election (which is in passed).
	{
		_, errOut, code := run(t, "reopen", "leader_election", "missed election timing")
		if code != 0 {
			t.Fatalf("reopen: want exit 0, got %d; stderr:\n%s", code, errOut)
		}
	}

	// Verify leader_election moved to untested with the GAP.
	{
		data, err := os.ReadFile(g2)
		if err != nil {
			t.Fatalf("read g2: %v", err)
		}
		g, parseErr := graph.Parse(data)
		if parseErr != nil {
			t.Fatalf("parse g2: %v", parseErr)
		}

		// leader_election must not be in passed.
		for _, c := range g.PassedConcepts {
			if c.ID == "leader_election" {
				t.Error("leader_election should not be in passed after reopen")
			}
		}
		// leader_election must be at top of untested with the GAP.
		if len(g.UntestedConcepts) == 0 {
			t.Fatal("untested block empty after reopen")
		}
		top := g.UntestedConcepts[0]
		if top.ID != "leader_election" {
			t.Errorf("want leader_election at top of untested, got %q", top.ID)
		}
		if top.GAP != "missed election timing" {
			t.Errorf("GAP: want 'missed election timing', got %q", top.GAP)
		}

		// replicated_log (the other passed concept) must still be in passed.
		foundReplicatedLog := false
		for _, c := range g.PassedConcepts {
			if c.ID == "replicated_log" {
				foundReplicatedLog = true
			}
		}
		if !foundReplicatedLog {
			t.Error("replicated_log should remain in passed after reopening leader_election")
		}

		// Round-trip.
		roundTripped := graph.Write(g)
		if string(data) != string(roundTripped) {
			t.Errorf("g2 round-trip mismatch:\ngot:\n%s\nwant:\n%s", data, roundTripped)
		}

		// Lint-clean (passed descendant replicated_log stays in passed,
		// untested leader_election → passed replicated_log via the existing edge).
		lintFileWithSrcRoot(t, g2, raftSrcRoot)
	}

	// Verify reopen event in g2's event log.
	{
		rows := readEventLog(t, g2)
		if len(rows) == 0 {
			t.Fatal("g2 event log empty after reopen")
		}
		last := rows[len(rows)-1]
		if last["ev"] != "reopen" {
			t.Errorf("g2 last event: want 'reopen', got %v", last["ev"])
		}
		if last["concept"] != "leader_election" {
			t.Errorf("reopen event concept: want 'leader_election', got %v", last["concept"])
		}
		if last["gap"] != "missed election timing" {
			t.Errorf("reopen event gap: want 'missed election timing', got %v", last["gap"])
		}
	}
}
