// Package gen provides a seeded random valid graph generator for use by
// property tests and the conformance corpus tool.
//
// All generated graphs satisfy the structural invariants of spec section 4:
// valid IDs, three blocks in fixed order, declarations before edges, each edge
// in the block required by §4.2, one batch class per question, concept edges
// forming a DAG with relation labels.
//
// Labels include adversarial content: every escapable character (" ' # < >),
// Mermaid keyword near-misses embedded in scope text, and non-ASCII/Unicode.
package gen

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/graph"
)

// genCite returns a hashed citation string "<hash>@<file>:<start>-<end>".
// The hash is cite.Hash of the fake content "line start\nline start+1\n...\nline end\n"
// (cite.Hash calls Normalize internally, so the trailing newline is stripped before hashing).
// This produces deterministic, syntactically-valid citations for generated graphs
// that do not correspond to any real source file.
func genCite(file string, start, end int) string {
	var sb strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	h := cite.Hash(sb.String())
	return fmt.Sprintf("%s@%s:%d-%d", h, file, start, end)
}

// defaultFrontmatter is the canonical frontmatter block from spec section 4.
const defaultFrontmatter = `---
config:
  look: classic
  darkMode: true
  theme: dark
  layout: elk
  elk:
    mergeEdges: true
    nodePlacementStrategy: NETWORK_SIMPLEX
---
`

// adversarialScopes contains scope texts exercising every escapable character
// plus Mermaid keyword near-misses and non-ASCII content.
var adversarialScopes = []string{
	`"double quoted" scope`,
	`it's apostrophe scope`,
	`hash #35; literal`,
	`angle <brackets> scope`,
	`contains > and < both`,
	`subgraph embedded keyword`,
	`end of line text`,
	`classDef style reference`,
	`click handler text`,
	`default value text`,
	`flowchart TB reference`,
	`日本語テキスト`,
	`Ünïcödé: 50% sure?`,
	`"multi" 'quote' #hash <angle>`,
	`a "quote" it's #test <b> >c<`,
	`end subgraph classDef click style default`,
	`a → b (unicode arrow)`,
	`concept: <b>bold</b> #35;`,
}

// adversarialGAPs contains GAP texts with escapable characters.
var adversarialGAPs = []string{
	`gaps "quoted" and 'quoted'`,
	`missing #35; detail`,
	`<misunderstanding> noted`,
	`treats > as < valid`,
	`subgraph confusion classDef`,
}

// relLabels contains edge relation labels with escapable characters.
var relLabels = []string{
	"depends on",
	"requires",
	"enables",
	"constrains",
	"leads to",
	`"quoted" rel`,
	`<angle> rel`,
	`it's a rel`,
	`#hash rel`,
	`a > b rel`,
	`subgraph end rel`,
}

// answerTexts contains answer label texts with escapable characters.
var answerTexts = []string{
	`correct "answer" here`,
	`it's the right one`,
	`a #hash answer`,
	`<html> answer text`,
	`answer > expected`,
	`日本語の回答`,
	`Ünïcödé answer 50%`,
	`subgraph end classDef embedded`,
	`a "b" c 'it#35;s' <d>`,
}

// answerClasses is the set of valid answer class names.
var answerClasses = []string{"pending", "pass", "fail", "unclear"}

// Graph generates a deterministic random valid graph for the given seed.
func Graph(seed int64) *graph.Graph {
	r := rand.New(rand.NewSource(seed))
	return GraphRand(r)
}

// GraphRand generates a valid graph using the provided random source.
// It is deterministic: given the same r state, it produces the same graph.
func GraphRand(r *rand.Rand) *graph.Graph {
	g := &graph.Graph{
		Frontmatter:   defaultFrontmatter,
		PassedTitle:   "Concepts User understands",
		UntestedTitle: "Concepts User has not been tested on",
		TestingTitle:  "Open tests validating and teaching User understanding",
	}

	numPassed := 1 + r.Intn(3)   // 1-3 passed concepts
	numUntested := 1 + r.Intn(3) // 1-3 untested concepts

	// Generate passed concepts with adversarial labels.
	for i := range numPassed {
		id := fmt.Sprintf("pc%d", i+1)
		cn := &graph.ConceptNode{
			ID:    id,
			Scope: pick(r, adversarialScopes),
			Cites: []string{genCite("src.txt", i*20+1, i*20+20)},
			Block: graph.BlockPassed,
		}
		if r.Intn(3) == 0 {
			cn.Cites = append(cn.Cites, genCite("extra.txt", i*5+1, i*5+10))
		}
		g.PassedConcepts = append(g.PassedConcepts, cn)
	}

	// DAG edges among passed concepts (forward-only to guarantee no cycles).
	for i := 1; i < numPassed; i++ {
		if r.Intn(2) == 0 {
			g.Edges = append(g.Edges, &graph.Edge{
				From:  fmt.Sprintf("pc%d", i),
				To:    fmt.Sprintf("pc%d", i+1),
				Label: pick(r, relLabels),
			})
		}
	}

	// Generate untested concepts with adversarial labels.
	for i := range numUntested {
		id := fmt.Sprintf("uc%d", i+1)
		cn := &graph.ConceptNode{
			ID:    id,
			Scope: pick(r, adversarialScopes),
			Cites: []string{genCite("src.txt", (i+numPassed)*20+1, (i+numPassed)*20+20)},
			Block: graph.BlockUntested,
		}
		if r.Intn(3) == 0 {
			cn.GAP = pick(r, adversarialGAPs)
		}
		g.UntestedConcepts = append(g.UntestedConcepts, cn)
	}

	// Edges from passed→untested (placed in untested by §4.2 rule).
	for i := range numUntested {
		ucID := fmt.Sprintf("uc%d", i+1)
		if numPassed > 0 && r.Intn(2) == 0 {
			pcIdx := r.Intn(numPassed)
			g.Edges = append(g.Edges, &graph.Edge{
				From:  fmt.Sprintf("pc%d", pcIdx+1),
				To:    ucID,
				Label: pick(r, relLabels),
			})
		}
		// Untested→untested DAG edges (forward-only).
		if i > 0 && r.Intn(2) == 0 {
			g.Edges = append(g.Edges, &graph.Edge{
				From:  fmt.Sprintf("uc%d", i),
				To:    ucID,
				Label: pick(r, relLabels),
			})
		}
	}

	// Generate 0-2 probe batches with questions and optional answers.
	// All testing items are placed in the testing block by §4.2.
	numBatches := r.Intn(3) // 0-2
	qCounter := 0
	for bn := range numBatches {
		batchClass := fmt.Sprintf("probe_%d", bn+1)
		conceptID := fmt.Sprintf("uc%d", 1+r.Intn(numUntested))
		numQs := 1 + r.Intn(2) // 1-2 questions per batch

		for range numQs {
			qCounter++
			qID := fmt.Sprintf("q%d", qCounter)
			aID := fmt.Sprintf("a%d", qCounter)

			qn := &graph.QuestionNode{
				ID:    qID,
				Scope: pick(r, adversarialScopes),
				Cite:  genCite("src.txt", qCounter*10+1, qCounter*10+15),
				Class: batchClass,
			}
			g.TestingItems = append(g.TestingItems, graph.TestingItem{Q: qn})

			// concept → question structural edge (home block: testing, since testing > untested).
			g.Edges = append(g.Edges, &graph.Edge{From: conceptID, To: qID})

			// Optionally add an answer node with question→answer edge.
			if r.Intn(2) == 0 {
				an := &graph.AnswerNode{
					ID:    aID,
					Label: pick(r, answerTexts),
					Class: answerClasses[r.Intn(len(answerClasses))],
				}
				g.TestingItems = append(g.TestingItems, graph.TestingItem{A: an})
				g.Edges = append(g.Edges, &graph.Edge{From: qID, To: aID})
			}
		}
	}

	return g
}

// BuildSidecar returns a map from node ID to the expected Mermaid subgraph ID.
// Because the writer always declares nodes before edges (§4.1), Mermaid's
// first-mention rule assigns every node to the subgraph containing its
// declaration.
func BuildSidecar(g *graph.Graph) map[string]string {
	s := make(map[string]string)
	for _, c := range g.PassedConcepts {
		s[c.ID] = "passed"
	}
	for _, c := range g.UntestedConcepts {
		s[c.ID] = "untested"
	}
	for _, item := range g.TestingItems {
		if item.Q != nil {
			s[item.Q.ID] = "testing"
		}
		if item.A != nil {
			s[item.A.ID] = "testing"
		}
	}
	return s
}

func pick(r *rand.Rand, slice []string) string {
	return slice[r.Intn(len(slice))]
}
