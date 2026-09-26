package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// --- test helpers ---

// writeTemp writes content to a new temp file with the given suffix and returns
// the path. The file is registered for cleanup.
func writeTemp(t *testing.T, content, suffix string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*"+suffix)
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return f.Name()
}

// mustLoad loads the state from a .mmd temp file or fatals.
func mustLoad(t *testing.T, content string, cfg Config) *State {
	t.Helper()
	path := writeTemp(t, content, ".mmd")
	s, err := Load(path, cfg)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

// defaultCfg returns the spec §13 default Config (no SrcRoot set).
func defaultCfg() Config {
	return Config{
		ProbeMin: 2,
		ProbeMax: 5,
		TeachMin: 1,
		TeachMax: 3,
		MaxFails: 2,
		MaxTeach: 8,
		MaxStall: 4,
	}
}

func sorted(ss []string) []string {
	out := make([]string, len(ss))
	copy(out, ss)
	sort.Strings(out)
	return out
}

// --- fixtures ---

// raftGraph is the canonical §4 sample graph (identical to testdata/raft.mmd).
// Read as state: probe_1 resolved with one fail; probe_2 is the fallback,
// locked because teach_3 exists; teach_3 is open with q6 unanswered.
const raftGraph = `---
config:
  look: classic
  darkMode: true
  theme: dark
  layout: elk
  elk:
    mergeEdges: true
    nodePlacementStrategy: NETWORK_SIMPLEX
---
flowchart TB
    subgraph passed["Concepts User understands"]
        leader_election["Leader election: terms, votes, majority<br/>raft.txt:120-188"]
        replicated_log["Replicated log: entries, indexes, terms<br/>raft.txt:40-96"]
        replicated_log --"elections protect"--> leader_election
    end
    subgraph untested["Concepts User has not been tested on"]
        log_matching["Log matching property<br/>GAP: treats index match as sufficient, ignores term<br/>raft.txt:190-240"]
        commit_rules["Commit rules: when an entry is safe to apply<br/>raft.txt:241-300"]
        replicated_log --"constrains"--> log_matching
        leader_election --"enables"--> commit_rules
        log_matching --"required by"--> commit_rules
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["Same index and term implies same entry<br/>raft.txt:192-201"]:::probe_1
        q2["Same index and term implies identical prefix<br/>raft.txt:202-215"]:::probe_1
        a1["Matching index and term means the same command is stored"]:::pass
        a2["Says matching index is enough; never mentions term"]:::fail
        q3["Induction step: how one check extends to the whole prefix<br/>raft.txt:229-240"]:::probe_2
        q4["Two logs agree at index 7 but differ in term: what follows<br/>raft.txt:202-215"]:::probe_2
        q5["What the AppendEntries consistency check compares<br/>raft.txt:216-228"]:::teach_3
        a5["Names prevLogIndex and prevLogTerm as the compared pair"]:::pass
        q6["Why a follower rejects on term mismatch<br/>raft.txt:216-228"]:::teach_3
        log_matching --> q1
        log_matching --> q2
        q1 --> a1
        q2 --> a2
        log_matching --> q3
        log_matching --> q4
        a2 --> q5
        q5 --> a5
        a2 --> q6
    end
    classDef probe_1,probe_2 stroke:#4aa3ff
    classDef teach_3 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// --- ResolveFile tests ---

func TestResolveFile_FlagFile(t *testing.T) {
	got, err := ResolveFile("/some/path.mmd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/some/path.mmd" {
		t.Errorf("got %q, want %q", got, "/some/path.mmd")
	}
}

func TestResolveFile_EnvVar(t *testing.T) {
	t.Setenv("TM_FILE", "/env/path.mmd")
	got, err := ResolveFile("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/env/path.mmd" {
		t.Errorf("got %q, want %q", got, "/env/path.mmd")
	}
}

func TestResolveFile_FlagOverridesEnv(t *testing.T) {
	t.Setenv("TM_FILE", "/env/path.mmd")
	got, err := ResolveFile("/flag/path.mmd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/flag/path.mmd" {
		t.Errorf("flag should take precedence over env")
	}
}

func TestResolveFile_TmConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".tmconfig")
	if err := os.WriteFile(cfgPath, []byte("file=/graphs/my.mmd\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	t.Setenv("TM_FILE", "") // clear env var

	got, err := ResolveFile("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/graphs/my.mmd" {
		t.Errorf("got %q, want /graphs/my.mmd", got)
	}
}

func TestResolveFile_TmConfigWithComments(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".tmconfig")
	content := "# comment\ndoc=foo.md\nfile=/my.mmd\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	t.Setenv("TM_FILE", "")

	got, err := ResolveFile("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/my.mmd" {
		t.Errorf("got %q, want /my.mmd", got)
	}
}

func TestResolveFile_NoSources(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(orig) })
	t.Setenv("TM_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := ResolveFile("")
	if err == nil {
		t.Fatal("expected error when no sources available")
	}
}

func TestResolveFile_TmConfigNoFileKey(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".tmconfig")
	if err := os.WriteFile(cfgPath, []byte("doc=foo.md\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	orig, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(orig) })
	t.Setenv("TM_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := ResolveFile("")
	if err == nil {
		t.Fatal("expected error: .tmconfig has no file= key")
	}
}

func TestResolveFile_UserConfigFallback(t *testing.T) {
	orig, _ := os.Getwd()
	_ = os.Chdir(t.TempDir())
	t.Cleanup(func() { _ = os.Chdir(orig) })
	t.Setenv("TM_FILE", "")
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	cfgPath := filepath.Join(xdg, "tm", "config")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("git = git\nfile = /lessons/g.mmd\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveFile("")
	if err != nil {
		t.Fatalf("ResolveFile: %v", err)
	}
	if got != "/lessons/g.mmd" {
		t.Errorf("ResolveFile = %q, want /lessons/g.mmd", got)
	}
}

// TestResolveFile_UnreadableTmConfig: a .tmconfig that exists but cannot be
// read is an error, not a silent fall-through to the user config.
func TestResolveFile_UnreadableTmConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	orig, _ := os.Getwd()
	dir := t.TempDir()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(orig) })
	t.Setenv("TM_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, ".tmconfig"), []byte("file = /g.mmd\n"), 0o000); err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveFile(""); err == nil || !strings.Contains(err.Error(), ".tmconfig") {
		t.Errorf("ResolveFile err = %v, want error naming .tmconfig", err)
	}
}

// --- ConfigFromEnv tests ---

func TestConfigFromEnv_Defaults(t *testing.T) {
	for _, v := range []string{
		"TM_PROBE_MIN", "TM_PROBE_MAX", "TM_TEACH_MIN", "TM_TEACH_MAX",
		"TM_MAX_FAILS", "TM_MAX_TEACH", "TM_MAX_STALL",
	} {
		t.Setenv(v, "")
	}
	cfg := ConfigFromEnv()
	if cfg.ProbeMin != 2 || cfg.ProbeMax != 5 {
		t.Errorf("probe min/max defaults wrong: got %d/%d", cfg.ProbeMin, cfg.ProbeMax)
	}
	if cfg.TeachMin != 1 || cfg.TeachMax != 3 {
		t.Errorf("teach min/max defaults wrong: got %d/%d", cfg.TeachMin, cfg.TeachMax)
	}
	if cfg.MaxFails != 2 || cfg.MaxTeach != 8 || cfg.MaxStall != 4 {
		t.Errorf("max fails/teach/stall defaults wrong: %d/%d/%d", cfg.MaxFails, cfg.MaxTeach, cfg.MaxStall)
	}
}

func TestConfigFromEnv_Override(t *testing.T) {
	t.Setenv("TM_PROBE_MIN", "3")
	t.Setenv("TM_MAX_FAILS", "5")
	cfg := ConfigFromEnv()
	if cfg.ProbeMin != 3 {
		t.Errorf("TM_PROBE_MIN: got %d, want 3", cfg.ProbeMin)
	}
	if cfg.MaxFails != 5 {
		t.Errorf("TM_MAX_FAILS: got %d, want 5", cfg.MaxFails)
	}
}

func TestConfigFromEnv_Invalid(t *testing.T) {
	t.Setenv("TM_PROBE_MIN", "abc")
	cfg := ConfigFromEnv()
	if cfg.ProbeMin != 2 {
		t.Errorf("bad TM_PROBE_MIN should fall back to default 2, got %d", cfg.ProbeMin)
	}
}

// --- §5 concept classification (raft fixture) ---

func TestFrontier(t *testing.T) {
	s := mustLoad(t, raftGraph, defaultCfg())
	got := s.Frontier()
	// log_matching's only parent is replicated_log (passed) → frontier.
	// commit_rules has log_matching (untested) as parent → blocked.
	want := []string{"log_matching"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Frontier() = %v, want %v", got, want)
	}
}

func TestBlocked(t *testing.T) {
	s := mustLoad(t, raftGraph, defaultCfg())
	got := s.Blocked()
	want := []string{"commit_rules"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Blocked() = %v, want %v", got, want)
	}
}

func TestOpen(t *testing.T) {
	s := mustLoad(t, raftGraph, defaultCfg())
	got := s.Open()
	// Only log_matching has questions; commit_rules has none.
	want := []string{"log_matching"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Open() = %v, want %v", got, want)
	}
}

// Empty untested — no concepts on any list.
func TestConceptLists_EmptyUntested(t *testing.T) {
	g := `flowchart TB
    subgraph passed["Passed"]
        c1["C1<br/>f.txt:1-5"]
    end
    subgraph untested["Untested"]
    end
    subgraph testing["Testing"]
    end
`
	s := mustLoad(t, g, defaultCfg())
	if len(s.Frontier()) != 0 {
		t.Errorf("Frontier should be empty")
	}
	if len(s.Blocked()) != 0 {
		t.Errorf("Blocked should be empty")
	}
	if len(s.Open()) != 0 {
		t.Errorf("Open should be empty")
	}
}

// Concept with no parents is on the frontier.
func TestFrontier_NoParents(t *testing.T) {
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-5"]
        bar["Bar<br/>f.txt:1-5"]
    end
    subgraph testing["Testing"]
    end
`
	s := mustLoad(t, g, defaultCfg())
	got := s.Frontier()
	want := []string{"bar", "foo"}
	if strings.Join(sorted(got), ",") != strings.Join(want, ",") {
		t.Errorf("Frontier() = %v, want %v", got, want)
	}
}

// --- ConceptOf ---

func TestConceptOf(t *testing.T) {
	s := mustLoad(t, raftGraph, defaultCfg())
	tests := []struct {
		qid         string
		wantConcept string
		wantOK      bool
	}{
		{"q1", "log_matching", true},  // probe_1 under log_matching
		{"q2", "log_matching", true},  // probe_1 under log_matching
		{"q5", "log_matching", true},  // teach_3 chain via a2 → q2
		{"q6", "log_matching", true},  // teach_3 chain via a2 → q2
		{"q99", "", false},            // unknown question
	}
	for _, tc := range tests {
		c, ok := s.ConceptOf(tc.qid)
		if ok != tc.wantOK || c != tc.wantConcept {
			t.Errorf("ConceptOf(%q) = (%q, %v), want (%q, %v)", tc.qid, c, ok, tc.wantConcept, tc.wantOK)
		}
	}
}

// --- BatchStateOf ---

func TestBatchStateOf(t *testing.T) {
	const draftGraph = `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q1<br/>f.txt:1-5"]:::probe_1
        q2["Q2<br/>f.txt:1-5"]:::probe_1
        foo --> q1
        foo --> q2
    end
    classDef probe_1 stroke:#4aa3ff
`
	tests := []struct {
		name       string
		graphStr   string // "" → raftGraph
		batchClass string
		want       BatchStatus
	}{
		{"resolved", "", "probe_1", BatchResolved},  // q1(pass), q2(fail) — all graded
		{"locked", "", "probe_2", BatchLocked},       // probe_2 unanswered; teach_3 (N=3>2) exists
		{"open", "", "teach_3", BatchOpen},           // q5 answered, q6 unanswered
		{"draft", draftGraph, "probe_1", BatchDraft}, // no answers, no higher teach batch
		{"unknown", "", "probe_99", BatchDraft},      // unknown batch returns BatchDraft
	}
	for _, tc := range tests {
		g := tc.graphStr
		if g == "" {
			g = raftGraph
		}
		s := mustLoad(t, g, defaultCfg())
		got := s.BatchStateOf(tc.batchClass)
		if got != tc.want {
			t.Errorf("%s: BatchStateOf(%q) = %v, want %v", tc.name, tc.batchClass, got, tc.want)
		}
	}
}

// TestBatchStateOf_GateBase_NoLock verifies Fix 1: a probe batch whose N is
// below a teach batch's N is NOT locked when the teach batch's N equals the
// gate base. Before the fix, BatchStateOf ignored the base and returned
// BatchLocked even for teach batches at or below base.
func TestBatchStateOf_GateBase_NoLock(t *testing.T) {
	// probe_3 (N=3), teach_4 (N=4, resolved pass), gate base=4.
	// teach_4's N (4) is not strictly above base (4), so it must not lock probe_3.
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
        %% tm:gate foo base=4
    end
    subgraph testing["Testing"]
        q3["Q<br/>f.txt:1-5"]:::probe_3
        q4["Q<br/>f.txt:1-5"]:::teach_4
        a4["pass"]:::pass
        foo --> q3
        foo --> q4
        q4 --> a4
    end
    classDef probe_3,probe_1 stroke:#4aa3ff
    classDef teach_4 stroke:#c9a227
    classDef pass stroke:#3fb950
`
	s := mustLoad(t, g, defaultCfg())
	got := s.BatchStateOf("probe_3")
	if got != BatchDraft {
		t.Errorf("BatchStateOf(probe_3) = %v, want BatchDraft; "+
			"teach_4 at gate base must not lock probe_3 (Fix 1)", got)
	}
}

// TestConceptStatus_LatestTeachNotAllPass_NoTeachBatches verifies that
// LatestTeachNotAllPass is true when no teach batches exist above base.
func TestConceptStatus_LatestTeachNotAllPass_NoTeachBatches(t *testing.T) {
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q<br/>f.txt:1-5"]:::probe_1
        foo --> q1
    end
    classDef probe_1 stroke:#4aa3ff
`
	s := mustLoad(t, g, defaultCfg())
	cs := s.ConceptStatus("foo")
	if !cs.LatestTeachNotAllPass {
		t.Error("LatestTeachNotAllPass should be true when no teach batches exist above base")
	}
}

// TestConceptStatus_LatestTeachNotAllPass_LatestAllPass verifies that
// LatestTeachNotAllPass is false when the latest teach batch is all-pass.
func TestConceptStatus_LatestTeachNotAllPass_LatestAllPass(t *testing.T) {
	// probe_1 (q1 fail), teach_2 (q2 pass) — latest batch is all-pass.
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q<br/>f.txt:1-5"]:::probe_1
        a1["fail"]:::fail
        q2["Q<br/>f.txt:1-5"]:::teach_2
        a2["pass"]:::pass
        foo --> q1
        q1 --> a1
        a1 --> q2
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef teach_2 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
	s := mustLoad(t, g, defaultCfg())
	cs := s.ConceptStatus("foo")
	if cs.LatestTeachNotAllPass {
		t.Error("LatestTeachNotAllPass should be false when latest teach batch is all-pass")
	}
}

// TestConceptStatus_LatestTeachNotAllPass_LatestHasFail verifies that
// LatestTeachNotAllPass is true when the latest teach batch has a non-pass answer.
func TestConceptStatus_LatestTeachNotAllPass_LatestHasFail(t *testing.T) {
	// probe_1 (q1 fail), teach_2 (q2 pass), teach_4 (q4 fail) — latest has fail.
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q<br/>f.txt:1-5"]:::probe_1
        a1["fail"]:::fail
        q2["Q<br/>f.txt:1-5"]:::teach_2
        a2["pass"]:::pass
        q4["Q<br/>f.txt:1-5"]:::teach_4
        a4["fail"]:::fail
        foo --> q1
        q1 --> a1
        a1 --> q2
        q2 --> a2
        a2 --> q4
        q4 --> a4
    end
    classDef probe_1 stroke:#4aa3ff
    classDef teach_2,teach_4 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
	s := mustLoad(t, g, defaultCfg())
	cs := s.ConceptStatus("foo")
	if !cs.LatestTeachNotAllPass {
		t.Error("LatestTeachNotAllPass should be true when latest teach batch has a fail answer")
	}
}

// --- TeachingTarget ---

func TestTeachingTarget_Basic(t *testing.T) {
	s := mustLoad(t, raftGraph, defaultCfg())
	// Both q5 and q6 trace back through a2 → q2 (probe with concept edge).
	target, ok := s.TeachingTarget("q5")
	if !ok || target != "q2" {
		t.Errorf("TeachingTarget(q5) = (%q, %v), want (q2, true)", target, ok)
	}
	target, ok = s.TeachingTarget("q6")
	if !ok || target != "q2" {
		t.Errorf("TeachingTarget(q6) = (%q, %v), want (q2, true)", target, ok)
	}
}

func TestTeachingTarget_NotTeach(t *testing.T) {
	s := mustLoad(t, raftGraph, defaultCfg())
	_, ok := s.TeachingTarget("q1")
	if ok {
		t.Error("TeachingTarget on a probe question should return false")
	}
}

func TestTeachingTarget_Unknown(t *testing.T) {
	s := mustLoad(t, raftGraph, defaultCfg())
	_, ok := s.TeachingTarget("q99")
	if ok {
		t.Error("TeachingTarget on unknown question should return false")
	}
}

// TestTeachingTarget_ReplacementProbe verifies that walkToRootProbe stops at
// the first probe encountered — including replacement/fallback probes whose
// incoming edge is an answer rather than a concept.
func TestTeachingTarget_ReplacementProbe(t *testing.T) {
	// Graph: foo → q1 (probe_1, fail) → a1 (fail) → q2 (probe_2, fallback) →
	// a2 (fail) → q3 (teach_3). q3's target must be q2, not q1.
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q1<br/>f.txt:1-5"]:::probe_1
        q2["Q2<br/>f.txt:1-5"]:::probe_2
        a1["A1"]:::fail
        a2["A2"]:::fail
        q3["Q3<br/>f.txt:1-5"]:::teach_3
        a3["A3"]:::fail
        foo --> q1
        q1 --> a1
        a1 --> q2
        q2 --> a2
        a2 --> q3
        q3 --> a3
    end
    classDef probe_1,probe_2 stroke:#4aa3ff
    classDef teach_3 stroke:#c9a227
    classDef fail stroke:#f85149
`
	s := mustLoad(t, g, defaultCfg())

	target, ok := s.TeachingTarget("q3")
	if !ok {
		t.Fatal("TeachingTarget(q3) should succeed")
	}
	// q3 teach target: walk back a2 → q2 (probe_2). q2 is a probe — stop here.
	// The target is q2, not q1, because q2 is the probe whose answer failed.
	if target != "q2" {
		t.Errorf("TeachingTarget(q3) = %q, want q2", target)
	}
}

// TestTeachingTarget_MultiHopTeach covers a true multi-hop teach chain where
// all intermediate nodes are teach questions (not probes).
func TestTeachingTarget_MultiHopTeach(t *testing.T) {
	// Graph: foo → q1 (probe_1, fail) → a1 (fail) → q2 (teach_2) → a2 (fail) → q3 (teach_3).
	// Both q2 and q3 should resolve to q1 as teaching target.
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q1<br/>f.txt:1-5"]:::probe_1
        a1["A1"]:::fail
        q2["Q2<br/>f.txt:1-5"]:::teach_2
        a2["A2"]:::fail
        q3["Q3<br/>f.txt:1-5"]:::teach_3
        a3["A3"]:::fail
        foo --> q1
        q1 --> a1
        a1 --> q2
        q2 --> a2
        a2 --> q3
        q3 --> a3
    end
    classDef probe_1 stroke:#4aa3ff
    classDef teach_2,teach_3 stroke:#c9a227
    classDef fail stroke:#f85149
`
	s := mustLoad(t, g, defaultCfg())

	// q2 is a teach question; chain: a1 → q1 (probe_1, concept edge) → q1 is the root.
	target, ok := s.TeachingTarget("q2")
	if !ok || target != "q1" {
		t.Errorf("TeachingTarget(q2) = (%q, %v), want (q1, true)", target, ok)
	}
	// q3 is teach_3; chain: a2 → q2 (teach_2, not probe) → a1 → q1 (probe_1). Root = q1.
	target, ok = s.TeachingTarget("q3")
	if !ok || target != "q1" {
		t.Errorf("TeachingTarget(q3) = (%q, %v), want (q1, true)", target, ok)
	}
}

// --- ConceptStatus for log_matching (raft fixture) ---

func TestConceptStatus_LogMatching(t *testing.T) {
	s := mustLoad(t, raftGraph, defaultCfg())
	cs := s.ConceptStatus("log_matching")

	if cs.Base != 0 {
		t.Errorf("Base = %d, want 0", cs.Base)
	}

	// Failed probe batches: probe_1 has a2=fail.
	if len(cs.FailedProbeBatches) != 1 || cs.FailedProbeBatches[0] != "probe_1" {
		t.Errorf("FailedProbeBatches = %v, want [probe_1]", cs.FailedProbeBatches)
	}

	// Fallback probes: probe_2 follows probe_1 (failed) and is unanswered.
	if len(cs.FallbackProbes) != 1 || cs.FallbackProbes[0] != "probe_2" {
		t.Errorf("FallbackProbes = %v, want [probe_2]", cs.FallbackProbes)
	}

	// teach_3 is not resolved (q6 unanswered), so stall streak = 0.
	if cs.StallStreak != 0 {
		t.Errorf("StallStreak = %d, want 0", cs.StallStreak)
	}
	if cs.Stalled {
		t.Error("Stalled should be false")
	}

	// TeachCount: teach_3 (N=3 > latestFailed N=1): in-scope = q5 (a5=pass, not OOS) + q6 (no answer, in-scope) = 2.
	if cs.TeachCount != 2 {
		t.Errorf("TeachCount = %d, want 2", cs.TeachCount)
	}
	if cs.TeachingSpent {
		t.Error("TeachingSpent should be false (2 < 8)")
	}

	// Gated: 1 failed batch < 2 (MaxFails), not stalled.
	if cs.Gated {
		t.Error("Gated should be false")
	}
}

// --- Stall streak ---

// stallGraph builds a concept "foo" with probe_1 (q1=fail, q2=fail) and one
// or more resolved teach batches, each with one question. verdicts lists the
// verdict ("pass"/"fail"/"unclear") for each teach batch in batch-number order,
// starting at batch N=3. Teach questions chain through the fail-answer of the
// previous batch (or a1 for the first).
func stallGraph(verdicts []string) string {
	var sb strings.Builder
	sb.WriteString("flowchart TB\n" +
		"    subgraph passed[\"Passed\"]\n    end\n" +
		"    subgraph untested[\"Untested\"]\n        foo[\"Foo<br/>f.txt:1-10\"]\n    end\n" +
		"    subgraph testing[\"Testing\"]\n")

	// probe_1: q1 (fail) and probe_2: q2,q3 (unanswered fallback).
	sb.WriteString("        q1[\"Q1<br/>f.txt:1-5\"]:::probe_1\n        a1[\"A1\"]:::fail\n")
	sb.WriteString("        q2[\"Q2<br/>f.txt:1-5\"]:::probe_2\n        q3[\"Q3<br/>f.txt:1-5\"]:::probe_2\n")

	type batchInfo struct {
		class string
		qNum  int
		verd  string
	}
	batches := make([]batchInfo, len(verdicts))
	qN := 4
	batchN := 3
	for i, v := range verdicts {
		batches[i] = batchInfo{
			class: fmt.Sprintf("teach_%d", batchN),
			qNum:  qN,
			verd:  v,
		}
		batchN++
		qN++
	}

	// Write question declarations.
	for _, bi := range batches {
		fmt.Fprintf(&sb, "        q%d[\"Q%d<br/>f.txt:1-5\"]:::%s\n", bi.qNum, bi.qNum, bi.class)
	}
	// Write answer declarations.
	for _, bi := range batches {
		fmt.Fprintf(&sb, "        a%d[\"A%d\"]:::%s\n", bi.qNum, bi.qNum, bi.verd)
	}

	// Write edges: probe and fallback probe edges.
	sb.WriteString("        foo --> q1\n        q1 --> a1\n")
	sb.WriteString("        foo --> q2\n        foo --> q3\n")
	// Teach questions chain through their preceding answers.
	for i, bi := range batches {
		if i == 0 {
			fmt.Fprintf(&sb, "        a1 --> q%d\n", bi.qNum)
		} else {
			fmt.Fprintf(&sb, "        a%d --> q%d\n", batches[i-1].qNum, bi.qNum)
		}
		fmt.Fprintf(&sb, "        q%d --> a%d\n", bi.qNum, bi.qNum)
	}
	sb.WriteString("    end\n")

	// ClassDefs.
	sb.WriteString("    classDef probe_1,probe_2 stroke:#4aa3ff\n")
	for _, bi := range batches {
		fmt.Fprintf(&sb, "    classDef %s stroke:#c9a227\n", bi.class)
	}
	sb.WriteString("    classDef fail stroke:#f85149\n")
	sb.WriteString("    classDef pass stroke:#3fb950\n")
	sb.WriteString("    classDef unclear stroke:#d29922\n")

	return sb.String()
}

func TestStallStreak_AllFail(t *testing.T) {
	// Two teach batches both resolved with fail → stall streak = 2 questions.
	g := stallGraph([]string{"fail", "fail"})
	cfg := defaultCfg()
	s := mustLoad(t, g, cfg)
	cs := s.ConceptStatus("foo")
	if cs.StallStreak != 2 {
		t.Errorf("StallStreak = %d, want 2", cs.StallStreak)
	}
	if cs.Stalled {
		t.Error("Stalled should be false (2 < MaxStall=4)")
	}
}

func TestStallStreak_ResetOnPass(t *testing.T) {
	// Three batches: fail, pass, fail.
	// From highest N: teach_5(fail)→streak+1; teach_4(pass)→STOP.
	// streak = 1.
	g := stallGraph([]string{"fail", "pass", "fail"})
	cfg := defaultCfg()
	s := mustLoad(t, g, cfg)
	cs := s.ConceptStatus("foo")
	if cs.StallStreak != 1 {
		t.Errorf("StallStreak = %d, want 1 (reset at pass in teach_4)", cs.StallStreak)
	}
}

func TestStallStreak_ResetOnPassFirst(t *testing.T) {
	// Most recent batch has a pass → streak = 0.
	g := stallGraph([]string{"fail", "fail", "pass"})
	cfg := defaultCfg()
	s := mustLoad(t, g, cfg)
	cs := s.ConceptStatus("foo")
	if cs.StallStreak != 0 {
		t.Errorf("StallStreak = %d, want 0 (most recent batch has pass)", cs.StallStreak)
	}
}

func TestStalled_StreakReachesMax(t *testing.T) {
	// MaxStall=2; two fail batches → stalled.
	g := stallGraph([]string{"fail", "fail"})
	cfg := defaultCfg()
	cfg.MaxStall = 2
	s := mustLoad(t, g, cfg)
	cs := s.ConceptStatus("foo")
	if !cs.Stalled {
		t.Error("should be stalled (streak=2 >= MaxStall=2)")
	}
	if !cs.Gated {
		t.Error("should be gated when stalled")
	}
}

func TestStallStreak_UnresolvedBreaks(t *testing.T) {
	// Two fail batches but the SECOND (most recent) is unresolved (no answer).
	// Graph: probe_1 fail; teach_3 resolved fail; teach_4 draft (no answer).
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q1<br/>f.txt:1-5"]:::probe_1
        q2["Q2<br/>f.txt:1-5"]:::probe_1
        a1["A1"]:::pass
        a2["A2"]:::fail
        q3["Q3<br/>f.txt:1-5"]:::probe_2
        q4["Q4<br/>f.txt:1-5"]:::probe_2
        q5["Q5<br/>f.txt:1-5"]:::teach_3
        a5["A5"]:::fail
        q6["Q6<br/>f.txt:1-5"]:::teach_4
        foo --> q1
        foo --> q2
        q1 --> a1
        q2 --> a2
        foo --> q3
        foo --> q4
        a2 --> q5
        q5 --> a5
        a5 --> q6
    end
    classDef probe_1,probe_2 stroke:#4aa3ff
    classDef teach_3,teach_4 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
	s := mustLoad(t, g, defaultCfg())
	cs := s.ConceptStatus("foo")
	// teach_4 (most recent, N=4) is draft (no answer) → breaks streak.
	// teach_3 is never reached → streak = 0.
	if cs.StallStreak != 0 {
		t.Errorf("StallStreak = %d, want 0 (unresolved teach_4 breaks streak)", cs.StallStreak)
	}
}

// --- Gated via failed probe count ---

func TestGated_FailedProbeBatches(t *testing.T) {
	// Two failed probe batches → gated (MaxFails=2).
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q1<br/>f.txt:1-5"]:::probe_1
        q2["Q2<br/>f.txt:1-5"]:::probe_1
        a1["A1"]:::fail
        a2["A2"]:::fail
        q3["Q3<br/>f.txt:1-5"]:::probe_2
        q4["Q4<br/>f.txt:1-5"]:::probe_2
        a3["A3"]:::fail
        a4["A4"]:::fail
        q5["Q5<br/>f.txt:1-5"]:::probe_3
        q6["Q6<br/>f.txt:1-5"]:::probe_3
        foo --> q1
        foo --> q2
        q1 --> a1
        q2 --> a2
        foo --> q3
        foo --> q4
        q3 --> a3
        q4 --> a4
        foo --> q5
        foo --> q6
    end
    classDef probe_1,probe_2,probe_3 stroke:#4aa3ff
    classDef fail stroke:#f85149
`
	cfg := defaultCfg()
	s := mustLoad(t, g, cfg)
	cs := s.ConceptStatus("foo")
	if len(cs.FailedProbeBatches) != 2 {
		t.Errorf("FailedProbeBatches = %v, want 2", cs.FailedProbeBatches)
	}
	if !cs.Gated {
		t.Error("should be gated (2 failed probe batches >= MaxFails=2)")
	}
}

// Probe batches at or below base do not count toward gate.
func TestGated_BaseSuppression(t *testing.T) {
	// probe_1 and probe_2 both fail but base=2 — so neither counts.
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        %% tm:gate foo base=2
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q1<br/>f.txt:1-5"]:::probe_1
        q2["Q2<br/>f.txt:1-5"]:::probe_1
        a1["A1"]:::fail
        a2["A2"]:::fail
        q3["Q3<br/>f.txt:1-5"]:::probe_2
        q4["Q4<br/>f.txt:1-5"]:::probe_2
        a3["A3"]:::fail
        a4["A4"]:::fail
        foo --> q1
        foo --> q2
        q1 --> a1
        q2 --> a2
        foo --> q3
        foo --> q4
        q3 --> a3
        q4 --> a4
    end
    classDef probe_1,probe_2 stroke:#4aa3ff
    classDef fail stroke:#f85149
`
	s := mustLoad(t, g, defaultCfg())
	cs := s.ConceptStatus("foo")
	if cs.Base != 2 {
		t.Errorf("Base = %d, want 2", cs.Base)
	}
	if len(cs.FailedProbeBatches) != 0 {
		t.Errorf("FailedProbeBatches = %v, want empty (both below/at base)", cs.FailedProbeBatches)
	}
	if cs.Gated {
		t.Error("should not be gated: all failed batches at or below base")
	}
}

// --- TeachCount and TeachingSpent ---

func TestTeachCount_AfterLatestFailed(t *testing.T) {
	// probe_1 (N=1) fails; probe_2 (N=2) fails; teach_3 has 1 in-scope question.
	// Latest failed probe N = 2. teach_3 N=3 > 2 → count = 1.
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q1<br/>f.txt:1-5"]:::probe_1
        q2["Q2<br/>f.txt:1-5"]:::probe_1
        a1["A1"]:::fail
        a2["A2"]:::fail
        q3["Q3<br/>f.txt:1-5"]:::probe_2
        q4["Q4<br/>f.txt:1-5"]:::probe_2
        a3["A3"]:::fail
        a4["A4"]:::fail
        q5["Q5<br/>f.txt:1-5"]:::probe_3
        q6["Q6<br/>f.txt:1-5"]:::probe_3
        q7["Q7<br/>f.txt:1-5"]:::teach_4
        a7["A7"]:::fail
        foo --> q1
        foo --> q2
        q1 --> a1
        q2 --> a2
        foo --> q3
        foo --> q4
        q3 --> a3
        q4 --> a4
        foo --> q5
        foo --> q6
        a4 --> q7
        q7 --> a7
    end
    classDef probe_1,probe_2,probe_3 stroke:#4aa3ff
    classDef teach_4 stroke:#c9a227
    classDef fail stroke:#f85149
`
	s := mustLoad(t, g, defaultCfg())
	cs := s.ConceptStatus("foo")
	// Latest failed probe above base=0: probe_2 (N=2) and probe_1 (N=1). Latest = N=2.
	// Teach batches with N > 2: teach_4 (N=4), 1 in-scope question.
	if cs.TeachCount != 1 {
		t.Errorf("TeachCount = %d, want 1", cs.TeachCount)
	}
}

func TestTeachingSpent(t *testing.T) {
	// MaxTeach=1 and one in-scope teach question → teaching spent.
	s := mustLoad(t, raftGraph, Config{
		ProbeMin: 2, ProbeMax: 5, TeachMin: 1, TeachMax: 3,
		MaxFails: 2, MaxTeach: 1, MaxStall: 4,
	})
	cs := s.ConceptStatus("log_matching")
	// TeachCount=2, MaxTeach=1 → TeachingSpent.
	if !cs.TeachingSpent {
		t.Error("TeachingSpent should be true (TeachCount=2 >= MaxTeach=1)")
	}
}

// --- OOS handling ---

func TestOOS_ExcludedFromStallCount(t *testing.T) {
	// teach_3 resolved: q5 is OOS, q6 is fail in-scope.
	// Stall streak should count only q6 (1 question), not q5 (OOS).
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q1<br/>f.txt:1-5"]:::probe_1
        q2["Q2<br/>f.txt:1-5"]:::probe_1
        a1["A1"]:::pass
        a2["A2"]:::fail
        q3["Q3<br/>f.txt:1-5"]:::probe_2
        q4["Q4<br/>f.txt:1-5"]:::probe_2
        q5["Q5<br/>f.txt:1-5"]:::teach_3
        q6["Q6<br/>f.txt:1-5"]:::teach_3
        a5["OOS<br/>A5 summary"]:::fail
        a6["A6 summary"]:::fail
        foo --> q1
        foo --> q2
        q1 --> a1
        q2 --> a2
        foo --> q3
        foo --> q4
        a2 --> q5
        a2 --> q6
        q5 --> a5
        q6 --> a6
    end
    classDef probe_1,probe_2 stroke:#4aa3ff
    classDef teach_3 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
	s := mustLoad(t, g, defaultCfg())
	cs := s.ConceptStatus("foo")
	// teach_3 is resolved; in-scope passes = 0 (both fail); in-scope count = 1 (q6 only, q5 OOS).
	if cs.StallStreak != 1 {
		t.Errorf("StallStreak = %d, want 1 (q5 is OOS, only q6 counts)", cs.StallStreak)
	}
}

func TestOOS_ExcludedFromTeachCount(t *testing.T) {
	// Similar to above: OOS question should not count in TeachCount.
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q1<br/>f.txt:1-5"]:::probe_1
        q2["Q2<br/>f.txt:1-5"]:::probe_1
        a1["A1"]:::pass
        a2["A2"]:::fail
        q3["Q3<br/>f.txt:1-5"]:::probe_2
        q4["Q4<br/>f.txt:1-5"]:::probe_2
        q5["Q5<br/>f.txt:1-5"]:::teach_3
        q6["Q6<br/>f.txt:1-5"]:::teach_3
        a5["OOS<br/>A5"]:::pass
        a6["A6"]:::fail
        foo --> q1
        foo --> q2
        q1 --> a1
        q2 --> a2
        foo --> q3
        foo --> q4
        a2 --> q5
        a2 --> q6
        q5 --> a5
        q6 --> a6
    end
    classDef probe_1,probe_2 stroke:#4aa3ff
    classDef teach_3 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
	s := mustLoad(t, g, defaultCfg())
	cs := s.ConceptStatus("foo")
	// In-scope count for teach_3: q5 OOS (excluded), q6 in-scope. Count = 1.
	if cs.TeachCount != 1 {
		t.Errorf("TeachCount = %d, want 1 (q5 OOS excluded)", cs.TeachCount)
	}
}

// --- Open targets ---

func TestOpenTargets_NoPassingTeach(t *testing.T) {
	s := mustLoad(t, raftGraph, defaultCfg())
	cs := s.ConceptStatus("log_matching")
	// q2 is the failed probe. No passing in-scope teach questions target q2:
	// q5 passes (targets q2) → q2 is NOT an open target.
	// Actually: a2=fail on q2. teach q5 targets q2 and has a5=pass.
	// So q2 has a passing in-scope teach question → closed target.
	// q2 should NOT appear in OpenTargets.
	for _, ot := range cs.OpenTargets {
		if ot == "q2" {
			t.Errorf("q2 should not be an open target (q5 already passed for it)")
		}
	}
}

func TestOpenTargets_NoTeachYet(t *testing.T) {
	// probe_1 has a fail but no teach questions yet → q2 is open target.
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q1<br/>f.txt:1-5"]:::probe_1
        q2["Q2<br/>f.txt:1-5"]:::probe_1
        a1["A1"]:::pass
        a2["A2"]:::fail
        q3["Q3<br/>f.txt:1-5"]:::probe_2
        q4["Q4<br/>f.txt:1-5"]:::probe_2
        foo --> q1
        foo --> q2
        q1 --> a1
        q2 --> a2
        foo --> q3
        foo --> q4
    end
    classDef probe_1,probe_2 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
	s := mustLoad(t, g, defaultCfg())
	cs := s.ConceptStatus("foo")
	found := false
	for _, ot := range cs.OpenTargets {
		if ot == "q2" {
			found = true
		}
	}
	if !found {
		t.Errorf("q2 (failed probe with no passing teach) should be an open target; got %v", cs.OpenTargets)
	}
}

// --- FallbackProbes ---

func TestFallbackProbes_Multiple(t *testing.T) {
	// probe_1 fails; probe_2 and probe_3 both unanswered after probe_1 → both fallback.
	g := `flowchart TB
    subgraph passed["Passed"]
    end
    subgraph untested["Untested"]
        foo["Foo<br/>f.txt:1-10"]
    end
    subgraph testing["Testing"]
        q1["Q1<br/>f.txt:1-5"]:::probe_1
        q2["Q2<br/>f.txt:1-5"]:::probe_1
        a1["A1"]:::fail
        a2["A2"]:::fail
        q3["Q3<br/>f.txt:1-5"]:::probe_2
        q4["Q4<br/>f.txt:1-5"]:::probe_2
        q5["Q5<br/>f.txt:1-5"]:::probe_3
        q6["Q6<br/>f.txt:1-5"]:::probe_3
        foo --> q1
        foo --> q2
        q1 --> a1
        q2 --> a2
        foo --> q3
        foo --> q4
        foo --> q5
        foo --> q6
    end
    classDef probe_1,probe_2,probe_3 stroke:#4aa3ff
    classDef fail stroke:#f85149
`
	s := mustLoad(t, g, defaultCfg())
	cs := s.ConceptStatus("foo")
	if len(cs.FallbackProbes) != 2 {
		t.Errorf("FallbackProbes = %v, want [probe_2, probe_3]", cs.FallbackProbes)
	}
}

// --- Load errors ---

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/x.mmd", defaultCfg())
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_ParseError(t *testing.T) {
	path := writeTemp(t, "not a valid mmd file at all\n", ".mmd")
	_, err := Load(path, defaultCfg())
	if err == nil {
		t.Fatal("expected error for invalid mmd content")
	}
}

// --- Graph accessor ---

func TestGraph_NotNil(t *testing.T) {
	s := mustLoad(t, raftGraph, defaultCfg())
	if s.Graph() == nil {
		t.Error("Graph() should not return nil")
	}
}

func TestCfg_Roundtrip(t *testing.T) {
	cfg := Config{ProbeMin: 3, ProbeMax: 7, MaxFails: 5, MaxTeach: 10, MaxStall: 6}
	s := mustLoad(t, raftGraph, cfg)
	got := s.Cfg()
	if got.ProbeMin != 3 || got.MaxFails != 5 {
		t.Errorf("Cfg() did not return the loaded config: %+v", got)
	}
}

// --- envInt edge cases ---

func TestEnvInt_NegativeIsDefault(t *testing.T) {
	// Negative numbers are not isDigits, so fall back to default.
	t.Setenv("TM_PROBE_MIN", "-1")
	cfg := ConfigFromEnv()
	if cfg.ProbeMin != 2 {
		t.Errorf("negative env value should fall back to default, got %d", cfg.ProbeMin)
	}
}

func TestEnvInt_Zero(t *testing.T) {
	t.Setenv("TM_MAX_FAILS", "0")
	cfg := ConfigFromEnv()
	if cfg.MaxFails != 0 {
		t.Errorf("TM_MAX_FAILS=0 should give 0, got %d", cfg.MaxFails)
	}
}

// --- Reserve block ---

// reserveGraph is a minimal graph with one concept in each block.
// alpha (passed) → beta (untested), gamma (reserve) → beta.
const reserveGraph = `flowchart TB
    subgraph passed["Passed"]
        alpha["Alpha concept<br/>src.txt:1-5"]
    end
    subgraph untested["Untested"]
        beta["Beta concept<br/>src.txt:6-10"]
        alpha --"requires"--> beta
    end
    subgraph reserve["Concepts held in reserve"]
        gamma["Gamma concept<br/>src.txt:11-15"]
        gamma --"enables"--> beta
    end
    subgraph testing["Testing"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestReserve_ReturnsReserveIDs(t *testing.T) {
	s := mustLoad(t, reserveGraph, defaultCfg())
	got := s.Reserve()
	if len(got) != 1 || got[0] != "gamma" {
		t.Errorf("Reserve() = %v, want [gamma]", got)
	}
}

func TestReserve_EmptyWhenNoReserveConcepts(t *testing.T) {
	s := mustLoad(t, raftGraph, defaultCfg())
	got := s.Reserve()
	if len(got) != 0 {
		t.Errorf("Reserve() = %v, want []", got)
	}
}

func TestReserveParents_ReturnReserveParents(t *testing.T) {
	s := mustLoad(t, reserveGraph, defaultCfg())
	got := s.ReserveParents("beta")
	if len(got) != 1 || got[0] != "gamma" {
		t.Errorf("ReserveParents(beta) = %v, want [gamma]", got)
	}
}

func TestReserveParents_EmptyForConceptWithNoReserveParent(t *testing.T) {
	s := mustLoad(t, reserveGraph, defaultCfg())
	// alpha has no parents at all; its ReserveParents should be empty.
	got := s.ReserveParents("alpha")
	if len(got) != 0 {
		t.Errorf("ReserveParents(alpha) = %v, want []", got)
	}
}

func TestAncestorClosure_IncludesDirectAndTransitiveParents(t *testing.T) {
	// reserveGraph: alpha → beta (untested), gamma (reserve) → beta.
	// AncestorClosure("beta"): beta=0, alpha=1, gamma=1.
	s := mustLoad(t, reserveGraph, defaultCfg())
	dist := s.AncestorClosure("beta")
	if dist["beta"] != 0 {
		t.Errorf("want beta=0, got %d", dist["beta"])
	}
	if dist["alpha"] != 1 {
		t.Errorf("want alpha=1, got %d", dist["alpha"])
	}
	if dist["gamma"] != 1 {
		t.Errorf("want gamma=1, got %d", dist["gamma"])
	}
	if len(dist) != 3 {
		t.Errorf("AncestorClosure(beta) has %d entries, want 3: %v", len(dist), dist)
	}
}

func TestAncestorClosure_GoalAloneWhenNoParents(t *testing.T) {
	s := mustLoad(t, reserveGraph, defaultCfg())
	dist := s.AncestorClosure("alpha")
	if len(dist) != 1 || dist["alpha"] != 0 {
		t.Errorf("AncestorClosure(alpha) = %v, want {alpha:0}", dist)
	}
}

func TestFrontier_ReserveParentDoesNotBlock(t *testing.T) {
	// beta has parents: alpha (passed) and gamma (reserve).
	// Both are in passed-or-reserve, so beta IS on the frontier.
	s := mustLoad(t, reserveGraph, defaultCfg())
	frontier := s.Frontier()
	found := false
	for _, id := range frontier {
		if id == "beta" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("beta should be on frontier (all parents passed or reserve); Frontier()=%v", frontier)
	}
}
