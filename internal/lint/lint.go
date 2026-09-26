// Package lint implements the §11 graph validation checks for .mmd files.
package lint

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/graph"
)

// Violation is a single lint finding. Line is the 1-based source line number;
// 0 means file-level (no specific line applies).
type Violation struct {
	Line int
	Msg  string
}

// Config holds the runtime configuration for lint size checks.
type Config struct {
	SrcRoot  string
	AidsDir  string
	ProbeMin int
	ProbeMax int
	TeachMin int
	TeachMax int
}

// Check validates data against the §11 lint rules and returns all violations.
// The returned slice is nil on a valid graph. Violations are sorted by Line
// ascending, with Line==0 entries last; order is stable within a line.
func Check(data []byte, cfg Config) []Violation {
	// §11.1: the file must be valid UTF-8.
	if !utf8.Valid(data) {
		return []Violation{{Line: 0, Msg: "file is not valid UTF-8"}}
	}

	g, err := graph.Parse(data)
	if err != nil {
		// If Parse returns an error, checks 2, 3, and 4 cannot be verified
		// individually. A broken parse yields no reliable model, so we return a
		// single file-level violation and stop — any other violations would be
		// spurious artefacts of the malformed input.
		return []Violation{{Line: 0, Msg: "invalid graph: " + err.Error()}}
	}
	// On a successful parse, checks 2 and 3 are satisfied by construction.

	// Build shared indexes used by multiple checks.
	allConcepts := buildConceptSet(g)
	qByID, aByID := buildQAMaps(g)
	inEdges := buildInEdges(g)
	blocks := buildBlockMap(g)

	// Run all checks and collect results. Two passes: first run to get total
	// count for preallocation, second pass to build the final slice.
	perCheck := [][]Violation{
		check4(g),
		check5(data, blocks),
		check6(g),
		check7(g),
		check8(g, inEdges, qByID, aByID, allConcepts),
		check9(g, cfg, inEdges, qByID, aByID, allConcepts),
		check10(g, allConcepts),
		check11(g, cfg),
		check12(g, allConcepts, inEdges, qByID, aByID),
		check15(g),
		check16(g, cfg),
		check17(g),
	}
	total := 0
	for _, r := range perCheck {
		total += len(r)
	}
	viols := make([]Violation, 0, total)
	for _, r := range perCheck {
		viols = append(viols, r...)
	}

	sort.SliceStable(viols, func(i, j int) bool {
		li, lj := viols[i].Line, viols[j].Line
		if li == 0 && lj == 0 {
			return false
		}
		if li == 0 {
			return false
		}
		if lj == 0 {
			return true
		}
		return li < lj
	})

	if len(viols) == 0 {
		return nil
	}
	return viols
}

// buildConceptSet returns the set of all concept IDs (passed, untested, and reserve).
func buildConceptSet(g *graph.Graph) map[string]bool {
	m := make(map[string]bool, len(g.PassedConcepts)+len(g.UntestedConcepts)+len(g.ReserveConcepts))
	for _, c := range g.PassedConcepts {
		m[c.ID] = true
	}
	for _, c := range g.UntestedConcepts {
		m[c.ID] = true
	}
	for _, c := range g.ReserveConcepts {
		m[c.ID] = true
	}
	return m
}

// buildQAMaps returns maps from ID to question and answer nodes.
func buildQAMaps(g *graph.Graph) (map[string]*graph.QuestionNode, map[string]*graph.AnswerNode) {
	qByID := make(map[string]*graph.QuestionNode)
	aByID := make(map[string]*graph.AnswerNode)
	for _, item := range g.TestingItems {
		if item.Q != nil {
			qByID[item.Q.ID] = item.Q
		}
		if item.A != nil {
			aByID[item.A.ID] = item.A
		}
	}
	return qByID, aByID
}

// buildInEdges returns a map from node ID to the slice of edges pointing to it.
func buildInEdges(g *graph.Graph) map[string][]*graph.Edge {
	m := make(map[string][]*graph.Edge)
	for _, e := range g.Edges {
		m[e.To] = append(m[e.To], e)
	}
	return m
}

// buildBlockMap returns a map from node ID to the block it belongs to.
func buildBlockMap(g *graph.Graph) map[string]graph.Block {
	m := make(map[string]graph.Block)
	for _, c := range g.PassedConcepts {
		m[c.ID] = graph.BlockPassed
	}
	for _, c := range g.UntestedConcepts {
		m[c.ID] = graph.BlockUntested
	}
	for _, c := range g.ReserveConcepts {
		m[c.ID] = graph.BlockReserve
	}
	for _, item := range g.TestingItems {
		if item.Q != nil {
			m[item.Q.ID] = graph.BlockTesting
		}
		if item.A != nil {
			m[item.A.ID] = graph.BlockTesting
		}
	}
	return m
}

// blockName returns the subgraph name string for a block constant.
func blockName(b graph.Block) string {
	switch b {
	case graph.BlockPassed:
		return "passed"
	case graph.BlockUntested:
		return "untested"
	case graph.BlockReserve:
		return "reserve"
	case graph.BlockTesting:
		return "testing"
	default:
		return "unknown"
	}
}

// check4 detects duplicate node declarations across all four blocks.
func check4(g *graph.Graph) []Violation {
	seen := make(map[string]bool)
	var viols []Violation

	declare := func(id string) {
		if seen[id] {
			viols = append(viols, Violation{Msg: fmt.Sprintf("duplicate declaration of node %q", id)})
		}
		seen[id] = true
	}

	for _, c := range g.PassedConcepts {
		declare(c.ID)
	}
	for _, c := range g.UntestedConcepts {
		declare(c.ID)
	}
	for _, c := range g.ReserveConcepts {
		declare(c.ID)
	}
	for _, item := range g.TestingItems {
		if item.Q != nil {
			declare(item.Q.ID)
		}
		if item.A != nil {
			declare(item.A.ID)
		}
	}
	return viols
}

// check5 verifies declaration order within blocks and that every edge is placed
// in the block §4.2 requires. It performs a raw line scan because the parsed
// model discards source positions.
func check5(data []byte, blocks map[string]graph.Block) []Violation {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var viols []Violation
	pos := 0

	// Skip optional frontmatter (--- ... ---).
	if pos < len(lines) && strings.TrimSpace(lines[pos]) == "---" {
		pos++
		for pos < len(lines) {
			if strings.TrimSpace(lines[pos]) == "---" {
				pos++
				break
			}
			pos++
		}
	}

	currentBlock := -1 // -1 = not inside any subgraph
	seenEdge := false  // true after the first edge line in the current block

	for ; pos < len(lines); pos++ {
		lineNum := pos + 1
		t := strings.TrimSpace(lines[pos])

		if t == "" || t == "flowchart TB" || strings.HasPrefix(t, "classDef ") {
			continue
		}

		if strings.HasPrefix(t, "subgraph ") {
			currentBlock = blockFromID(parseSubgraphID(t))
			seenEdge = false
			continue
		}

		if t == "end" {
			currentBlock = -1
			seenEdge = false
			continue
		}

		if currentBlock < 0 {
			continue
		}

		// Skip %% comment and meta lines; they precede declarations by spec
		// and never constitute a declaration or edge for ordering purposes.
		if strings.HasPrefix(t, "%%") {
			continue
		}

		blk := graph.Block(currentBlock)

		if strings.Contains(t, "-->") {
			seenEdge = true
			from, to, ok := parseEdgeLine(t)
			if !ok {
				continue
			}
			// Report each unknown endpoint once.
			_, fok := blocks[from]
			_, tok := blocks[to]
			if !fok {
				viols = append(viols, Violation{Line: lineNum, Msg: fmt.Sprintf("edge references unknown node %q", from)})
			}
			if !tok {
				viols = append(viols, Violation{Line: lineNum, Msg: fmt.Sprintf("edge references unknown node %q", to)})
			}
			// Only check placement when both endpoints are known.
			if fok && tok {
				home := graph.EdgeHomeBlock(from, to, blocks)
				if blk != home {
					viols = append(viols, Violation{Line: lineNum, Msg: fmt.Sprintf("edge %s --> %s belongs in block %s", from, to, blockName(home))})
				}
			}
		} else if strings.Contains(t, `["`) {
			// Node declaration line.
			if seenEdge {
				viols = append(viols, Violation{Line: lineNum, Msg: fmt.Sprintf("declaration follows an edge in block %s", blockName(blk))})
			}
		}
	}
	return viols
}

// parseSubgraphID extracts the ID token from a "subgraph ID[...]" header line.
func parseSubgraphID(t string) string {
	rest := strings.TrimPrefix(t, "subgraph ")
	idx := strings.IndexByte(rest, '[')
	if idx < 0 {
		return rest
	}
	return rest[:idx]
}

// blockFromID maps a subgraph ID string to the corresponding block index,
// returning -1 for unrecognised IDs.
func blockFromID(id string) int {
	switch id {
	case "passed":
		return int(graph.BlockPassed)
	case "untested":
		return int(graph.BlockUntested)
	case "reserve":
		return int(graph.BlockReserve)
	case "testing":
		return int(graph.BlockTesting)
	default:
		return -1
	}
}

// parseEdgeLine parses the from and to node IDs from an edge line.
// It handles both structural (-->) and labeled (--"..."--> ) edges.
func parseEdgeLine(t string) (from, to string, ok bool) {
	if i := strings.Index(t, ` --"`); i >= 0 {
		from = strings.TrimSpace(t[:i])
		rest := t[i+4:]
		end := strings.Index(rest, `"-->`)
		if end < 0 {
			return "", "", false
		}
		to = strings.TrimSpace(rest[end+4:])
	} else if i := strings.Index(t, " --> "); i >= 0 {
		from = strings.TrimSpace(t[:i])
		to = strings.TrimSpace(t[i+5:])
	} else {
		return "", "", false
	}
	return from, to, true
}

// check6 verifies that all concept IDs are valid per §11.6.
func check6(g *graph.Graph) []Violation {
	var viols []Violation
	check := func(cn *graph.ConceptNode) {
		if !graph.ValidConceptID(cn.ID) {
			viols = append(viols, Violation{Msg: fmt.Sprintf("invalid concept ID %q", cn.ID)})
		}
	}
	for _, c := range g.PassedConcepts {
		check(c)
	}
	for _, c := range g.UntestedConcepts {
		check(c)
	}
	for _, c := range g.ReserveConcepts {
		check(c)
	}
	return viols
}

// check7 verifies that questions carry a valid batch class, answers carry a
// valid answer class, and concepts carry no class (§11.7).
func check7(g *graph.Graph) []Violation {
	var viols []Violation

	// Concepts must carry no class.
	for _, c := range g.PassedConcepts {
		if c.Class != "" {
			viols = append(viols, Violation{Msg: fmt.Sprintf("concept %q must not carry a class (got %q)", c.ID, c.Class)})
		}
	}
	for _, c := range g.UntestedConcepts {
		if c.Class != "" {
			viols = append(viols, Violation{Msg: fmt.Sprintf("concept %q must not carry a class (got %q)", c.ID, c.Class)})
		}
	}
	for _, c := range g.ReserveConcepts {
		if c.Class != "" {
			viols = append(viols, Violation{Msg: fmt.Sprintf("concept %q must not carry a class (got %q)", c.ID, c.Class)})
		}
	}

	for _, item := range g.TestingItems {
		if item.Q != nil {
			q := item.Q
			if !graph.ValidBatchID(q.Class) {
				viols = append(viols, Violation{Msg: fmt.Sprintf("question %q has invalid batch class %q", q.ID, q.Class)})
			}
		}
		if item.A != nil {
			a := item.A
			if !graph.IsAnswerClass(a.Class) {
				viols = append(viols, Violation{Msg: fmt.Sprintf("answer %q has invalid answer class %q", a.ID, a.Class)})
			}
		}
	}
	return viols
}

// resolveConcept walks the incoming-edge chain upward from a question ID until
// it reaches a concept node. It returns the concept ID or an error when the
// chain contains a cycle, a missing node, or a non-answer non-concept source.
func resolveConcept(
	id string,
	inEdges map[string][]*graph.Edge,
	qByID map[string]*graph.QuestionNode,
	aByID map[string]*graph.AnswerNode,
	concepts map[string]bool,
) (string, error) {
	visited := make(map[string]bool)
	cur := id
	for {
		if visited[cur] {
			return "", fmt.Errorf("cycle detected at %q", cur)
		}
		visited[cur] = true

		edges := inEdges[cur]
		if len(edges) != 1 {
			return "", fmt.Errorf("node %q has %d incoming edges", cur, len(edges))
		}

		src := edges[0].From
		if concepts[src] {
			return src, nil
		}
		a, ok := aByID[src]
		if !ok {
			return "", fmt.Errorf("source %q of node %q is not a concept or answer", src, cur)
		}
		n := graph.QuestionN(a.ID)
		if n == 0 {
			return "", fmt.Errorf("invalid answer ID %q", a.ID)
		}
		nextQ := fmt.Sprintf("q%d", n)
		if _, exists := qByID[nextQ]; !exists {
			return "", fmt.Errorf("question %q not found", nextQ)
		}
		cur = nextQ
	}
}

// walkToRootProbe follows the incoming-edge chain upward from a question whose
// incoming source is an answer, looking for the root probe question — a probe
// whose single incoming edge comes from a concept. It returns the root probe and
// the answer that feeds it, or (nil, nil) when the root cannot be found due to a
// cycle, missing node, or wrong structure.
func walkToRootProbe(
	id string,
	inEdges map[string][]*graph.Edge,
	qByID map[string]*graph.QuestionNode,
	aByID map[string]*graph.AnswerNode,
	concepts map[string]bool,
) (*graph.QuestionNode, *graph.AnswerNode) {
	visited := make(map[string]bool)
	cur := id
	for {
		if visited[cur] {
			return nil, nil
		}
		visited[cur] = true

		edges := inEdges[cur]
		if len(edges) != 1 {
			return nil, nil
		}

		srcID := edges[0].From
		aX, isAnswer := aByID[srcID]
		if !isAnswer {
			return nil, nil
		}

		n := graph.QuestionN(aX.ID)
		if n == 0 {
			return nil, nil
		}
		qXID := fmt.Sprintf("q%d", n)
		qX, ok := qByID[qXID]
		if !ok {
			return nil, nil
		}

		// A root probe is a probe whose single incoming edge comes from a concept.
		if graph.IsProbeClass(qX.Class) {
			qXEdges := inEdges[qX.ID]
			if len(qXEdges) == 1 && concepts[qXEdges[0].From] {
				return qX, aX
			}
		}

		cur = qXID
	}
}

// check8 verifies question/answer edge topology per §11.8.
func check8(
	g *graph.Graph,
	inEdges map[string][]*graph.Edge,
	qByID map[string]*graph.QuestionNode,
	aByID map[string]*graph.AnswerNode,
	concepts map[string]bool,
) []Violation {
	var viols []Violation

	// Each answer aN must have exactly one incoming edge from qN.
	for _, item := range g.TestingItems {
		if item.A == nil {
			continue
		}
		a := item.A
		n := graph.QuestionN(a.ID)
		expectedQ := fmt.Sprintf("q%d", n)

		edges := inEdges[a.ID]
		if len(edges) != 1 {
			viols = append(viols, Violation{Msg: fmt.Sprintf("answer %q must have exactly one incoming edge", a.ID)})
			continue
		}
		if edges[0].From != expectedQ {
			viols = append(viols, Violation{Msg: fmt.Sprintf("answer %q must be fed by q%d", a.ID, n)})
		}
	}

	// Each question must have exactly one incoming edge of the right kind,
	// must resolve to a concept, and (for teach/replacement) must chain back to
	// a failed or unclear probe.
	for _, item := range g.TestingItems {
		if item.Q == nil {
			continue
		}
		q := item.Q

		edges := inEdges[q.ID]
		if len(edges) != 1 {
			viols = append(viols, Violation{Msg: fmt.Sprintf("question %q must have exactly one incoming edge", q.ID)})
			continue
		}

		src := edges[0].From
		srcAnswer, srcIsAnswer := aByID[src]
		srcIsConcept := concepts[src]
		needsRootCheck := false

		if graph.IsProbeClass(q.Class) {
			switch {
			case srcIsConcept:
				// Regular probe from concept: fine.
			case srcIsAnswer:
				if srcAnswer.Class != "unclear" {
					viols = append(viols, Violation{Msg: fmt.Sprintf("probe %q must originate at its concept or an unclear answer", q.ID)})
				}
				needsRootCheck = true
			default:
				viols = append(viols, Violation{Msg: fmt.Sprintf("probe %q must originate at its concept or an unclear answer", q.ID)})
			}
		} else if graph.IsTeachClass(q.Class) {
			if !srcIsAnswer {
				viols = append(viols, Violation{Msg: fmt.Sprintf("teach question %q must follow an answer", q.ID)})
			} else {
				needsRootCheck = true
			}
		}

		// Every question must resolve to exactly one concept.
		if _, err := resolveConcept(q.ID, inEdges, qByID, aByID, concepts); err != nil {
			viols = append(viols, Violation{Msg: fmt.Sprintf("question %q does not resolve to a concept", q.ID)})
			// Skip root-probe check since we already failed concept resolution.
			continue
		}

		if needsRootCheck {
			rootQ, rootA := walkToRootProbe(q.ID, inEdges, qByID, aByID, concepts)
			// A nil rootQ means the chain structure does not terminate at a
			// recognisable root probe, even though the concept itself is reachable.
			// The accurate diagnostic is the same as a wrong-class root answer.
			if rootQ == nil || (rootA.Class != "fail" && rootA.Class != "unclear") {
				viols = append(viols, Violation{Msg: fmt.Sprintf("question %q chain must end at a failed or unclear probe", q.ID)})
			}
		}
	}
	return viols
}

// isReplacementBatch reports whether the given probe batch is a replacement
// batch per §8.5: every question's single incoming edge comes from an unclear answer.
func isReplacementBatch(
	batchClass string,
	qs []*graph.QuestionNode,
	inEdges map[string][]*graph.Edge,
	aByID map[string]*graph.AnswerNode,
) bool {
	if !graph.IsProbeClass(batchClass) {
		return false
	}
	for _, q := range qs {
		edges := inEdges[q.ID]
		if len(edges) != 1 {
			return false
		}
		a, ok := aByID[edges[0].From]
		if !ok || a.Class != "unclear" {
			return false
		}
	}
	return true
}

// check9 verifies batch constraints: concept consistency, max size, and min
// size when answers exist (replacement batches are exempt from both limits).
func check9(
	g *graph.Graph,
	cfg Config,
	inEdges map[string][]*graph.Edge,
	qByID map[string]*graph.QuestionNode,
	aByID map[string]*graph.AnswerNode,
	concepts map[string]bool,
) []Violation {
	// Group questions by batch class in declaration order.
	batchOrder := make([]string, 0)
	batches := make(map[string][]*graph.QuestionNode)
	for _, item := range g.TestingItems {
		if item.Q == nil {
			continue
		}
		q := item.Q
		if !graph.ValidBatchID(q.Class) {
			continue
		}
		if _, seen := batches[q.Class]; !seen {
			batchOrder = append(batchOrder, q.Class)
		}
		batches[q.Class] = append(batches[q.Class], q)
	}

	var viols []Violation

	for _, batchClass := range batchOrder {
		qs := batches[batchClass]

		// All questions must resolve to the same concept.
		conceptID := ""
		multiConcept := false
		unresolved := false
		for _, q := range qs {
			c, err := resolveConcept(q.ID, inEdges, qByID, aByID, concepts)
			if err != nil || c == "" {
				unresolved = true
				break
			}
			if conceptID == "" {
				conceptID = c
			} else if c != conceptID {
				multiConcept = true
				break
			}
		}
		if multiConcept {
			viols = append(viols, Violation{Msg: fmt.Sprintf("batch %s spans multiple concepts", batchClass)})
			continue
		}
		if unresolved {
			continue
		}

		repl := isReplacementBatch(batchClass, qs, inEdges, aByID)
		if repl {
			// Replacement batches are exempt from both min and max checks.
			continue
		}

		size := len(qs)

		// Determine whether any question in the batch has an answer.
		hasAnswer := false
		for _, q := range qs {
			aID := fmt.Sprintf("a%d", graph.QuestionN(q.ID))
			if _, ok := aByID[aID]; ok {
				hasAnswer = true
				break
			}
		}

		var maxSize int
		if graph.IsProbeClass(batchClass) {
			maxSize = cfg.ProbeMax
		} else {
			maxSize = cfg.TeachMax
		}
		if size > maxSize {
			viols = append(viols, Violation{Msg: fmt.Sprintf("batch %s has %d questions, exceeds max %d", batchClass, size, maxSize)})
		}

		if hasAnswer {
			var minSize int
			if graph.IsProbeClass(batchClass) {
				minSize = cfg.ProbeMin
			} else {
				minSize = cfg.TeachMin
			}
			if size < minSize {
				viols = append(viols, Violation{Msg: fmt.Sprintf("batch %s has %d questions, below min %d", batchClass, size, minSize)})
			}
		}
	}
	return viols
}

// check10 verifies that every concept-to-concept edge carries a relation label
// and that the directed graph of concepts forms a DAG.
func check10(g *graph.Graph, concepts map[string]bool) []Violation {
	var viols []Violation

	// Build adjacency list for concept-to-concept edges only.
	adj := make(map[string][]string)
	for _, e := range g.Edges {
		if !concepts[e.From] || !concepts[e.To] {
			continue
		}
		if e.Label == "" {
			viols = append(viols, Violation{Msg: fmt.Sprintf("concept edge %s --> %s has no relation label", e.From, e.To)})
		}
		adj[e.From] = append(adj[e.From], e.To)
	}

	// DFS cycle detection over concept nodes.
	// color: 0 = unvisited, 1 = on the current path (gray), 2 = fully visited (black).
	color := make(map[string]int)
	hasCycle := false
	var dfs func(id string)
	dfs = func(id string) {
		if hasCycle || color[id] == 2 {
			return
		}
		if color[id] == 1 {
			hasCycle = true
			return
		}
		color[id] = 1
		for _, nb := range adj[id] {
			dfs(nb)
		}
		color[id] = 2
	}

	// Sort concept IDs for deterministic traversal order.
	sortedConcepts := make([]string, 0, len(concepts))
	for id := range concepts {
		sortedConcepts = append(sortedConcepts, id)
	}
	sort.Strings(sortedConcepts)
	for _, id := range sortedConcepts {
		if color[id] == 0 {
			dfs(id)
		}
	}
	if hasCycle {
		viols = append(viols, Violation{Msg: "concept edges must form a DAG"})
	}
	return viols
}

// check11 is a static citation check (§11): it verifies citation syntax,
// requires a hash on every citation, and rejects raw '"' in locators.
// No file I/O is performed; file existence and bounds are checked at
// write time (add/q/edit) and via tm lint --drift.
func check11(g *graph.Graph, _ Config) []Violation {
	var viols []Violation

	checkCite := func(prefix, citeStr string) {
		c, err := cite.Parse(citeStr)
		if err != nil {
			viols = append(viols, Violation{Msg: fmt.Sprintf("%s: %v", prefix, err)})
			return
		}
		// §11: every citation must carry a content hash.
		if c.Hash == "" {
			viols = append(viols, Violation{Msg: fmt.Sprintf("%s: citation %q is missing a hash; use tm rehash or supply <hash>@<locator>:START-END", prefix, citeStr)})
		}
		// §11: raw '"' in a locator is rejected by Parse, so it cannot reach here.
		// §11.14: for git: locators, check for structural errors including raw ":".
		if cite.IsGit(c.File) {
			if _, gitErr := cite.ParseGit(c.File); gitErr != nil {
				viols = append(viols, Violation{Msg: fmt.Sprintf("%s: citation %q: %s", prefix, citeStr, gitErr.Error())})
			}
		}
	}

	for _, c := range g.PassedConcepts {
		pfx := fmt.Sprintf("concept %q", c.ID)
		for _, citeStr := range c.Cites {
			checkCite(pfx, citeStr)
		}
	}
	for _, c := range g.UntestedConcepts {
		pfx := fmt.Sprintf("concept %q", c.ID)
		for _, citeStr := range c.Cites {
			checkCite(pfx, citeStr)
		}
	}
	for _, c := range g.ReserveConcepts {
		pfx := fmt.Sprintf("concept %q", c.ID)
		for _, citeStr := range c.Cites {
			checkCite(pfx, citeStr)
		}
	}
	for _, item := range g.TestingItems {
		if item.Q != nil {
			q := item.Q
			if q.Cite != "" {
				checkCite(fmt.Sprintf("question %q", q.ID), q.Cite)
			}
		}
	}
	return viols
}

// check15 verifies that when a %% tm:next line is present, its q counter is
// strictly greater than every qN/aN suffix in the file and its batch counter is
// strictly greater than every probe_N/teach_N suffix in the file (§11 check 15).
// Fix suggestion: raise the counters in %% tm:next.
func check15(g *graph.Graph) []Violation {
	if g.NextMeta == nil {
		return nil
	}
	const fix = "; fix: raise the counters in %% tm:next"
	var viols []Violation
	for _, item := range g.TestingItems {
		if item.Q != nil {
			if n := graph.QuestionN(item.Q.ID); n >= g.NextMeta.Q {
				viols = append(viols, Violation{Msg: fmt.Sprintf(
					"tm:next q=%d is not greater than question %s (N=%d)%s",
					g.NextMeta.Q, item.Q.ID, n, fix,
				)})
			}
			if n := graph.BatchN(item.Q.Class); n >= g.NextMeta.Batch {
				viols = append(viols, Violation{Msg: fmt.Sprintf(
					"tm:next batch=%d is not greater than batch class %s (N=%d)%s",
					g.NextMeta.Batch, item.Q.Class, n, fix,
				)})
			}
		}
		if item.A != nil {
			if n := graph.QuestionN(item.A.ID); n >= g.NextMeta.Q {
				viols = append(viols, Violation{Msg: fmt.Sprintf(
					"tm:next q=%d is not greater than answer %s (N=%d)%s",
					g.NextMeta.Q, item.A.ID, n, fix,
				)})
			}
		}
	}
	return viols
}

// check12 verifies that every passed concept has no GAP, no gate meta line
// targeting it, and no questions that resolve to it.
// Also verifies that every reserve concept has no questions and no gate line.
func check12(
	g *graph.Graph,
	concepts map[string]bool,
	inEdges map[string][]*graph.Edge,
	qByID map[string]*graph.QuestionNode,
	aByID map[string]*graph.AnswerNode,
) []Violation {
	if len(g.PassedConcepts) == 0 && len(g.ReserveConcepts) == 0 {
		return nil
	}

	// Build the set of concept IDs targeted by gate meta lines.
	gateMetas := make(map[string]bool, len(g.UntestedMetas))
	for _, m := range g.UntestedMetas {
		gateMetas[m.Concept] = true
	}

	// Map each concept to whether any question resolves to it.
	conceptHasQ := make(map[string]bool)
	for _, item := range g.TestingItems {
		if item.Q == nil {
			continue
		}
		c, err := resolveConcept(item.Q.ID, inEdges, qByID, aByID, concepts)
		if err == nil && c != "" {
			conceptHasQ[c] = true
		}
	}

	var viols []Violation
	for _, c := range g.PassedConcepts {
		if c.GAP != "" {
			viols = append(viols, Violation{Msg: fmt.Sprintf("passed concept %q has a GAP", c.ID)})
		}
		if gateMetas[c.ID] {
			viols = append(viols, Violation{Msg: fmt.Sprintf("passed concept %q has a gate line", c.ID)})
		}
		if conceptHasQ[c.ID] {
			viols = append(viols, Violation{Msg: fmt.Sprintf("passed concept %q has open questions", c.ID)})
		}
	}
	// Reserve concepts must have no questions and no gate line. GAP is allowed.
	for _, c := range g.ReserveConcepts {
		if gateMetas[c.ID] {
			viols = append(viols, Violation{Msg: fmt.Sprintf("reserve concept %q has a gate line", c.ID)})
		}
		if conceptHasQ[c.ID] {
			viols = append(viols, Violation{Msg: fmt.Sprintf("reserve concept %q has questions", c.ID)})
		}
	}
	return viols
}

// check17 verifies that when %% tm:format is present, its value does not exceed
// the binary's CurrentFormat (§11 check 17). A format above the binary refuses
// with a fix to upgrade tm; a lower format is accepted so migrate can lint its result.
func check17(g *graph.Graph) []Violation {
	if g.Format == nil {
		return nil
	}
	if g.Format.N > graph.CurrentFormat {
		return []Violation{{
			Msg: fmt.Sprintf(
				"%% tm:format %d is above this binary (format %d); fix: upgrade tm",
				g.Format.N, graph.CurrentFormat,
			),
		}}
	}
	return nil
}

// check16 verifies that no concept or question cites a file that resolves
// inside the aids directory (§11 check 16, §4.6 rule 5). No file reads are
// performed; the check is purely path-based.
func check16(g *graph.Graph, cfg Config) []Violation {
	if cfg.AidsDir == "" {
		return nil
	}
	aidsDir := filepath.Clean(cfg.AidsDir)

	checkCite := func(nodeID, citeStr string, violations *[]Violation) {
		c, err := cite.Parse(citeStr)
		if err != nil {
			return
		}
		if cite.IsURI(c.File) || cite.IsGit(c.File) {
			return
		}
		path := filepath.Clean(cite.Resolve(c, cfg.SrcRoot))
		inAids := path == aidsDir || strings.HasPrefix(path, aidsDir+string(filepath.Separator))
		if inAids {
			*violations = append(*violations, Violation{Msg: fmt.Sprintf(
				"%s cites aid %s; fix: cite the primary source; link the aid with tm aid %s %s",
				nodeID, c.File, nodeID, c.File,
			)})
		}
	}

	var viols []Violation
	for _, cn := range g.PassedConcepts {
		for _, cite := range cn.Cites {
			checkCite(cn.ID, cite, &viols)
		}
	}
	for _, cn := range g.UntestedConcepts {
		for _, c := range cn.Cites {
			checkCite(cn.ID, c, &viols)
		}
	}
	for _, cn := range g.ReserveConcepts {
		for _, c := range cn.Cites {
			checkCite(cn.ID, c, &viols)
		}
	}
	for _, item := range g.TestingItems {
		if item.Q != nil {
			checkCite(item.Q.ID, item.Q.Cite, &viols)
		}
	}
	return viols
}
