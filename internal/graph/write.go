package graph

import (
	"sort"
	"strings"
)

const (
	indent1 = "    "     // 4 spaces: subgraph/end/classDef level
	indent2 = "        " // 8 spaces: node/edge level inside a subgraph
)

// Write serialises g to canonical Mermaid text per spec section 4.
// The output uses LF line endings and ends with a trailing newline.
func Write(g *Graph) []byte {
	var b strings.Builder

	// Verbatim front-matter.
	if g.Frontmatter != "" {
		b.WriteString(g.Frontmatter)
	}

	b.WriteString("flowchart TB\n")

	// Build the node-to-block map once; the edge placement rule (4.2) uses it.
	blocks := g.nodeBlocks()

	writePassedBlock(&b, g, blocks)
	writeUntestedBlock(&b, g, blocks)
	writeReserveBlock(&b, g, blocks)
	writeTestingBlock(&b, g, blocks)
	writeClassDefs(&b, g)

	return []byte(b.String())
}

// writePassedBlock emits the passed subgraph.
func writePassedBlock(b *strings.Builder, g *Graph, blocks map[string]Block) {
	writeSubgraphOpen(b, "passed", g.PassedTitle)
	for _, cn := range g.PassedConcepts {
		writeLeadingComments(b, cn.LeadingComments)
		writeConceptNode(b, cn)
	}
	for _, e := range g.Edges {
		if edgeHomeBlock(e, blocks) == BlockPassed {
			writeLeadingComments(b, e.LeadingComments)
			writeEdge(b, e)
		}
	}
	writeSubgraphClose(b)
}

// writeUntestedBlock emits the untested subgraph.
func writeUntestedBlock(b *strings.Builder, g *Graph, blocks map[string]Block) {
	writeSubgraphOpen(b, "untested", g.UntestedTitle)
	// NextMeta line comes first (§4.6: before any %% tm:gate lines).
	if g.NextMeta != nil {
		writeLeadingComments(b, g.NextMeta.LeadingComments)
		writeNextMeta(b, g.NextMeta)
	}
	for _, m := range g.UntestedMetas {
		writeLeadingComments(b, m.LeadingComments)
		writeGateMeta(b, m)
	}
	for _, cn := range g.UntestedConcepts {
		writeLeadingComments(b, cn.LeadingComments)
		writeConceptNode(b, cn)
	}
	for _, e := range g.Edges {
		if edgeHomeBlock(e, blocks) == BlockUntested {
			writeLeadingComments(b, e.LeadingComments)
			writeEdge(b, e)
		}
	}
	writeSubgraphClose(b)
}

// writeReserveBlock emits the reserve subgraph. The title defaults to
// "Concepts held in reserve" when ReserveTitle is empty (e.g. when the block
// was absent in a pre-v0.3 file and the caller did not set it).
func writeReserveBlock(b *strings.Builder, g *Graph, blocks map[string]Block) {
	title := g.ReserveTitle
	if title == "" {
		title = "Concepts held in reserve"
	}
	writeSubgraphOpen(b, "reserve", title)
	for _, cn := range g.ReserveConcepts {
		writeLeadingComments(b, cn.LeadingComments)
		writeConceptNode(b, cn)
	}
	for _, e := range g.Edges {
		if edgeHomeBlock(e, blocks) == BlockReserve {
			writeLeadingComments(b, e.LeadingComments)
			writeEdge(b, e)
		}
	}
	writeSubgraphClose(b)
}

// writeTestingBlock emits the testing subgraph.
func writeTestingBlock(b *strings.Builder, g *Graph, blocks map[string]Block) {
	writeSubgraphOpen(b, "testing", g.TestingTitle)
	for _, item := range g.TestingItems {
		if item.Q != nil {
			writeLeadingComments(b, item.Q.LeadingComments)
			writeQuestionNode(b, item.Q)
		} else if item.A != nil {
			writeLeadingComments(b, item.A.LeadingComments)
			writeAnswerNode(b, item.A)
		}
	}
	for _, e := range g.Edges {
		if edgeHomeBlock(e, blocks) == BlockTesting {
			writeLeadingComments(b, e.LeadingComments)
			writeEdge(b, e)
		}
	}
	writeSubgraphClose(b)
}

// writeSubgraphOpen emits the subgraph opening line.
func writeSubgraphOpen(b *strings.Builder, id, title string) {
	b.WriteString(indent1)
	b.WriteString("subgraph ")
	b.WriteString(id)
	b.WriteString(`["`)
	b.WriteString(title)
	b.WriteString("\"]\n")
}

// writeSubgraphClose emits the subgraph closing line.
func writeSubgraphClose(b *strings.Builder) {
	b.WriteString(indent1)
	b.WriteString("end\n")
}

// writeNextMeta emits the %% tm:next meta line.
func writeNextMeta(b *strings.Builder, m *NextMeta) {
	b.WriteString(indent2)
	b.WriteString("%% tm:next q=")
	writeInt(b, m.Q)
	b.WriteString(" batch=")
	writeInt(b, m.Batch)
	b.WriteByte('\n')
}

// writeGateMeta emits a %% tm:gate meta line.
func writeGateMeta(b *strings.Builder, m GateMeta) {
	b.WriteString(indent2)
	b.WriteString("%% tm:gate ")
	b.WriteString(m.Concept)
	b.WriteString(" base=")
	writeInt(b, m.Base)
	b.WriteByte('\n')
}

// writeConceptNode emits a concept node declaration.
func writeConceptNode(b *strings.Builder, cn *ConceptNode) {
	b.WriteString(indent2)
	b.WriteString(cn.ID)
	b.WriteString(`["`)
	b.WriteString(conceptLabel(cn))
	b.WriteString("\"]\n")
}

// conceptLabel builds the escaped label string for a concept node.
func conceptLabel(cn *ConceptNode) string {
	parts := []string{Escape(cn.Scope)}
	if cn.GAP != "" {
		parts = append(parts, "GAP: "+Escape(cn.GAP))
	}
	if len(cn.Cites) > 0 {
		parts = append(parts, strings.Join(cn.Cites, ", "))
	}
	return strings.Join(parts, "<br/>")
}

// writeQuestionNode emits a question node declaration.
func writeQuestionNode(b *strings.Builder, q *QuestionNode) {
	b.WriteString(indent2)
	b.WriteString(q.ID)
	b.WriteString(`["`)
	b.WriteString(Escape(q.Scope))
	b.WriteString("<br/>")
	b.WriteString(q.Cite)
	b.WriteString(`"]:::`)
	b.WriteString(q.Class)
	b.WriteByte('\n')
}

// writeAnswerNode emits an answer node declaration.
// Label format: [OOS<br/>][ASKED: <esc wording><br/>]<esc body>
// The optional OOS field comes first, then the optional ASKED: field, then the body.
func writeAnswerNode(b *strings.Builder, a *AnswerNode) {
	b.WriteString(indent2)
	b.WriteString(a.ID)
	b.WriteString(`["`)
	if a.OOS {
		b.WriteString("OOS<br/>")
	}
	if a.Asked != "" {
		b.WriteString("ASKED: ")
		b.WriteString(Escape(a.Asked))
		b.WriteString("<br/>")
	}
	b.WriteString(Escape(a.Label))
	b.WriteString(`"]:::`)
	b.WriteString(a.Class)
	b.WriteByte('\n')
}

// writeEdge emits one edge line at the node/edge indent level.
func writeEdge(b *strings.Builder, e *Edge) {
	b.WriteString(indent2)
	b.WriteString(e.From)
	if e.Label == "" {
		b.WriteString(" --> ")
		b.WriteString(e.To)
	} else {
		b.WriteString(` --"`)
		b.WriteString(Escape(e.Label))
		b.WriteString(`"--> `)
		b.WriteString(e.To)
	}
	b.WriteByte('\n')
}

// writeLeadingComments emits any %% comment lines that precede a declaration,
// each indented to the node/edge level (indent2).
func writeLeadingComments(b *strings.Builder, comments []string) {
	for _, c := range comments {
		b.WriteString(indent2)
		b.WriteString(c)
		b.WriteByte('\n')
	}
}

// writeClassDefs emits the CLI-owned classDef lines.
// Probe batch classes share one line (sorted by N), teach batch classes share
// one line (sorted by N), followed by the four fixed answer class lines.
func writeClassDefs(b *strings.Builder, g *Graph) {
	var probes []string
	var teaches []string
	seen := map[string]bool{}

	for _, item := range g.TestingItems {
		if item.Q == nil {
			continue
		}
		class := item.Q.Class
		if seen[class] {
			continue
		}
		seen[class] = true
		if IsProbeClass(class) {
			probes = append(probes, class)
		} else if IsTeachClass(class) {
			teaches = append(teaches, class)
		}
	}

	sort.Slice(probes, func(i, j int) bool { return BatchN(probes[i]) < BatchN(probes[j]) })
	sort.Slice(teaches, func(i, j int) bool { return BatchN(teaches[i]) < BatchN(teaches[j]) })

	if len(probes) > 0 {
		b.WriteString(indent1)
		b.WriteString("classDef ")
		b.WriteString(strings.Join(probes, ","))
		b.WriteString(" stroke:#4aa3ff\n")
	}
	if len(teaches) > 0 {
		b.WriteString(indent1)
		b.WriteString("classDef ")
		b.WriteString(strings.Join(teaches, ","))
		b.WriteString(" stroke:#c9a227\n")
	}
	b.WriteString(indent1 + "classDef pass stroke:#3fb950\n")
	b.WriteString(indent1 + "classDef fail stroke:#f85149\n")
	b.WriteString(indent1 + "classDef unclear stroke:#d29922\n")
	b.WriteString(indent1 + "classDef pending stroke-dasharray:4 3\n")
}

// writeInt writes a non-negative integer to b.
func writeInt(b *strings.Builder, n int) {
	if n == 0 {
		b.WriteByte('0')
		return
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	b.Write(digits[i:])
}
