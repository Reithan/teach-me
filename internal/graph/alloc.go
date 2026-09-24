package graph

import (
	"bytes"
	"encoding/json"
	"os"
)

// NextQuestionN returns the next available question/answer suffix N for a new
// node. When g.NextMeta is present, it returns g.NextMeta.Q and advances the
// counter. On the first call for a file without the line, it seeds from
// max(max N in file, max N in the event log at logPath) + 1, initialises
// g.NextMeta for both Q and Batch, then returns the seeded value.
//
// logPath is the path to the event log (<graph>.mmd.jsonl). An empty or
// unreadable path is treated as log max = 0.
func NextQuestionN(g *Graph, logPath string) int {
	seedNextMeta(g, logPath)
	n := g.NextMeta.Q
	g.NextMeta.Q++
	return n
}

// NextBatchN returns the next available batch number (probe_N / teach_N suffix).
// It follows the same seeding and counter-advance rules as NextQuestionN.
func NextBatchN(g *Graph, logPath string) int {
	seedNextMeta(g, logPath)
	n := g.NextMeta.Batch
	g.NextMeta.Batch++
	return n
}

// seedNextMeta initialises g.NextMeta when it is absent by seeding both the Q
// and Batch counters from the file and the event log.
func seedNextMeta(g *Graph, logPath string) {
	if g.NextMeta != nil {
		return
	}
	fileQMax := maxQuestionNInFile(g)
	fileBMax := maxBatchNInFile(g)
	logQMax, logBMax := maxFromLog(logPath)
	qNext := intMax(fileQMax, logQMax) + 1
	bNext := intMax(fileBMax, logBMax) + 1
	g.NextMeta = &NextMeta{Q: qNext, Batch: bNext}
}

// maxQuestionNInFile returns the maximum numeric suffix among all qN and aN IDs
// in g.TestingItems. Returns 0 when none exist.
func maxQuestionNInFile(g *Graph) int {
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
	return maxN
}

// maxBatchNInFile returns the maximum numeric suffix across all probe_N and
// teach_N class IDs found in g.TestingItems. Returns 0 when none exist.
func maxBatchNInFile(g *Graph) int {
	maxN := 0
	for _, item := range g.TestingItems {
		if item.Q != nil {
			if n := BatchN(item.Q.Class); n > maxN {
				maxN = n
			}
		}
	}
	return maxN
}

// maxFromLog scans every JSONL row in logPath and returns the maximum qN/aN
// suffix and the maximum probe_N/teach_N suffix found across all string values.
// Returns (0, 0) when logPath is empty, missing, or unreadable.
func maxFromLog(logPath string) (qMax, bMax int) {
	if logPath == "" {
		return 0, 0
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return 0, 0
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}
		scanLogRow(row, &qMax, &bMax)
	}
	return qMax, bMax
}

// scanLogRow walks all string values in the map (including nested maps and
// arrays) and updates the max counters. JSON arrays unmarshal as []any.
func scanLogRow(m map[string]any, qMax, bMax *int) {
	for _, v := range m {
		scanLogValue(v, qMax, bMax)
	}
}

// scanLogValue recurses into any JSON value and extracts max IDs.
func scanLogValue(v any, qMax, bMax *int) {
	switch val := v.(type) {
	case string:
		if ValidQuestionID(val) || ValidAnswerID(val) {
			if n := QuestionN(val); n > *qMax {
				*qMax = n
			}
		}
		if n := BatchN(val); n > *bMax {
			*bMax = n
		}
	case map[string]any:
		for _, child := range val {
			scanLogValue(child, qMax, bMax)
		}
	case []any:
		for _, elem := range val {
			scanLogValue(elem, qMax, bMax)
		}
	}
}

// intMax returns the larger of a and b.
func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
