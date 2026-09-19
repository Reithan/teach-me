package graph

// NextQuestionN returns the lowest available question/answer suffix N for a
// new node (global max of all qN and aN suffixes in g.TestingItems, plus 1).
// Returns 1 when the testing block contains no questions or answers.
func NextQuestionN(g *Graph) int {
	maxN := 0
	for _, item := range g.TestingItems {
		if item.Q != nil {
			if n := QuestionN(item.Q.ID); n > maxN {
				maxN = n
			}
		}
		if item.A != nil {
			if n := QuestionN(item.A.ID); n > maxN {
				maxN = n
			}
		}
	}
	return maxN + 1
}

// NextBatchN returns the lowest available batch number (global max N across
// all probe_N and teach_N class IDs found in g.TestingItems, plus 1).
// Returns 1 when no batches exist.
func NextBatchN(g *Graph) int {
	maxN := 0
	for _, item := range g.TestingItems {
		if item.Q != nil {
			if n := BatchN(item.Q.Class); n > maxN {
				maxN = n
			}
		}
	}
	return maxN + 1
}
