// External (black-box) property tests; using package graph_test avoids the
// import cycle that would arise if this file were package graph and imported
// internal/tools/gen (which itself imports internal/graph).
package graph_test

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/tools/gen"
)

// seedList returns the seeds used for property tests: 0..99 plus a handful of
// interesting values.
func seedList() []int64 {
	seeds := make([]int64, 0, 110)
	for i := int64(0); i < 100; i++ {
		seeds = append(seeds, i)
	}
	seeds = append(seeds, []int64{1337, 42424242, 0x7fffffff, 999999}...)
	return seeds
}

// TestGraphProperty_RoundTrip verifies that Write(Parse(Write(g))) == Write(g)
// for random seeded valid graphs — i.e. the serialisation is stable.
func TestGraphProperty_RoundTrip(t *testing.T) {
	for _, seed := range seedList() {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Parallel()
			r := rand.New(rand.NewSource(seed))
			g := gen.GraphRand(r)

			out1 := graph.Write(g)

			g2, err := graph.Parse(out1)
			if err != nil {
				t.Fatalf("Parse(Write(g)) failed for seed %d: %v\nOutput:\n%s",
					seed, err, out1)
			}

			// parse(write(g)) must equal g structurally.
			if !reflect.DeepEqual(g, g2) {
				t.Errorf("seed %d: Parse(Write(g)) != g (model round-trip failed)", seed)
			}

			out2 := graph.Write(g2)
			if !bytes.Equal(out1, out2) {
				t.Errorf("round-trip not stable for seed %d\nfirst  Write:\n%s\nsecond Write:\n%s",
					seed, out1, out2)
			}
		})
	}
}

// TestGraphProperty_Structure verifies structural invariants of Write output:
//  1. Three blocks in fixed order (passed, untested, testing).
//  2. Each question has a valid batch class.
//  3. Each edge appears in the block required by the §4.2 rule.
//  4. In each block, all declarations appear before all edges.
func TestGraphProperty_Structure(t *testing.T) {
	for _, seed := range seedList() {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Parallel()
			r := rand.New(rand.NewSource(seed))
			g := gen.GraphRand(r)
			out := string(graph.Write(g))

			checkWriteStructure(t, g, out, seed)
		})
	}
}

// checkWriteStructure asserts structural invariants for one generated graph.
func checkWriteStructure(t *testing.T, g *graph.Graph, out string, seed int64) {
	t.Helper()

	// 1. Three blocks in fixed order.
	passedIdx := strings.Index(out, "subgraph passed")
	untestedIdx := strings.Index(out, "subgraph untested")
	testingIdx := strings.Index(out, "subgraph testing")
	if passedIdx < 0 || untestedIdx < 0 || testingIdx < 0 {
		t.Errorf("seed %d: missing one or more subgraph blocks", seed)
		return
	}
	if passedIdx >= untestedIdx || untestedIdx >= testingIdx {
		t.Errorf("seed %d: blocks not in order passed=%d untested=%d testing=%d",
			seed, passedIdx, untestedIdx, testingIdx)
	}

	// 2. Each question has a valid batch class.
	for _, item := range g.TestingItems {
		if item.Q != nil && !graph.ValidBatchID(item.Q.Class) {
			t.Errorf("seed %d: question %q has invalid batch class %q",
				seed, item.Q.ID, item.Q.Class)
		}
	}

	// 3. Each edge appears in the correct block per the §4.2 rule.
	// The generator uses the naming convention: pc* → passed, uc* → untested,
	// q*/a* → testing.  We determine the expected block from that convention.
	regions := [3]string{
		out[passedIdx:untestedIdx],
		out[untestedIdx:testingIdx],
		out[testingIdx:],
	}
	for _, e := range g.Edges {
		home := edgeBlock(e)

		// Reconstruct the exact edge text that the writer would emit.
		edgeStr := e.From + " --> " + e.To
		if e.Label != "" {
			edgeStr = e.From + ` --"` + graph.Escape(e.Label) + `"--> ` + e.To
		}
		if !strings.Contains(regions[int(home)], edgeStr) {
			t.Errorf("seed %d: edge %q not found in expected block %q\nFull output:\n%s",
				seed, edgeStr, blockName(home), out)
		}
	}

	// 4. In each block, all declarations appear before all edges.
	checkDeclsBeforeEdges(t, seed, "passed", out[passedIdx:untestedIdx])
	checkDeclsBeforeEdges(t, seed, "untested", out[untestedIdx:testingIdx])
	checkDeclsBeforeEdges(t, seed, "testing", out[testingIdx:])
}

// edgeBlock determines the expected home block for edge e using the §4.2 rule.
// It relies on the generator's naming convention: pc* → passed, uc* → untested,
// q*/a* → testing.
func edgeBlock(e *graph.Edge) graph.Block {
	blockOf := func(id string) graph.Block {
		switch {
		case len(id) >= 1 && (id[0] == 'q' || id[0] == 'a'):
			return graph.BlockTesting
		case strings.HasPrefix(id, "uc"):
			return graph.BlockUntested
		default:
			return graph.BlockPassed
		}
	}
	fb := blockOf(e.From)
	tb := blockOf(e.To)
	if tb > fb {
		return tb
	}
	return fb
}

// blockName returns the subgraph id string for a block.
func blockName(b graph.Block) string {
	switch b {
	case graph.BlockPassed:
		return "passed"
	case graph.BlockUntested:
		return "untested"
	case graph.BlockTesting:
		return "testing"
	default:
		return "unknown"
	}
}

// checkDeclsBeforeEdges verifies that within blockOut all node declaration
// lines (containing `["`) appear before all edge lines (containing ` --> `).
func checkDeclsBeforeEdges(t *testing.T, seed int64, blkName, blockOut string) {
	t.Helper()

	lines := strings.Split(blockOut, "\n")
	lastDeclLine := -1
	firstEdgeLine := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip the subgraph header line, "end", and blank lines.
		if strings.HasPrefix(trimmed, "subgraph ") || trimmed == "end" || trimmed == "" {
			continue
		}
		// A node declaration contains `["` but not ` --> `.
		if strings.Contains(trimmed, `["`) && !strings.Contains(trimmed, " --> ") {
			if i > lastDeclLine {
				lastDeclLine = i
			}
		}
		// An edge line contains "-->": either " --> " (structural) or
		// `--"..."--> ` (labelled). The ">" in node labels is escaped to
		// "#gt;" so "-->" never appears inside a declaration.
		if strings.Contains(trimmed, "-->") {
			if firstEdgeLine < 0 {
				firstEdgeLine = i
			}
		}
	}

	if lastDeclLine >= 0 && firstEdgeLine >= 0 && firstEdgeLine < lastDeclLine {
		t.Errorf("seed %d: block %q: edge line (%d) precedes declaration line (%d)",
			seed, blkName, firstEdgeLine, lastDeclLine)
	}
}
