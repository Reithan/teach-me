package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/graph"
)

// BatchStatus identifies the lifecycle state of a question batch per spec §5.
type BatchStatus int

const (
	// BatchDraft means no answers exist; for a probe batch, no higher-numbered
	// teach batch exists under the same concept.
	BatchDraft BatchStatus = iota

	// BatchLocked means this is a probe batch with no answers and a
	// higher-numbered teach batch exists under the same concept.
	BatchLocked

	// BatchOpen means at least one answer exists but not every question is
	// answered and graded.
	BatchOpen

	// BatchResolved means every question has an answer and every answer is
	// graded (class is pass, fail, or unclear — not pending).
	BatchResolved
)

// ConceptStatusResult holds all §5 derived facts for a single untested concept.
type ConceptStatusResult struct {
	// ID is the concept identifier.
	ID string

	// Base is the gate base N from the %% tm:gate meta line, or 0 when no gate
	// line exists. Batches at or below Base do not count toward gate triggers.
	Base int

	// FailedProbeBatches lists the probe batch class IDs above Base that hold
	// at least one fail answer, sorted by batch N ascending.
	FailedProbeBatches []string

	// FallbackProbes lists unanswered probe batches whose N is above Base and
	// above some failed probe batch's N, sorted by batch N ascending.
	FallbackProbes []string

	// OpenTargets lists the failed probe question IDs (teaching targets) with
	// no passing, in-scope teach question targeting them yet, sorted ascending.
	OpenTargets []string

	// StallStreak is the count of in-scope teach questions in the unbroken run
	// of most-recent resolved teach batches above Base that each resolved with
	// zero pass. Any resolved batch with a pass breaks the run.
	StallStreak int

	// Stalled is true when StallStreak >= Config.MaxStall.
	Stalled bool

	// TeachCount is the count of in-scope teach questions under the concept in
	// teach batches whose N is above the latest failed probe batch above Base.
	TeachCount int

	// TeachingSpent is true when TeachCount >= Config.MaxTeach.
	TeachingSpent bool

	// LatestTeachNotAllPass is true when either (a) no teach batch above Base
	// exists yet, or (b) the latest teach batch above Base has at least one
	// in-scope question whose answer is not "pass" (or the question is unanswered).
	// Used by ask/answer for the teaching-incomplete refusal (§7, §8.6–8.9).
	LatestTeachNotAllPass bool

	// Gated is true when len(FailedProbeBatches) >= Config.MaxFails or Stalled.
	Gated bool
}

// State holds the fully derived state of a graph file. It is immutable after
// construction via Load.
type State struct {
	g   *graph.Graph
	cfg Config

	// --- indexes built at load time ---

	qByID   map[string]*graph.QuestionNode
	aByID   map[string]*graph.AnswerNode
	inEdges map[string][]*graph.Edge // node ID → its incoming edges

	// Concept membership.
	passedSet   map[string]bool
	untestedSet map[string]bool
	reserveSet  map[string]bool
	allConcepts map[string]bool // passedSet ∪ untestedSet ∪ reserveSet

	// Gate base per concept (from %% tm:gate meta lines; 0 when absent).
	gateBase map[string]int

	// Batch indexes.
	batchOrder     []string                         // all batch class IDs in declaration order
	batchQs        map[string][]*graph.QuestionNode // batch class → questions (declaration order)
	batchConcept   map[string]string                // batch class → concept ID
	conceptBatches map[string][]string              // concept ID → batch class IDs sorted by N ascending

	// Per-question caches.
	qConceptOf   map[string]string // qID → concept ID
	qTeachTarget map[string]string // teach qID → root probe qID (the teaching target)
}

// Load reads the graph file at path, parses it, and returns the fully computed
// derived state. A parse error propagates directly; Load does not re-run lint.
// cfg.SrcRoot is filled from cite.SrcRoot(graphDir) when it is empty.
func Load(file string, cfg Config) (*State, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	g, err := graph.Parse(data)
	if err != nil {
		return nil, err
	}
	if cfg.SrcRoot == "" {
		cfg.SrcRoot = cite.SrcRoot(filepath.Dir(file))
	}
	return buildState(g, cfg), nil
}

// LoadFromGraph builds a fully derived State from an already-parsed in-memory
// graph (no file I/O). cfg.SrcRoot should be set by the caller when cite
// resolution is needed. Used by answer --override to re-derive state after a
// gate clear without touching the on-disk file.
func LoadFromGraph(g *graph.Graph, cfg Config) *State {
	return buildState(g, cfg)
}

// buildState is the common implementation behind Load and LoadFromGraph.
// It builds all derived indexes from g and cfg.
func buildState(g *graph.Graph, cfg Config) *State {
	s := &State{
		g:              g,
		cfg:            cfg,
		qByID:          make(map[string]*graph.QuestionNode),
		aByID:          make(map[string]*graph.AnswerNode),
		inEdges:        make(map[string][]*graph.Edge),
		passedSet:      make(map[string]bool),
		untestedSet:    make(map[string]bool),
		reserveSet:     make(map[string]bool),
		allConcepts:    make(map[string]bool),
		gateBase:       make(map[string]int),
		batchQs:        make(map[string][]*graph.QuestionNode),
		batchConcept:   make(map[string]string),
		conceptBatches: make(map[string][]string),
		qConceptOf:     make(map[string]string),
		qTeachTarget:   make(map[string]string),
	}

	// Concept membership.
	for _, c := range g.PassedConcepts {
		s.passedSet[c.ID] = true
		s.allConcepts[c.ID] = true
	}
	for _, c := range g.UntestedConcepts {
		s.untestedSet[c.ID] = true
		s.allConcepts[c.ID] = true
	}
	for _, c := range g.ReserveConcepts {
		s.reserveSet[c.ID] = true
		s.allConcepts[c.ID] = true
	}

	// Gate base from meta lines.
	for _, m := range g.UntestedMetas {
		s.gateBase[m.Concept] = m.Base
	}

	// Q/A indexes.
	for _, item := range g.TestingItems {
		if item.Q != nil {
			s.qByID[item.Q.ID] = item.Q
		}
		if item.A != nil {
			s.aByID[item.A.ID] = item.A
		}
	}

	// Incoming-edge index.
	for _, e := range g.Edges {
		s.inEdges[e.To] = append(s.inEdges[e.To], e)
	}

	// Batch indexes — preserve declaration order.
	seen := make(map[string]bool)
	for _, item := range g.TestingItems {
		if item.Q == nil {
			continue
		}
		q := item.Q
		if !graph.ValidBatchID(q.Class) {
			continue
		}
		if !seen[q.Class] {
			seen[q.Class] = true
			s.batchOrder = append(s.batchOrder, q.Class)
		}
		s.batchQs[q.Class] = append(s.batchQs[q.Class], q)
	}

	// Resolve the concept for each batch (lint ensures all questions agree).
	for _, batch := range s.batchOrder {
		qs := s.batchQs[batch]
		if len(qs) == 0 {
			continue
		}
		cID, err := s.resolveConcept(qs[0].ID)
		if err != nil {
			continue // malformed graph; lint would flag this
		}
		s.batchConcept[batch] = cID
	}

	// Group batches per concept sorted by N ascending.
	for _, batch := range s.batchOrder {
		cID, ok := s.batchConcept[batch]
		if !ok {
			continue
		}
		s.conceptBatches[cID] = append(s.conceptBatches[cID], batch)
	}
	for cID, batches := range s.conceptBatches {
		sort.Slice(batches, func(i, j int) bool {
			return graph.BatchN(batches[i]) < graph.BatchN(batches[j])
		})
		s.conceptBatches[cID] = batches
	}

	// Cache per-question concept.
	for qID := range s.qByID {
		if cID, err := s.resolveConcept(qID); err == nil {
			s.qConceptOf[qID] = cID
		}
	}

	// Cache teaching targets for teach questions.
	for qID, q := range s.qByID {
		if !graph.IsTeachClass(q.Class) {
			continue
		}
		if rootQ, _ := s.walkToRootProbe(qID); rootQ != nil {
			s.qTeachTarget[qID] = rootQ.ID
		}
	}

	return s
}

// --- §5 Concept classification ---

// Frontier returns the IDs of untested concepts whose every concept-parent is
// in passed, sorted ascending. A concept with no parents is also on the
// frontier.
func (s *State) Frontier() []string {
	var result []string
	for _, c := range s.g.UntestedConcepts {
		if s.isOnFrontier(c.ID) {
			result = append(result, c.ID)
		}
	}
	sort.Strings(result)
	return result
}

// Blocked returns the IDs of untested concepts that have at least one
// concept-parent not in passed, sorted ascending.
func (s *State) Blocked() []string {
	var result []string
	for _, c := range s.g.UntestedConcepts {
		if !s.isOnFrontier(c.ID) {
			result = append(result, c.ID)
		}
	}
	sort.Strings(result)
	return result
}

// Reserve returns the IDs of concepts in the reserve block, sorted ascending.
func (s *State) Reserve() []string {
	result := make([]string, 0, len(s.g.ReserveConcepts))
	for _, c := range s.g.ReserveConcepts {
		result = append(result, c.ID)
	}
	sort.Strings(result)
	return result
}

// ReserveParents returns the IDs of a concept's parents that are in reserve,
// sorted ascending. These are prerequisites the graph records but the current
// session does not enforce (spec §5 "Reserve parents").
func (s *State) ReserveParents(conceptID string) []string {
	var result []string
	for _, e := range s.inEdges[conceptID] {
		if s.reserveSet[e.From] {
			result = append(result, e.From)
		}
	}
	sort.Strings(result)
	return result
}

// AncestorClosure returns the set of concepts reachable from goal by walking
// parent edges (incoming concept→concept edges) through every block. The map
// value is the BFS hop distance from goal (goal itself = 0).
func (s *State) AncestorClosure(goal string) map[string]int {
	dist := map[string]int{goal: 0}
	queue := []string{goal}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range s.inEdges[cur] {
			if !s.allConcepts[e.From] {
				continue
			}
			if _, seen := dist[e.From]; !seen {
				dist[e.From] = dist[cur] + 1
				queue = append(queue, e.From)
			}
		}
	}
	return dist
}

// Open returns the IDs of untested concepts that have at least one question,
// sorted ascending.
func (s *State) Open() []string {
	var result []string
	for _, c := range s.g.UntestedConcepts {
		if len(s.conceptBatches[c.ID]) > 0 {
			result = append(result, c.ID)
		}
	}
	sort.Strings(result)
	return result
}

// isOnFrontier reports whether the untested concept conceptID is on the
// frontier (all concept-parents are in passed or reserve, per spec §5).
// A reserve parent does not block the frontier.
func (s *State) isOnFrontier(conceptID string) bool {
	for _, e := range s.inEdges[conceptID] {
		if s.allConcepts[e.From] && !s.passedSet[e.From] && !s.reserveSet[e.From] {
			return false
		}
	}
	return true
}

// --- §5 Question/concept mapping ---

// ConceptOf returns the concept ID that question qid resolves to by walking
// its incoming-edge chain. Returns ("", false) when the question is not known
// or the chain cannot be resolved.
func (s *State) ConceptOf(qid string) (string, bool) {
	c, ok := s.qConceptOf[qid]
	return c, ok
}

// --- §5 Batch state ---

// BatchStateOf returns the §5 status of the batch identified by batchClass.
// Returns BatchDraft for unknown batch IDs.
func (s *State) BatchStateOf(batchClass string) BatchStatus {
	qs, ok := s.batchQs[batchClass]
	if !ok || len(qs) == 0 {
		return BatchDraft
	}

	// Determine answered/graded status across all questions.
	hasAnswer := false
	allGraded := true
	for _, q := range qs {
		a := s.answerFor(q.ID)
		if a == nil {
			allGraded = false
		} else {
			hasAnswer = true
			if a.Class == "pending" {
				allGraded = false
			}
		}
	}

	if hasAnswer {
		if allGraded {
			return BatchResolved
		}
		return BatchOpen
	}

	// No answers. For probe batches, check whether a higher-numbered teach batch
	// exists under the same concept AND is above the gate base (§7 gate paragraph:
	// "Probes left unanswered from before the gate stop being fallback probes and
	// are asked as written"). A teach batch at or below the gate base no longer
	// locks any probe batch (Fix 1).
	if graph.IsProbeClass(batchClass) {
		probeN := graph.BatchN(batchClass)
		cID := s.batchConcept[batchClass]
		base := s.gateBase[cID]
		for _, other := range s.conceptBatches[cID] {
			if graph.IsTeachClass(other) && graph.BatchN(other) > probeN && graph.BatchN(other) > base {
				return BatchLocked
			}
		}
	}

	return BatchDraft
}

// --- §5 Teaching target ---

// TeachingTarget returns the probe question ID that teach question teachQID
// leads back to when its --re chain is walked to the root. Returns ("", false)
// when teachQID is not a known teach question or its chain is malformed.
func (s *State) TeachingTarget(teachQID string) (string, bool) {
	t, ok := s.qTeachTarget[teachQID]
	return t, ok
}

// --- §5 Per-concept derived facts ---

// ConceptStatus computes all §5 derived facts for the given concept. The
// returned struct is meaningful for untested concepts; for passed concepts most
// fields will be zero/empty.
func (s *State) ConceptStatus(conceptID string) ConceptStatusResult {
	r := ConceptStatusResult{
		ID:   conceptID,
		Base: s.gateBase[conceptID],
	}
	base := r.Base

	batches := s.conceptBatches[conceptID] // sorted by N ascending

	// Partition into probe and teach batches above base.
	var probeBatches, teachBatches []string
	for _, b := range batches {
		if graph.BatchN(b) <= base {
			continue
		}
		if graph.IsProbeClass(b) {
			probeBatches = append(probeBatches, b)
		} else {
			teachBatches = append(teachBatches, b)
		}
	}

	// LatestTeachNotAllPass: true when there are no teach batches above base,
	// or the latest one has an in-scope question not yet graded "pass" (§7,
	// §8.6–8.9; Fix 2).
	if len(teachBatches) == 0 {
		r.LatestTeachNotAllPass = true
	} else {
		latest := teachBatches[len(teachBatches)-1]
		latestAllPass := true
		for _, q := range s.batchQs[latest] {
			a := s.answerFor(q.ID)
			inScope := a == nil || !a.OOS
			if inScope && (a == nil || a.Class != "pass") {
				latestAllPass = false
				break
			}
		}
		r.LatestTeachNotAllPass = !latestAllPass
	}

	// Failed probe batches: probe batches above base with ≥1 fail answer.
	for _, pb := range probeBatches {
		if s.batchHasFail(pb) {
			r.FailedProbeBatches = append(r.FailedProbeBatches, pb)
		}
	}

	// Fallback probes: unanswered probe batches that follow some failed probe
	// above base (i.e., the fallback's N > any failed probe's N).
	if len(r.FailedProbeBatches) > 0 {
		for _, pb := range probeBatches {
			if s.batchHasAnyAnswer(pb) {
				continue
			}
			pN := graph.BatchN(pb)
			for _, failedPb := range r.FailedProbeBatches {
				if pN > graph.BatchN(failedPb) {
					r.FallbackProbes = append(r.FallbackProbes, pb)
					break
				}
			}
		}
	}

	// Open targets: failed probe questions above base with no passing,
	// in-scope teach question targeting them. Only failed probes need a
	// teaching round (§8.6); an unclear probe takes a replacement probe
	// instead (§8.5, no teaching), and an unclear that recurs is recorded
	// as fail (§8.2), so it re-enters here via its fail class.
	//
	// Build a set of probe question IDs that already have a passing in-scope
	// teach question.
	targetHasPass := make(map[string]bool)
	for _, tb := range teachBatches {
		for _, q := range s.batchQs[tb] {
			target, ok := s.qTeachTarget[q.ID]
			if !ok {
				continue
			}
			a := s.answerFor(q.ID)
			inScope := a == nil || !a.OOS
			if inScope && a != nil && a.Class == "pass" {
				targetHasPass[target] = true
			}
		}
	}
	for _, pb := range probeBatches {
		for _, q := range s.batchQs[pb] {
			a := s.answerFor(q.ID)
			if a == nil {
				continue
			}
			if a.Class == "fail" {
				if !targetHasPass[q.ID] {
					r.OpenTargets = append(r.OpenTargets, q.ID)
				}
			}
		}
	}
	sort.Strings(r.OpenTargets)

	// Stall streak: walk teach batches above base from highest N downward.
	// Count in-scope questions in the consecutive run of resolved zero-pass
	// batches. Any resolved batch with a pass (or unresolved batch) breaks the run.
	streak := 0
	for i := len(teachBatches) - 1; i >= 0; i-- {
		tb := teachBatches[i]
		if s.BatchStateOf(tb) != BatchResolved {
			break
		}
		if s.batchInScopePasses(tb) > 0 {
			break
		}
		streak += s.batchInScopeCount(tb)
	}
	r.StallStreak = streak
	r.Stalled = streak >= s.cfg.MaxStall

	// Teach count: in-scope teach questions in teach batches whose N is greater
	// than the latest (highest-N) failed probe above base.
	latestFailedN := 0
	for _, pb := range r.FailedProbeBatches {
		if n := graph.BatchN(pb); n > latestFailedN {
			latestFailedN = n
		}
	}
	teachCount := 0
	for _, tb := range teachBatches {
		if graph.BatchN(tb) > latestFailedN {
			teachCount += s.batchInScopeCount(tb)
		}
	}
	r.TeachCount = teachCount
	r.TeachingSpent = teachCount >= s.cfg.MaxTeach

	// Gated.
	r.Gated = len(r.FailedProbeBatches) >= s.cfg.MaxFails || r.Stalled

	return r
}

// Graph returns the underlying parsed graph. Callers that need raw model
// access (e.g. the read commands) use this.
func (s *State) Graph() *graph.Graph {
	return s.g
}

// Cfg returns the effective config used to load this state.
func (s *State) Cfg() Config {
	return s.cfg
}

// --- internal helpers ---

// resolveConcept walks the incoming-edge chain from qid upward until it
// reaches a concept node. Mirrors lint.resolveConcept with the same semantics.
func (s *State) resolveConcept(qid string) (string, error) {
	visited := make(map[string]bool)
	cur := qid
	for {
		if visited[cur] {
			return "", fmt.Errorf("cycle at %q", cur)
		}
		visited[cur] = true

		edges := s.inEdges[cur]
		if len(edges) != 1 {
			return "", fmt.Errorf("node %q has %d incoming edges", cur, len(edges))
		}
		src := edges[0].From
		if s.allConcepts[src] {
			return src, nil
		}
		a, ok := s.aByID[src]
		if !ok {
			return "", fmt.Errorf("source %q of %q is not a concept or answer", src, cur)
		}
		n := graph.QuestionN(a.ID)
		if n == 0 {
			return "", fmt.Errorf("invalid answer ID %q", a.ID)
		}
		nextQ := fmt.Sprintf("q%d", n)
		if _, exists := s.qByID[nextQ]; !exists {
			return "", fmt.Errorf("question %q not found", nextQ)
		}
		cur = nextQ
	}
}

// walkToRootProbe follows the incoming-edge chain from a question until it
// finds a probe whose single incoming edge comes from a concept (the root
// probe / teaching target). Returns (nil, nil) on structural failure.
// Mirrors lint.walkToRootProbe.
func (s *State) walkToRootProbe(qid string) (*graph.QuestionNode, *graph.AnswerNode) {
	visited := make(map[string]bool)
	cur := qid
	for {
		if visited[cur] {
			return nil, nil
		}
		visited[cur] = true

		edges := s.inEdges[cur]
		if len(edges) != 1 {
			return nil, nil
		}
		srcID := edges[0].From
		a, isAnswer := s.aByID[srcID]
		if !isAnswer {
			return nil, nil
		}
		n := graph.QuestionN(a.ID)
		if n == 0 {
			return nil, nil
		}
		qXID := fmt.Sprintf("q%d", n)
		qX, ok := s.qByID[qXID]
		if !ok {
			return nil, nil
		}
		// The teaching target is any probe in the chain — including replacement
		// probes (incoming from an unclear answer rather than a concept).
		// Walking through a replacement probe would return the wrong ancestor.
		if graph.IsProbeClass(qX.Class) {
			return qX, a
		}
		cur = qXID
	}
}

// answerFor returns the answer node for question qid (aN where N matches qN),
// or nil when no answer exists.
func (s *State) answerFor(qid string) *graph.AnswerNode {
	n := graph.QuestionN(qid)
	if n == 0 {
		return nil
	}
	return s.aByID[fmt.Sprintf("a%d", n)]
}

// batchHasFail reports whether any question in batchClass has a fail answer.
func (s *State) batchHasFail(batchClass string) bool {
	for _, q := range s.batchQs[batchClass] {
		a := s.answerFor(q.ID)
		if a != nil && a.Class == "fail" {
			return true
		}
	}
	return false
}

// batchHasAnyAnswer reports whether any question in batchClass has an answer.
func (s *State) batchHasAnyAnswer(batchClass string) bool {
	for _, q := range s.batchQs[batchClass] {
		if s.answerFor(q.ID) != nil {
			return true
		}
	}
	return false
}

// batchInScopePasses returns the count of in-scope pass answers in a batch.
// In-scope: answer is not marked OOS.
func (s *State) batchInScopePasses(batchClass string) int {
	count := 0
	for _, q := range s.batchQs[batchClass] {
		a := s.answerFor(q.ID)
		if a != nil && !a.OOS && a.Class == "pass" {
			count++
		}
	}
	return count
}

// batchInScopeCount returns the count of in-scope teach questions in a batch.
// In-scope: no answer, or answer is not marked OOS.
func (s *State) batchInScopeCount(batchClass string) int {
	count := 0
	for _, q := range s.batchQs[batchClass] {
		a := s.answerFor(q.ID)
		if a == nil || !a.OOS {
			count++
		}
	}
	return count
}
