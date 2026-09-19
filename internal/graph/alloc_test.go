package graph

import "testing"

func TestNextQuestionN_Empty(t *testing.T) {
	g := &Graph{}
	if got := NextQuestionN(g); got != 1 {
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
	if got := NextQuestionN(g); got != 4 {
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
	if got := NextQuestionN(g); got != 6 {
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
	if got := NextQuestionN(g); got != 4 {
		t.Errorf("NextQuestionN q1,q3,a3: want 4, got %d", got)
	}
}

func TestNextBatchN_Empty(t *testing.T) {
	g := &Graph{}
	if got := NextBatchN(g); got != 1 {
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
	if got := NextBatchN(g); got != 3 {
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
	if got := NextBatchN(g); got != 5 {
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
	if got := NextBatchN(g); got != 4 {
		t.Errorf("NextBatchN probe_3 with answer: want 4, got %d", got)
	}
}
