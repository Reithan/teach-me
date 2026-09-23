package graph_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// TestLongURILabelSize verifies that the graph parser and writer round-trip a
// citation whose locator is a 200-character HTTPS URL, and records the Mermaid
// label byte count (text inside ["..."]) for that node.
//
// Design note (M9 §9): the citation format is <hash>@<url>:START-END.  A
// 200-character https URL produces a citation string of 217 bytes
// (12 hash + 1 "@" + 200 URL + 4 ":1-5") and, with a short scope prefix, a
// Mermaid node label of 227 bytes — well within any known Mermaid renderer
// limit.  Both graph.Parse and the graph.Write round-trip accept it without
// truncation or error.
func TestLongURILabelSize(t *testing.T) {
	t.Parallel()

	// Construct a 200-character HTTPS URL: "https://" + 192 "a"s.
	const hashStr = "000000000000"
	url := "https://" + strings.Repeat("a", 192) // 8+192 = 200 chars
	citeStr := hashStr + "@" + url + ":1-5"      // 12+1+200+4 = 217 chars

	const scope = "scope"
	// Mermaid label text = scope + "<br/>" + cite = 5+5+217 = 227 bytes.
	const wantLabelBytes = 227

	g := &graph.Graph{
		Frontmatter:   "",
		PassedTitle:   "Concepts User understands",
		UntestedTitle: "Concepts User has not been tested on",
		TestingTitle:  "Open tests validating and teaching User understanding",
		TestingItems: []graph.TestingItem{
			{
				Q: &graph.QuestionNode{
					ID:    "q1",
					Scope: scope,
					Cite:  citeStr,
					Class: "probe_1",
				},
			},
		},
	}

	// graph.Write must produce a valid Mermaid file.
	data := graph.Write(g)

	// Find the q1 declaration line to measure the label.
	labelBytes := 0
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, `q1["`) {
			continue
		}
		// Label is the text between `["` and the closing `"]`.
		start := strings.Index(trimmed, `["`)
		end := strings.LastIndex(trimmed, `"]`)
		if start < 0 || end <= start {
			t.Fatalf("cannot find label in line: %q", line)
		}
		labelBytes = end - start - 2 // subtract the `["` prefix
		break
	}
	if labelBytes == 0 {
		t.Fatalf("q1 node not found in graph output:\n%s", data)
	}
	if labelBytes != wantLabelBytes {
		t.Errorf("label size: got %d bytes, want %d", labelBytes, wantLabelBytes)
	}

	// graph.Parse must round-trip the written file without error.
	g2, err := graph.Parse(data)
	if err != nil {
		t.Fatalf("graph.Parse round-trip: %v\ndata:\n%s", err, data)
	}
	if len(g2.TestingItems) == 0 || g2.TestingItems[0].Q == nil {
		t.Fatal("round-tripped graph has no question node")
	}
	got := g2.TestingItems[0].Q.Cite
	if got != citeStr {
		t.Errorf("round-tripped cite: got %q, want %q", got, citeStr)
	}

	// Verify the label text is present in the raw bytes (no truncation).
	wantLabel := scope + "<br/>" + citeStr
	if !bytes.Contains(data, []byte(wantLabel)) {
		t.Errorf("label text not found verbatim in output; want %d-byte string starting with %q",
			len(wantLabel), wantLabel[:30])
	}

	t.Logf("long URI label size: %d bytes (cite=%d, scope=%d, <br/>=5)",
		labelBytes, len(citeStr), len(scope))
}
