package cli_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// dueSetup makes a graph with three cards on the passed concept and clears
// the card settings from the environment.
func dueSetup(t *testing.T) (file string) {
	t.Helper()
	_, file = cardSetup(t)
	for _, name := range []string{"TM_CARD_RETENTION", "TM_CARD_NEW", "TM_CARD_LEECH"} {
		t.Setenv(name, "")
	}
	mustRun(t, "card", "add", "done", "src.txt:1-1", "front 1", "back 1")
	mustRun(t, "card", "add", "done", "src.txt:2-2", "front 2", "back 2")
	mustRun(t, "card", "add", "done", "src.txt:3-3", "front 3", "back 3")
	return file
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

type dueJSON struct {
	N       int      `json:"n"`
	Front   string   `json:"front"`
	Options []string `json:"options"`
}

func dueCards(t *testing.T) []dueJSON {
	t.Helper()
	var got []dueJSON
	out := mustRun(t, "due", "--format", "json")
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("tm due --format json printed %q: %v", out, err)
	}
	return got
}

func TestDue_PrintsNewCardsWithOptionsInAFreshOrder(t *testing.T) {
	dueSetup(t)

	orders := map[string]bool{}
	for range 25 {
		got := dueCards(t)
		if len(got) != 3 {
			t.Fatalf("want 3 due cards, got %+v", got)
		}
		order := make([]string, 0, len(got))
		for _, c := range got {
			order = append(order, c.Front)
			want := []string{"back 1", "back 2", "back 3"}
			opts := slices.Clone(c.Options)
			slices.Sort(opts)
			if !slices.Equal(opts, want) {
				t.Fatalf("card %d options %v, want %v in some order", c.N, c.Options, want)
			}
		}
		orders[strings.Join(order, ",")] = true
	}
	if len(orders) < 2 {
		t.Errorf("25 calls printed the cards in one order only: %v", orders)
	}

	lines := strings.Split(strings.TrimSpace(mustRun(t, "due", "--limit", "2")), "\n")
	if len(lines) != 2 {
		t.Fatalf("--limit 2 printed %d lines: %q", len(lines), lines)
	}
	if f := strings.Split(lines[0], " | "); len(f) != 5 || !strings.HasPrefix(f[1], "front ") {
		t.Errorf("want number, front, and three options on a line, got %q", lines[0])
	}
}

func TestDue_NothingDue(t *testing.T) {
	_, file := cardSetup(t)
	if out := mustRun(t, "due"); out != "" {
		t.Errorf("no cards: lines format printed %q", out)
	}
	if out := mustRun(t, "due", "--format", "json"); out != "[]\n" {
		t.Errorf("no cards: json format printed %q", out)
	}
	if _, err := os.Stat(file + ".cards.jsonl"); !os.IsNotExist(err) {
		t.Errorf("tm due should not create a cards file")
	}
}

func TestDue_ALoneCardHasNoOptions(t *testing.T) {
	cardSetup(t)
	mustRun(t, "card", "add", "done", "src.txt:1-1", "front", "back\nsecond line")
	if got := dueCards(t); len(got) != 1 || got[0].Options == nil || len(got[0].Options) != 0 {
		t.Errorf("want one card with an empty options list, got %+v", got)
	}
	if out := mustRun(t, "due"); out != "1 | front\n" {
		t.Errorf("lines format: %q", out)
	}
}

func TestDue_NewLimitAndUsageErrors(t *testing.T) {
	dueSetup(t)
	t.Setenv("TM_CARD_NEW", "1")
	if got := dueCards(t); len(got) != 1 || got[0].N != 1 {
		t.Errorf("TM_CARD_NEW=1: want card 1 only, got %+v", got)
	}
	wantRefusal(t, 3, "--limit", "due", "--limit", "0")
	wantRefusal(t, 3, "--limit", "due", "--limit", "x")
	t.Setenv("TM_CARD_NEW", "many")
	wantRefusal(t, 3, "TM_CARD_NEW", "due")
}

func TestReview_RatingAndPick(t *testing.T) {
	file := dueSetup(t)
	cardsFile := file + ".cards.jsonl"

	if out := mustRun(t, "review", "1", "good"); out != "ok\n" {
		t.Errorf("rating: printed %q", out)
	}
	if out := mustRun(t, "review", "2", "--pick", " back 2 "); out != "right\n" {
		t.Errorf("right pick: printed %q", out)
	}
	if out := mustRun(t, "review", "3", "--pick", "back 2"); out != "wrong | back 3\n" {
		t.Errorf("wrong pick: printed %q", out)
	}
	rows := strings.Split(strings.TrimSpace(readFile(t, cardsFile)), "\n")
	for i, want := range []string{
		`"ev":"review","n":1,"rating":3,"via":"rate","s":2.3065,"d":2.1181,"step":1,`,
		`"ev":"review","n":2,"rating":3,"via":"pick","s":2.3065,"d":2.1181,"step":1,`,
		`"ev":"review","n":3,"rating":1,"via":"pick","s":0.212,"d":6.4133,"step":2,`,
	} {
		if row := rows[3+i]; !strings.Contains(row, want) {
			t.Errorf("review row %d lacks %s:\n%s", i+1, want, row)
		}
	}
	if log := readFile(t, file+".jsonl"); strings.Contains(log, `"review"`) {
		t.Errorf("a review should not write the event log:\n%s", log)
	}

	// Every card is now on a step, so none counts as due, but with nothing
	// else left tm due shows them early.
	if got := dueCards(t); len(got) != 3 {
		t.Errorf("want the 3 step cards shown early, got %+v", got)
	}
	// A second good moves card 1 into review, due in 2 days.
	mustRun(t, "review", "1", "good")
	if out := mustRun(t, "card", "show", "1"); !strings.Contains(out, "state review") {
		t.Errorf("after two goods card 1 should be in review:\n%s", out)
	}
	if got := dueCards(t); len(got) != 2 {
		t.Errorf("card 1 should no longer be shown, got %+v", got)
	}
}

func TestDue_ACardInReviewIsDueOnItsDay(t *testing.T) {
	file := dueSetup(t)
	t.Setenv("TM_CARD_NEW", "0")
	if got := dueCards(t); len(got) != 0 {
		t.Fatalf("with no new cards allowed nothing should be due, got %+v", got)
	}
	past := time.Now().Add(-72 * time.Hour).UTC().Format(time.RFC3339)
	yesterday := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	appendLine(t, file+".cards.jsonl",
		`{"t":"`+past+`","ev":"review","n":2,"rating":4,"via":"serve","s":8.2956,"d":1,"step":0,"due":"`+yesterday+`"}`)
	if got := dueCards(t); len(got) != 1 || got[0].N != 2 {
		t.Errorf("want card 2 due, got %+v", got)
	}
}

func TestReview_Refusals(t *testing.T) {
	dir, file := cardSetup(t)
	for _, name := range []string{"TM_CARD_RETENTION", "TM_CARD_NEW", "TM_CARD_LEECH"} {
		t.Setenv(name, "")
	}
	list := dir + "/list.tsv"
	if err := os.WriteFile(list, []byte("uno\tone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "card", "import", list, "--concept", "todo", "--origin", "user") // card 1, waiting
	mustRun(t, "card", "add", "done", "src.txt:1-1", "front", "back")           // card 2
	mustRun(t, "card", "add", "done", "src.txt:2-2", "front b", "back b")       // card 3
	appendLine(t, file+".cards.jsonl", `{"t":"2026-11-11T00:00:00Z","ev":"hold","n":2,"on":true}`)
	mustRun(t, "card", "rm", "3")
	before := readFile(t, file+".cards.jsonl")

	wantRefusal(t, 1, "card 1 is waiting: todo is not passed", "review", "1", "good")
	wantRefusal(t, 1, "card 2 is held", "review", "2", "--pick", "back")
	wantRefusal(t, 3, "card 3 was removed", "review", "3", "good")
	wantRefusal(t, 3, "unknown card 9", "review", "9", "good")
	wantRefusal(t, 3, "not a card number", "review", "x", "good")
	wantRefusal(t, 3, "not both and not neither", "review", "2")
	wantRefusal(t, 3, "not both and not neither", "review", "2", "good", "--pick", "back")
	wantRefusal(t, 3, "must be one of", "review", "2", "fine")
	t.Setenv("TM_CARD_RETENTION", "0.5")
	wantRefusal(t, 3, "TM_CARD_RETENTION must be between 0.70 and 0.97", "review", "2", "good")
	t.Setenv("TM_CARD_RETENTION", "")
	t.Setenv("TM_ROLE", "grader")
	wantRefusal(t, 1, "", "review", "2", "good")
	wantRefusal(t, 1, "", "due")

	if after := readFile(t, file+".cards.jsonl"); after != before {
		t.Errorf("a refused review wrote to the cards file:\n%s", after)
	}
}
