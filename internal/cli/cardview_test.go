package cli_test

import (
	"os"
	"strings"
	"testing"
	"time"
)

// lapseRows returns review rows that take card n into review and then make
// it lapse the given number of times, ending in review and due next year.
func lapseRows(n, lapses int) []string {
	at := time.Now().Add(-240 * time.Hour).UTC()
	far := time.Now().Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339)
	row := func(rating, step int) string {
		at = at.Add(time.Hour)
		return `{"t":"` + at.Format(time.RFC3339) + `","ev":"review","n":` + string(rune('0'+n)) +
			`,"rating":` + string(rune('0'+rating)) + `,"via":"serve","s":5,"d":5,"step":` +
			string(rune('0'+step)) + `,"due":"` + far + `"}`
	}
	rows := make([]string, 0, 1+2*lapses)
	rows = append(rows, row(4, 0))
	for range lapses {
		rows = append(rows, row(1, 1), row(3, 0))
	}
	return rows
}

func TestStatus_CardsDueCount(t *testing.T) {
	file := dueSetup(t)
	if out := mustRun(t, "status"); !strings.HasPrefix(out, "passed 1  open 1  blocked 0  reserve 1  cards due 3\n") {
		t.Errorf("counts line: %q", out)
	}
	t.Setenv("TM_CARD_NEW", "0")
	if out := mustRun(t, "status"); strings.Contains(out, "cards due") {
		t.Errorf("nothing is due, so the count should be left out: %q", out)
	}

	// A broken cards file does not stop tm status.
	appendLine(t, file+".cards.jsonl", "not a row")
	out, errOut, code := run(t, "status")
	if code != 0 || !strings.HasPrefix(out, "passed 1  open 1  blocked 0  reserve 1\n") ||
		!strings.Contains(errOut, "line 4 is not a card row") {
		t.Errorf("broken cards file: exit %d, out %q, err %q", code, out, errOut)
	}
}

func TestStatus_LeechLine(t *testing.T) {
	file := dueSetup(t)
	t.Setenv("TM_CARD_NEW", "0")
	for _, row := range append(lapseRows(1, 4), lapseRows(2, 3)...) {
		appendLine(t, file+".cards.jsonl", row)
	}

	out := mustRun(t, "status")
	if !strings.Contains(out, "\ndone  passed  leech 1/3 cards\n") {
		t.Errorf("want a leech line for done:\n%s", out)
	}
	if out := mustRun(t, "status", "--passed"); strings.Count(out, "done  passed") != 1 {
		t.Errorf("--passed should print the concept once, with the leech note:\n%s", out)
	}
	t.Setenv("TM_CARD_LEECH", "3")
	if out := mustRun(t, "status"); !strings.Contains(out, "done  passed  leech 2/3 cards") {
		t.Errorf("TM_CARD_LEECH=3:\n%s", out)
	}
	t.Setenv("TM_CARD_LEECH", "5")
	if out := mustRun(t, "status"); strings.Contains(out, "leech") {
		t.Errorf("TM_CARD_LEECH=5 should show no leech:\n%s", out)
	}
}

func TestShow_ListsTheConceptsCards(t *testing.T) {
	dir, file := cardSetup(t)
	for _, name := range []string{"TM_CARD_RETENTION", "TM_CARD_NEW", "TM_CARD_LEECH"} {
		t.Setenv(name, "")
	}
	if out := mustRun(t, "show", "done"); strings.Contains(out, "cards") {
		t.Errorf("a concept with no cards should print no cards section:\n%s", out)
	}

	mustRun(t, "card", "add", "done", "src.txt:1-1", "front 1", "back 1")
	mustRun(t, "card", "add", "done", "src.txt:2-2", "front\n2", "back 2")
	mustRun(t, "card", "add", "done", "src.txt:3-3", "front 3", "back 3")
	mustRun(t, "review", "2", "good")
	mustRun(t, "card", "rm", "3")
	list := dir + "/list.tsv"
	if err := os.WriteFile(list, []byte("uno\tone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "card", "import", list, "--concept", "todo", "--origin", "user")

	if out := mustRun(t, "show", "done"); !strings.Contains(out, "\ncards 2\n  1 new  front 1\n  2 learning  front 2\n") {
		t.Errorf("show done:\n%s", out)
	}
	if out := mustRun(t, "show", "todo"); !strings.Contains(out, "\ncards 1\n  4 waiting  uno\n") {
		t.Errorf("show todo:\n%s", out)
	}

	appendLine(t, file+".cards.jsonl", "not a row")
	out, errOut, code := run(t, "show", "done")
	if code != 0 || strings.Contains(out, "cards") || !strings.Contains(errOut, "is not a card row") {
		t.Errorf("broken cards file: exit %d, out %q, err %q", code, out, errOut)
	}
}
