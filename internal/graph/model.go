// Package graph implements the tm graph file model, parser, writer, and
// label-escaping for the Mermaid subset described in spec section 4.
package graph

// Block identifies one of the three fixed subgraph sections, in file order.
type Block int

const (
	// BlockPassed is the "passed" subgraph: concepts the user has demonstrated.
	BlockPassed Block = 0
	// BlockUntested is the "untested" subgraph: concepts not yet tested.
	BlockUntested Block = 1
	// BlockTesting is the "testing" subgraph: open questions and answers.
	BlockTesting Block = 2
)

// ConceptNode is a concept in the passed or untested block.
type ConceptNode struct {
	ID    string
	Scope string
	// GAP is the gap annotation text, empty if no GAP field is present.
	GAP string
	// Cites holds the raw citation strings (e.g. "raft.txt:120-188").
	Cites []string
	// Block records which subgraph the concept belongs to.
	Block Block
	// Class is the :::class suffix if present; always empty for valid concepts.
	// §11.7 requires concepts to carry no class; lint reports a non-empty value.
	Class string
	// LeadingComments are %% comment lines immediately preceding this node.
	LeadingComments []string
}

// QuestionNode is a question in the testing block.
type QuestionNode struct {
	ID    string
	Scope string
	Cite  string
	// Class is the batch class, e.g. "probe_1" or "teach_3".
	Class           string
	LeadingComments []string
}

// AnswerNode is an answer in the testing block.
type AnswerNode struct {
	ID string
	// OOS is set when the answer is flagged out-of-scope (teach answers only).
	OOS bool
	// Asked holds the teacher's wording as recorded by tm answer --asked.
	// Empty when the flag was not supplied.
	Asked string
	// Label holds the unescaped answer text or grader summary.
	Label string
	// Class is the answer class: "pending", "pass", "fail", or "unclear".
	Class           string
	LeadingComments []string
}

// TestingItem holds exactly one of Q (QuestionNode) or A (AnswerNode).
type TestingItem struct {
	Q *QuestionNode
	A *AnswerNode
}

// Edge is a directed edge between two nodes.
type Edge struct {
	From string
	To   string
	// Label is empty for structural edges (-->) and non-empty for
	// concept-to-concept edges (--"label"-->).
	Label           string
	LeadingComments []string
}

// GateMeta holds the data from a %% tm:gate meta line.
type GateMeta struct {
	Concept         string
	Base            int
	LeadingComments []string
}

// Graph is the parsed, structured representation of a .mmd file.
type Graph struct {
	// Frontmatter is the verbatim YAML front-matter block (including --- fences),
	// terminated by a trailing newline. Empty string if the file has none.
	Frontmatter string

	// Subgraph titles as they appear in the file.
	PassedTitle   string
	UntestedTitle string
	TestingTitle  string

	// Concepts in the passed block, in declaration order.
	PassedConcepts []*ConceptNode

	// Gate meta lines and concepts in the untested block, in order.
	UntestedMetas    []GateMeta
	UntestedConcepts []*ConceptNode

	// Question and answer items in the testing block, in declaration order.
	TestingItems []TestingItem

	// All edges from the file, in the order they appear.
	// The writer applies the section 4.2 edge placement rule to determine
	// which block emits each edge.
	Edges []*Edge
}

// nodeBlocks builds a map from node ID to Block for all nodes in the graph.
func (g *Graph) nodeBlocks() map[string]Block {
	m := make(map[string]Block)
	for _, c := range g.PassedConcepts {
		m[c.ID] = BlockPassed
	}
	for _, c := range g.UntestedConcepts {
		m[c.ID] = BlockUntested
	}
	for _, item := range g.TestingItems {
		if item.Q != nil {
			m[item.Q.ID] = BlockTesting
		}
		if item.A != nil {
			m[item.A.ID] = BlockTesting
		}
	}
	return m
}

// edgeHomeBlock returns the block an edge belongs to per the 4.2 rule:
// the home block of whichever endpoint's block comes later in file order.
func edgeHomeBlock(e *Edge, blocks map[string]Block) Block {
	fb, fok := blocks[e.From]
	tb, tok := blocks[e.To]
	if !fok && !tok {
		return BlockPassed
	}
	if !fok {
		return tb
	}
	if !tok {
		return fb
	}
	if tb > fb {
		return tb
	}
	return fb
}
