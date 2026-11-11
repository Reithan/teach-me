package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDrop_RemovesTheConceptsCards(t *testing.T) {
	dir, file := cardSetup(t)
	list := filepath.Join(dir, "list.tsv")
	if err := os.WriteFile(list, []byte("uno\tone\ndos\ttwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "card", "import", list, "--concept", "todo", "--origin", "user")
	mustRun(t, "card", "add", "done", "src.txt:1-2", "kept front", "kept back")
	mustRun(t, "card", "rm", "2")

	mustRun(t, "drop", "todo")

	cardsFile := readFile(t, file+".cards.jsonl")
	if !strings.HasSuffix(strings.TrimSpace(cardsFile), `"ev":"rm","n":1}`) {
		t.Errorf("want the cards file to end with an rm row for card 1:\n%s", cardsFile)
	}
	if n := strings.Count(cardsFile, `"ev":"rm"`); n != 2 {
		t.Errorf("want 2 rm rows (card 2 by hand, card 1 by drop), got %d:\n%s", n, cardsFile)
	}
	if log := readFile(t, file+".jsonl"); !strings.Contains(log, `"ev":"drop"`) || !strings.Contains(log, `"cards":[1],`) {
		t.Errorf("drop event lacks the removed card numbers:\n%s", log)
	}
	wantRefusal(t, 3, "card 1", "card", "show", "1")
	if out := mustRun(t, "card", "show", "3"); !strings.Contains(out, "kept front") {
		t.Errorf("another concept's card should survive the drop:\n%s", out)
	}
}

func TestDrop_ConceptWithoutCardsLogsNoCardsField(t *testing.T) {
	_, file := cardSetup(t)
	mustRun(t, "drop", "todo")
	if log := readFile(t, file+".jsonl"); strings.Contains(log, `"cards"`) {
		t.Errorf("drop event should have no cards field:\n%s", log)
	}
	if _, err := os.Stat(file + ".cards.jsonl"); !os.IsNotExist(err) {
		t.Errorf("drop should not create a cards file; stat err = %v", err)
	}
}

func TestDrop_ABrokenCardsFileKeepsTheConcept(t *testing.T) {
	_, file := cardSetup(t)
	if err := os.WriteFile(file+".cards.jsonl", []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantRefusal(t, 2, "line 1 is not a card row", "drop", "todo")
	if !strings.Contains(blockOf(t, file, "untested"), "todo[") {
		t.Errorf("the concept should still be in the graph")
	}
}

func TestCardsFileIsNotASource(t *testing.T) {
	dir, file := cardSetup(t)
	mustRun(t, "card", "add", "done", "src.txt:1-2", "front", "back")
	name := filepath.Base(file) + ".cards.jsonl"
	want := name + " is a cards file, not a source"

	for _, args := range [][]string{
		{"add", "fresh", name + ":1-1", "scope"},
		{"q", "todo", name + ":1-1", "scope"},
		{"recite", "done", name + ":1-1"},
		{"src", name},
		{"card", "add", "done", name + ":1-1", "front 2", "back 2"},
		{"card", "import", name},
	} {
		wantRefusal(t, 1, want, args...)
	}

	// A file elsewhere with the cards suffix is refused too.
	other := filepath.Join(dir, "other.mmd.cards.jsonl")
	if err := os.WriteFile(other, []byte("a\tb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantRefusal(t, 1, "other.mmd.cards.jsonl is a cards file, not a source", "src", "other.mmd.cards.jsonl")
}
