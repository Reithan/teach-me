package graph

import (
	"fmt"
	"strings"
)

// Parse parses the contents of a .mmd file and returns the graph.
// It accepts both LF and CRLF line endings (spec 16.4).
// Label fields in node declarations are unescaped before being stored in the model.
func Parse(data []byte) (*Graph, error) {
	// Normalize line endings to LF.
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	lines := strings.Split(s, "\n")
	// Drop the final empty element that appears when the file ends with \n.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	p := &parser{lines: lines}
	return p.parse()
}

// parser holds the parsing state.
type parser struct {
	lines           []string
	pos             int
	pendingComments []string
}

func (p *parser) peekTrimmed() (string, bool) {
	if p.pos >= len(p.lines) {
		return "", false
	}
	return strings.TrimSpace(p.lines[p.pos]), true
}

func (p *parser) nextTrimmed() (string, bool) {
	if p.pos >= len(p.lines) {
		return "", false
	}
	l := strings.TrimSpace(p.lines[p.pos])
	p.pos++
	return l, true
}

func (p *parser) parse() (*Graph, error) {
	g := &Graph{}

	// Parse optional frontmatter.
	if line, ok := p.peekTrimmed(); ok && line == "---" {
		fm, err := p.parseFrontmatter()
		if err != nil {
			return nil, err
		}
		g.Frontmatter = fm
	}

	// Expect "flowchart TB".
	line, ok := p.nextTrimmed()
	if !ok {
		return nil, fmt.Errorf("graph: expected \"flowchart TB\", got end of file")
	}
	if line != "flowchart TB" {
		return nil, fmt.Errorf("graph: expected \"flowchart TB\", got %q", line)
	}

	// Parse three blocks in fixed order.
	if err := p.parseBlock(g, BlockPassed); err != nil {
		return nil, err
	}
	if err := p.parseBlock(g, BlockUntested); err != nil {
		return nil, err
	}
	if err := p.parseBlock(g, BlockTesting); err != nil {
		return nil, err
	}

	// Consume trailing classDef lines and blank lines; they are regenerated
	// by the writer.
	for p.pos < len(p.lines) {
		t := strings.TrimSpace(p.lines[p.pos])
		if t == "" || strings.HasPrefix(t, "classDef ") || strings.HasPrefix(t, "%%") {
			p.pos++
			continue
		}
		return nil, fmt.Errorf("graph: unexpected line after blocks: %q", p.lines[p.pos])
	}

	return g, nil
}

// parseFrontmatter reads from the opening "---" through the closing "---" and
// returns the verbatim block (LF-separated), including both fence lines,
// followed by a single trailing newline.
func (p *parser) parseFrontmatter() (string, error) {
	start := p.pos
	p.pos++ // consume opening "---"
	for p.pos < len(p.lines) {
		if strings.TrimSpace(p.lines[p.pos]) == "---" {
			p.pos++ // consume closing "---"
			return strings.Join(p.lines[start:p.pos], "\n") + "\n", nil
		}
		p.pos++
	}
	return "", fmt.Errorf("graph: unclosed frontmatter block")
}

// blockIDStr returns the expected subgraph identifier string for b.
func blockIDStr(b Block) string {
	switch b {
	case BlockPassed:
		return "passed"
	case BlockUntested:
		return "untested"
	case BlockTesting:
		return "testing"
	default:
		return ""
	}
}

// parseBlock parses one subgraph block (opening line, contents, "end").
func (p *parser) parseBlock(g *Graph, block Block) error {
	// Consume leading blank lines.
	for p.pos < len(p.lines) && strings.TrimSpace(p.lines[p.pos]) == "" {
		p.pos++
	}

	line, ok := p.nextTrimmed()
	if !ok {
		return fmt.Errorf("graph: expected subgraph %s, got end of file", blockIDStr(block))
	}
	if !strings.HasPrefix(line, "subgraph ") {
		return fmt.Errorf("graph: expected subgraph %s, got %q", blockIDStr(block), line)
	}

	id, title, err := parseSubgraphHeader(line)
	if err != nil {
		return err
	}
	if id != blockIDStr(block) {
		return fmt.Errorf("graph: expected block %q, got %q", blockIDStr(block), id)
	}

	switch block {
	case BlockPassed:
		g.PassedTitle = title
	case BlockUntested:
		g.UntestedTitle = title
	case BlockTesting:
		g.TestingTitle = title
	}

	// Parse lines until "end".
	for {
		if p.pos >= len(p.lines) {
			return fmt.Errorf("graph: unclosed subgraph %s", id)
		}
		t := strings.TrimSpace(p.lines[p.pos])

		if t == "end" {
			p.pos++
			break
		}

		if t == "" {
			p.pos++
			continue
		}

		// %% tm:gate is a meta line (untested block only).
		if strings.HasPrefix(t, "%% tm:gate ") {
			comments := p.takePending()
			p.pos++
			meta, err := parseGateMeta(t, comments)
			if err != nil {
				return err
			}
			if block != BlockUntested {
				return fmt.Errorf("graph: tm:gate meta outside untested block")
			}
			g.UntestedMetas = append(g.UntestedMetas, meta)
			continue
		}

		// %% comment line (attached to the next non-comment line).
		if strings.HasPrefix(t, "%%") {
			p.pendingComments = append(p.pendingComments, t)
			p.pos++
			continue
		}

		// Edge or node declaration.
		comments := p.takePending()
		p.pos++
		if err := p.parseBlockLine(g, block, t, comments); err != nil {
			return err
		}
	}

	return nil
}

// takePending returns and clears the accumulated pending comments.
func (p *parser) takePending() []string {
	if len(p.pendingComments) == 0 {
		return nil
	}
	c := p.pendingComments
	p.pendingComments = nil
	return c
}

// parseBlockLine dispatches a non-comment, non-meta, non-blank block line.
func (p *parser) parseBlockLine(g *Graph, block Block, t string, comments []string) error {
	// Detect edge lines: structural (-->) or labeled (--"..."-->).
	if strings.Contains(t, "-->") {
		return parseEdge(g, t, comments)
	}
	// Node declaration: contains ["
	if strings.Contains(t, `["`) {
		return parseNode(g, block, t, comments)
	}
	return fmt.Errorf("graph: unrecognized line in block %s: %q", blockIDStr(block), t)
}

// parseSubgraphHeader parses a line like:
//
//	subgraph passed["Concepts User understands"]
func parseSubgraphHeader(line string) (id, title string, err error) {
	rest := strings.TrimPrefix(line, "subgraph ")
	idx := strings.Index(rest, `["`)
	if idx < 0 {
		return "", "", fmt.Errorf("graph: invalid subgraph header: %q", line)
	}
	id = rest[:idx]
	rest = rest[idx+2:]
	end := strings.LastIndex(rest, `"]`)
	if end < 0 {
		return "", "", fmt.Errorf("graph: unclosed title in subgraph header: %q", line)
	}
	title = rest[:end]
	return id, title, nil
}

// parseGateMeta parses a %% tm:gate line:
//
//	%% tm:gate <concept> base=<N>
func parseGateMeta(t string, comments []string) (GateMeta, error) {
	rest := strings.TrimPrefix(t, "%% tm:gate ")
	parts := strings.Fields(rest)
	if len(parts) != 2 {
		return GateMeta{}, fmt.Errorf("graph: invalid tm:gate line: %q", t)
	}
	if !strings.HasPrefix(parts[1], "base=") {
		return GateMeta{}, fmt.Errorf("graph: invalid tm:gate line (missing base=): %q", t)
	}
	baseStr := parts[1][5:]
	if !isDigits(baseStr) || len(baseStr) == 0 {
		return GateMeta{}, fmt.Errorf("graph: invalid base value in tm:gate: %q", t)
	}
	return GateMeta{
		Concept:         parts[0],
		Base:            parseDigits(baseStr),
		LeadingComments: comments,
	}, nil
}

// parseNode parses a node declaration line:
//
//	id["label"]
//	id["label"]:::class
func parseNode(g *Graph, block Block, t string, comments []string) error {
	idx := strings.Index(t, `["`)
	if idx < 0 {
		return fmt.Errorf("graph: invalid node declaration: %q", t)
	}
	id := t[:idx]
	rest := t[idx+2:] // after ["

	endIdx := strings.LastIndex(rest, `"]`)
	if endIdx < 0 {
		return fmt.Errorf("graph: unclosed label in node: %q", t)
	}
	label := rest[:endIdx]
	suffix := rest[endIdx+2:] // after "]

	class := ""
	if strings.HasPrefix(suffix, ":::") {
		class = suffix[3:]
	}

	switch {
	case ValidQuestionID(id):
		if block != BlockTesting {
			return fmt.Errorf("graph: question %q declared outside testing block", id)
		}
		qn := parseQuestionNode(id, label, class, comments)
		g.TestingItems = append(g.TestingItems, TestingItem{Q: qn})

	case ValidAnswerID(id):
		if block != BlockTesting {
			return fmt.Errorf("graph: answer %q declared outside testing block", id)
		}
		an := parseAnswerNode(id, label, class, comments)
		g.TestingItems = append(g.TestingItems, TestingItem{A: an})

	default:
		if block == BlockTesting {
			return fmt.Errorf("graph: concept %q declared in testing block", id)
		}
		cn := parseConceptNode(id, block, label, class, comments)
		if block == BlockPassed {
			g.PassedConcepts = append(g.PassedConcepts, cn)
		} else {
			g.UntestedConcepts = append(g.UntestedConcepts, cn)
		}
	}

	return nil
}

// parseConceptNode builds a ConceptNode from the raw (escaped) label.
// Label fields are separated by "<br/>": scope[, "GAP: " gap][, cite...].
func parseConceptNode(id string, block Block, label, class string, comments []string) *ConceptNode {
	cn := &ConceptNode{
		ID:              id,
		Block:           block,
		Class:           class,
		LeadingComments: comments,
	}
	parts := strings.Split(label, "<br/>")
	cn.Scope = Unescape(parts[0])
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, "GAP: ") {
			cn.GAP = Unescape(strings.TrimPrefix(part, "GAP: "))
		} else {
			// Citation field; may be comma-separated.
			for _, c := range strings.Split(part, ",") {
				c = strings.TrimSpace(c)
				if c != "" {
					cn.Cites = append(cn.Cites, c)
				}
			}
		}
	}
	return cn
}

// parseQuestionNode builds a QuestionNode from the raw label.
// Label format: scope followed by a "<br/>" separator and then the citation.
func parseQuestionNode(id, label, class string, comments []string) *QuestionNode {
	qn := &QuestionNode{
		ID:              id,
		Class:           class,
		LeadingComments: comments,
	}
	parts := strings.SplitN(label, "<br/>", 2)
	qn.Scope = Unescape(parts[0])
	if len(parts) > 1 {
		qn.Cite = parts[1] // citation is not unescaped (raw file:a-b)
	}
	return qn
}

// parseAnswerNode builds an AnswerNode from the raw label.
// Label format: [OOS<br/>][ASKED: <wording><br/>]<body>
// Mirrors the OOS prefix handling in parseConceptNode's GAP: prefix pattern.
func parseAnswerNode(id, label, class string, comments []string) *AnswerNode {
	an := &AnswerNode{
		ID:              id,
		Class:           class,
		LeadingComments: comments,
	}
	// Split into fields; consume prefix fields by name; last field is the body.
	parts := strings.Split(label, "<br/>")
	i := 0

	// Consume optional OOS prefix.
	if i < len(parts) && parts[i] == "OOS" {
		an.OOS = true
		i++
	}

	// Consume optional ASKED: field.
	if i < len(parts) && strings.HasPrefix(parts[i], "ASKED: ") {
		an.Asked = Unescape(strings.TrimPrefix(parts[i], "ASKED: "))
		i++
	}

	// Remaining field(s) are the body. Re-join with <br/> in case the body
	// itself was split (should not happen since Escape encodes < and >, but
	// defensive: reassemble any tail).
	if i < len(parts) {
		an.Label = Unescape(strings.Join(parts[i:], "<br/>"))
	}
	return an
}

// parseEdge parses a structural edge ("from --> to") or a labeled edge
// ("from --label--> to" with a quoted label).
func parseEdge(g *Graph, t string, comments []string) error {
	var from, to, label string

	// Try labeled edge first: look for --" pattern.
	if i := strings.Index(t, ` --"`); i >= 0 {
		from = t[:i]
		rest := t[i+4:] // after --"
		end := strings.Index(rest, `"-->`)
		if end < 0 {
			return fmt.Errorf("graph: invalid labeled edge: %q", t)
		}
		label = Unescape(rest[:end])
		to = strings.TrimSpace(rest[end+4:])
	} else if i := strings.Index(t, " --> "); i >= 0 {
		from = t[:i]
		to = t[i+5:]
	} else {
		return fmt.Errorf("graph: invalid edge: %q", t)
	}

	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)

	g.Edges = append(g.Edges, &Edge{
		From:            from,
		To:              to,
		Label:           label,
		LeadingComments: comments,
	})
	return nil
}
