package cli_test

import (
	"os"
	"strings"
	"testing"
)

// cardsLineGraph is a current-format graph whose untested concept carries a
// %% tm:cards line (§4.6).
func cardsLineGraph(format string) string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format ` + format + `
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
        %% tm:cards mycon
    end
    subgraph reserve["Concepts held in reserve"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
}

// blockOf returns the text of the named subgraph in a written graph.
func blockOf(t *testing.T, file, block string) string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	start := strings.Index(s, "subgraph "+block+"[")
	if start < 0 {
		t.Fatalf("no %s block in:\n%s", block, s)
	}
	return s[start : start+strings.Index(s[start:], "    end\n")]
}

func TestCardsLine_MovesWithItsConcept(t *testing.T) {
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, cardsLineGraph("3"))

	if _, errOut, code := run(t, "reserve", "mycon"); code != 0 {
		t.Fatalf("reserve: exit %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(blockOf(t, file, "reserve"), "%% tm:cards mycon") {
		t.Errorf("cards line did not move to reserve:\n%s", blockOf(t, file, "reserve"))
	}
	if strings.Contains(blockOf(t, file, "untested"), "tm:cards") {
		t.Errorf("cards line left behind in untested")
	}
	lintFile(t, file, dir)
}

func TestCardsLine_RemovedWithItsConcept(t *testing.T) {
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, cardsLineGraph("3"))

	if _, errOut, code := run(t, "drop", "mycon"); code != 0 {
		t.Fatalf("drop: exit %d; stderr:\n%s", code, errOut)
	}
	data, _ := os.ReadFile(file)
	if strings.Contains(string(data), "tm:cards") {
		t.Errorf("cards line survived the drop:\n%s", data)
	}
	lintFile(t, file, dir)
}

func TestMigrate_Format2ChangesOnlyTheMarker(t *testing.T) {
	dir, _ := qSetupDir(t)
	src := strings.Replace(cardsLineGraph("2"), "        %% tm:cards mycon\n", "", 1)
	file := qWriteGraph(t, dir, src)

	// Every other command refuses a format-2 graph and names the fix.
	_, errOut, code := run(t, "status")
	if code != 1 || !strings.Contains(errOut, "g.mmd is format 2, this is tm format 3") ||
		!strings.Contains(errOut, "fix: tm migrate") {
		t.Fatalf("status on format 2: want exit 1 with migrate fix, got %d:\n%s", code, errOut)
	}

	out, errOut, code := run(t, "migrate")
	if code != 0 {
		t.Fatalf("migrate: exit %d; stderr:\n%s", code, errOut)
	}
	// The plain-path citation is not run through the format-1 rules, so it
	// is neither rewritten nor reported as left.
	if strings.TrimSpace(out) != "migrated g.mmd format 2 -> 3: 0 rewritten, 0 left" {
		t.Errorf("migrate output: got %q", out)
	}
	data, _ := os.ReadFile(file)
	want := strings.Replace(src, "tm:format 2", "tm:format 3", 1)
	if string(data) != want {
		t.Errorf("want only the marker changed.\nwant:\n%s\ngot:\n%s", want, data)
	}
	if _, errOut, code := run(t, "status"); code != 0 {
		t.Errorf("status after migrate: exit %d:\n%s", code, errOut)
	}
}
