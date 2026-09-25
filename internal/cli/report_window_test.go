package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/cite"
)

// reportWindowSrc writes src.txt with n lines ("line I" padded with 'x' to
// width) into dir, sets TM_SRC_ROOT, and returns the file's line slice so a
// caller can hash a range.
func reportWindowSrc(t *testing.T, dir string, n, width int) []string {
	t.Helper()
	lines := make([]string, n)
	for i := 1; i <= n; i++ {
		line := fmt.Sprintf("line %d", i)
		if pad := width - len(line); pad > 0 {
			line += strings.Repeat("x", pad)
		}
		lines[i-1] = line
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	return lines
}

// TestReport_FulltextWindow verifies that tm report --fulltext bounds each
// concept's inlined source to one window (§6): the fenced block holds at most a
// window of lines and, when text is cut, the concept ends with a more: trailer
// naming the citation's next range; a citation within the window has no trailer.
func TestReport_FulltextWindow(t *testing.T) {
	tests := []struct {
		name        string
		n           int
		width       int
		start, end  int
		wantFenced  int    // numbered source lines expected inside the fence
		trailerTail string // "" means no trailer expected
	}{
		{
			name:        "over by lines",
			n:           300,
			start:       1,
			end:         300,
			wantFenced:  200,
			trailerTail: "201-300",
		},
		{
			name:        "over by chars whole-line cut",
			n:           100,
			width:       100, // 101 bytes/line; 79 lines reach 7979, 80th would pass 8000
			start:       1,
			end:         100,
			wantFenced:  79,
			trailerTail: "80-100",
		},
		{
			name:       "within window has no trailer",
			n:          50,
			start:      1,
			end:        50,
			wantFenced: 50,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			t.Setenv("TM_FILE", "")
			dir := t.TempDir()
			lines := reportWindowSrc(t, dir, tc.n, tc.width)

			hash := cite.Hash(strings.Join(lines[tc.start-1:tc.end], "\n"))
			cited := fmt.Sprintf("%s@src.txt:%d-%d", hash, tc.start, tc.end)

			graph := qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        big["Big concept<br/>` + cited + `"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
`
			file := filepath.Join(dir, "g.mmd")
			if err := os.WriteFile(file, []byte(graph), 0o644); err != nil {
				t.Fatal(err)
			}

			out, errOut, code := run(t, "report", "--file", file, "big", "--fulltext")
			if code != 0 {
				t.Fatalf("tm report exit %d; stderr:\n%s", code, errOut)
			}

			// Count source lines inside the fenced block.
			fenced, inFence, trailer := 0, false, ""
			for _, ln := range strings.Split(out, "\n") {
				switch {
				case ln == "```":
					inFence = !inFence
				case strings.HasPrefix(ln, "more: "):
					trailer = ln
				case inFence:
					fenced++
				}
			}
			if fenced != tc.wantFenced {
				t.Errorf("fenced lines = %d, want %d;\ngot:\n%s", fenced, tc.wantFenced, out)
			}
			if tc.trailerTail == "" {
				if trailer != "" {
					t.Errorf("unexpected trailer %q", trailer)
				}
				return
			}
			want := "more: tm src src.txt " + tc.trailerTail
			if trailer != want {
				t.Errorf("trailer = %q, want %q", trailer, want)
			}
		})
	}
}

// TestReport_FulltextWindow_MultiCite verifies the window is one budget across a
// concept's citations (§6): the first citation spends most of it, the window
// runs out inside the second, and the concept ends with a single trailer naming
// the second citation's next range.
func TestReport_FulltextWindow_MultiCite(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	dir := t.TempDir()
	lines := reportWindowSrc(t, dir, 300, 0)

	h1 := cite.Hash(strings.Join(lines[0:150], "\n"))   // src.txt:1-150
	h2 := cite.Hash(strings.Join(lines[150:250], "\n")) // src.txt:151-250
	cites := fmt.Sprintf("%s@src.txt:1-150, %s@src.txt:151-250", h1, h2)

	graph := qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        big["Big concept<br/>` + cites + `"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
`
	file := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(file, []byte(graph), 0o644); err != nil {
		t.Fatal(err)
	}

	out, errOut, code := run(t, "report", "--file", file, "big", "--fulltext")
	if code != 0 {
		t.Fatalf("tm report exit %d; stderr:\n%s", code, errOut)
	}

	fenced, inFence, trailer := 0, false, ""
	for _, ln := range strings.Split(out, "\n") {
		switch {
		case ln == "```":
			inFence = !inFence
		case strings.HasPrefix(ln, "more: "):
			trailer = ln
		case inFence:
			fenced++
		}
	}
	// 150 lines from the first citation plus 50 from the second reach the 200
	// line budget; the second citation is cut at file line 200.
	if fenced != 200 {
		t.Errorf("fenced lines = %d, want 200;\ngot:\n%s", fenced, out)
	}
	if want := "more: tm src src.txt 201-250"; trailer != want {
		t.Errorf("trailer = %q, want %q", trailer, want)
	}
}
