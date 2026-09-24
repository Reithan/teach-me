package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ── Fallback (no NextMeta present) ──────────────────────────────────────────

func TestNextQuestionN_Empty(t *testing.T) {
	g := &Graph{}
	if got := NextQuestionN(g, ""); got != 1 {
		t.Errorf("NextQuestionN empty graph: want 1, got %d", got)
	}
}

func TestNextQuestionN_QuestionsOnly(t *testing.T) {
	g := &Graph{
		TestingItems: []TestingItem{
			{Q: &QuestionNode{ID: "q3", Class: "probe_1"}},
			{Q: &QuestionNode{ID: "q1", Class: "probe_1"}},
		},
	}
	if got := NextQuestionN(g, ""); got != 4 {
		t.Errorf("NextQuestionN q1,q3: want 4, got %d", got)
	}
}

func TestNextQuestionN_AnswersOnly(t *testing.T) {
	g := &Graph{
		TestingItems: []TestingItem{
			{A: &AnswerNode{ID: "a5", Class: "pass"}},
			{A: &AnswerNode{ID: "a2", Class: "fail"}},
		},
	}
	if got := NextQuestionN(g, ""); got != 6 {
		t.Errorf("NextQuestionN a2,a5: want 6, got %d", got)
	}
}

func TestNextQuestionN_MixedQA(t *testing.T) {
	g := &Graph{
		TestingItems: []TestingItem{
			{Q: &QuestionNode{ID: "q3", Class: "probe_1"}},
			{A: &AnswerNode{ID: "a3", Class: "fail"}},
			{Q: &QuestionNode{ID: "q1", Class: "probe_1"}},
		},
	}
	if got := NextQuestionN(g, ""); got != 4 {
		t.Errorf("NextQuestionN q1,q3,a3: want 4, got %d", got)
	}
}

func TestNextBatchN_Empty(t *testing.T) {
	g := &Graph{}
	if got := NextBatchN(g, ""); got != 1 {
		t.Errorf("NextBatchN empty graph: want 1, got %d", got)
	}
}

func TestNextBatchN_ProbeOnly(t *testing.T) {
	g := &Graph{
		TestingItems: []TestingItem{
			{Q: &QuestionNode{ID: "q1", Class: "probe_2"}},
			{Q: &QuestionNode{ID: "q2", Class: "probe_2"}},
		},
	}
	if got := NextBatchN(g, ""); got != 3 {
		t.Errorf("NextBatchN probe_2: want 3, got %d", got)
	}
}

func TestNextBatchN_Mixed(t *testing.T) {
	g := &Graph{
		TestingItems: []TestingItem{
			{Q: &QuestionNode{ID: "q1", Class: "probe_2"}},
			{Q: &QuestionNode{ID: "q3", Class: "teach_4"}},
		},
	}
	if got := NextBatchN(g, ""); got != 5 {
		t.Errorf("NextBatchN probe_2,teach_4: want 5, got %d", got)
	}
}

func TestNextBatchN_AnswersIgnored(t *testing.T) {
	// Answers have no batch class; only question Class fields count.
	g := &Graph{
		TestingItems: []TestingItem{
			{Q: &QuestionNode{ID: "q1", Class: "probe_3"}},
			{A: &AnswerNode{ID: "a1", Class: "pass"}},
		},
	}
	if got := NextBatchN(g, ""); got != 4 {
		t.Errorf("NextBatchN probe_3 with answer: want 4, got %d", got)
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
