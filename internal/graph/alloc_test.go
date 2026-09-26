package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ── Fallback (no NextMeta present) ──────────────────────────────────────────

// TestNextMeta_FallbackSeeding covers seedNextMeta: both Q and Batch counters
// are seeded from max N in the file when g.NextMeta is nil.
// Answers count toward Q seeding but not Batch seeding (only question Class fields count).
func TestNextMeta_FallbackSeeding(t *testing.T) {
	cases := []struct {
		name      string
		items     []TestingItem
		wantQ     int
		wantBatch int
	}{
		{"empty", nil, 1, 1},
		{
			"questions only",
			[]TestingItem{
				{Q: &QuestionNode{ID: "q3", Class: "probe_1"}},
				{Q: &QuestionNode{ID: "q1", Class: "probe_1"}},
			},
			4, 2,
		},
		{
			"answers only",
			[]TestingItem{
				{A: &AnswerNode{ID: "a5", Class: "pass"}},
				{A: &AnswerNode{ID: "a2", Class: "fail"}},
			},
			6, 1,
		},
		{
			"mixed Q and A",
			[]TestingItem{
				{Q: &QuestionNode{ID: "q3", Class: "probe_1"}},
				{A: &AnswerNode{ID: "a3", Class: "fail"}},
				{Q: &QuestionNode{ID: "q1", Class: "probe_1"}},
			},
			4, 2,
		},
		{
			"probe classes only",
			[]TestingItem{
				{Q: &QuestionNode{ID: "q1", Class: "probe_2"}},
				{Q: &QuestionNode{ID: "q2", Class: "probe_2"}},
			},
			3, 3,
		},
		{
			"mixed probe and teach",
			[]TestingItem{
				{Q: &QuestionNode{ID: "q1", Class: "probe_2"}},
				{Q: &QuestionNode{ID: "q3", Class: "teach_4"}},
			},
			4, 5,
		},
		{
			"answer Class ignored for batch",
			[]TestingItem{
				{Q: &QuestionNode{ID: "q1", Class: "probe_3"}},
				{A: &AnswerNode{ID: "a1", Class: "pass"}},
			},
			2, 4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &Graph{TestingItems: tc.items}
			if gotQ := NextQuestionN(g, ""); gotQ != tc.wantQ {
				t.Errorf("NextQuestionN got %d, want %d", gotQ, tc.wantQ)
			}
			if gotB := NextBatchN(g, ""); gotB != tc.wantBatch {
				t.Errorf("NextBatchN got %d, want %d", gotB, tc.wantBatch)
			}
		})
	}
}

// ── Counter (NextMeta present) ───────────────────────────────────────────────

func TestNextQuestionN_UsesCounter(t *testing.T) {
	// When NextMeta is present, the counter is returned regardless of file content.
	g := &Graph{
		NextMeta: &NextMeta{Q: 10, Batch: 5},
		TestingItems: []TestingItem{
			{Q: &QuestionNode{ID: "q3", Class: "probe_1"}},
		},
	}
	if got := NextQuestionN(g, ""); got != 10 {
		t.Errorf("NextQuestionN with counter=10: want 10, got %d", got)
	}
	// Counter must advance.
	if g.NextMeta.Q != 11 {
		t.Errorf("NextQuestionN counter after advance: want 11, got %d", g.NextMeta.Q)
	}
}

func TestNextBatchN_UsesCounter(t *testing.T) {
	g := &Graph{
		NextMeta: &NextMeta{Q: 7, Batch: 4},
		TestingItems: []TestingItem{
			{Q: &QuestionNode{ID: "q1", Class: "probe_2"}},
		},
	}
	if got := NextBatchN(g, ""); got != 4 {
		t.Errorf("NextBatchN with counter=4: want 4, got %d", got)
	}
	if g.NextMeta.Batch != 5 {
		t.Errorf("NextBatchN counter after advance: want 5, got %d", g.NextMeta.Batch)
	}
}

func TestAlloc_CounterAdvancesMonotonically(t *testing.T) {
	g := &Graph{NextMeta: &NextMeta{Q: 1, Batch: 1}}
	for i := 1; i <= 5; i++ {
		got := NextQuestionN(g, "")
		if got != i {
			t.Errorf("call %d: want %d, got %d", i, i, got)
		}
	}
	for i := 1; i <= 3; i++ {
		got := NextBatchN(g, "")
		if got != i {
			t.Errorf("batch call %d: want %d, got %d", i, i, got)
		}
	}
}

// ── Seeding from the event log ───────────────────────────────────────────────

func writeLogFile(t *testing.T, path string, rows []map[string]any) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			t.Fatalf("encode log row: %v", err)
		}
	}
}

func TestAlloc_SeedsFromLog_HigherThanFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "graph.mmd.jsonl")
	// Log contains q7 and probe_3 — higher than anything in the file.
	writeLogFile(t, logPath, []map[string]any{
		{"ev": "q", "q": "q7", "batch": "probe_3"},
	})

	g := &Graph{
		TestingItems: []TestingItem{
			{Q: &QuestionNode{ID: "q2", Class: "probe_1"}},
		},
	}
	// Q seeding: max(2, 7)+1 = 8.
	got := NextQuestionN(g, logPath)
	if got != 8 {
		t.Errorf("NextQuestionN seed from log: want 8, got %d", got)
	}
	// Batch seeding: max(1, 3)+1 = 4.
	bGot := NextBatchN(g, logPath)
	if bGot != 4 {
		t.Errorf("NextBatchN seed from log: want 4, got %d", bGot)
	}
}

func TestAlloc_SeedsFromLog_MissingLogOK(t *testing.T) {
	// A nonexistent log path is silently treated as log max = 0.
	g := &Graph{
		TestingItems: []TestingItem{
			{Q: &QuestionNode{ID: "q5", Class: "probe_2"}},
		},
	}
	got := NextQuestionN(g, "/nonexistent/path.jsonl")
	if got != 6 {
		t.Errorf("NextQuestionN missing log: want 6, got %d", got)
	}
}

func TestAlloc_SeedsFromLog_AnswerIDs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "graph.mmd.jsonl")
	// Log contains answer id a9.
	writeLogFile(t, logPath, []map[string]any{
		{"ev": "answer", "id": "a9"},
	})

	g := &Graph{}
	got := NextQuestionN(g, logPath)
	if got != 10 {
		t.Errorf("NextQuestionN from log with a9: want 10, got %d", got)
	}
}

func TestAlloc_LogScanOnlyOnFirstAlloc(t *testing.T) {
	// Once NextMeta is set (line present in file), the log is never read.
	// We pass a nonexistent log path, but NextMeta is pre-populated.
	g := &Graph{NextMeta: &NextMeta{Q: 20, Batch: 10}}
	got := NextQuestionN(g, "/no/such/file")
	if got != 20 {
		t.Errorf("want counter value 20, got %d", got)
	}
}

func TestAlloc_SeedsFromLog_NestedArray(t *testing.T) {
	// Exercises the []any branch in scanLogValue (e.g. gc event nodes array).
	dir := t.TempDir()
	logPath := filepath.Join(dir, "graph.mmd.jsonl")
	// nodes array containing maps with id fields like the gc event.
	writeLogFile(t, logPath, []map[string]any{
		{"ev": "gc", "nodes": []any{
			map[string]any{"id": "q6", "class": "probe_2"},
			map[string]any{"id": "a6", "class": "pass"},
		}},
	})
	g := &Graph{}
	got := NextQuestionN(g, logPath)
	// Q: max(0, 6)+1 = 7. Batch: max(0, 2)+1 = 3.
	if got != 7 {
		t.Errorf("NextQuestionN from log nested array: want 7, got %d", got)
	}
	bGot := NextBatchN(g, logPath)
	if bGot != 3 {
		t.Errorf("NextBatchN from log nested array: want 3, got %d", bGot)
	}
}

func TestAlloc_SeedsFromLog_MalformedLineIgnored(t *testing.T) {
	// A malformed JSON line is skipped; valid lines after it still count.
	dir := t.TempDir()
	logPath := filepath.Join(dir, "graph.mmd.jsonl")
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	// Write a bad line then a valid line.
	_, _ = f.WriteString("not valid json\n")
	enc := json.NewEncoder(f)
	_ = enc.Encode(map[string]any{"ev": "q", "q": "q4", "batch": "probe_2"})
	_ = f.Close()

	g := &Graph{}
	got := NextQuestionN(g, logPath)
	if got != 5 {
		t.Errorf("NextQuestionN with malformed line: want 5, got %d", got)
	}
}
