package cards

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

func cardRow(n int, concept, front, back string) Row {
	return Row{Ev: EvCard, N: n, Concept: concept, Origin: OriginSource, Front: front, Back: back, Src: "abc@src.txt:1-2"}
}

func reviewRow(n, rating, step int) Row {
	return Row{Ev: EvReview, N: n, Rating: rating, Via: ViaTerm, S: 2.3065, D: 2.1181, Step: step, Due: t0.Add(10 * time.Minute)}
}

// write appends rows to a fresh cards file and returns its path.
func write(t *testing.T, rows ...Row) string {
	t.Helper()
	path := Path(filepath.Join(t.TempDir(), "g.tm.mmd"))
	if err := Append(path, t0, rows...); err != nil {
		t.Fatalf("Append: %v", err)
	}
	return path
}

func load(t *testing.T, path string) *Deck {
	t.Helper()
	d, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return d
}

func TestPathAndIsCardsFile(t *testing.T) {
	if got := Path("dir/g.tm.mmd"); got != "dir/g.tm.mmd.cards.jsonl" {
		t.Errorf("Path = %q", got)
	}
	if !IsCardsFile("dir/g.tm.mmd.cards.jsonl") || IsCardsFile("dir/g.tm.mmd.jsonl") {
		t.Error("IsCardsFile misjudged a path")
	}
}

func TestLoad_MissingFileIsAnEmptyDeck(t *testing.T) {
	d := load(t, filepath.Join(t.TempDir(), "none.cards.jsonl"))
	if len(d.Cards) != 0 || d.NextN() != 1 {
		t.Errorf("want an empty deck starting at 1, got %d cards, next %d", len(d.Cards), d.NextN())
	}
}

func TestAppend_WritesTheSpecFieldsInOrder(t *testing.T) {
	path := write(t,
		cardRow(1, "raft", "leader <b>?", "one & only"),
		Row{Ev: EvCard, N: 2, Origin: OriginUser, Front: "f", Back: "b"},
		reviewRow(1, Good, 1),
		Row{Ev: EvEdit, N: 1, Front: "f2", Back: "b2"},
		Row{Ev: EvHold, N: 1, On: true},
		Row{Ev: EvRemove, N: 2},
	)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"t":"2026-11-11T09:00:00Z","ev":"card","n":1,"concept":"raft","origin":"source","front":"leader <b>?","back":"one & only","src":"abc@src.txt:1-2"}
{"t":"2026-11-11T09:00:00Z","ev":"card","n":2,"concept":null,"origin":"user","front":"f","back":"b","src":null}
{"t":"2026-11-11T09:00:00Z","ev":"review","n":1,"rating":3,"via":"term","s":2.3065,"d":2.1181,"step":1,"due":"2026-11-11T09:10:00Z"}
{"t":"2026-11-11T09:00:00Z","ev":"edit","n":1,"front":"f2","back":"b2"}
{"t":"2026-11-11T09:00:00Z","ev":"hold","n":1,"on":true}
{"t":"2026-11-11T09:00:00Z","ev":"rm","n":2}
`
	if string(data) != want {
		t.Errorf("file:\n%s\nwant:\n%s", data, want)
	}
}

func TestAppend_KeepsARowsOwnTimeAndRefusesAnUnknownKind(t *testing.T) {
	own := t0.Add(-time.Hour)
	r := cardRow(1, "", "f", "b")
	r.T = own
	path := write(t, r)
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"t":"2026-11-11T08:00:00Z"`) {
		t.Errorf("row time was restamped: %s", data)
	}
	if err := Append(path, t0, Row{Ev: "nope", N: 1}); err == nil {
		t.Error("want an error for an unknown row kind")
	}
	if err := Append(path, t0); err != nil {
		t.Errorf("appending nothing: %v", err)
	}
	if err := Append(filepath.Join(t.TempDir(), "no", "dir.cards.jsonl"), t0, r); err == nil {
		t.Error("want an error when the directory does not exist")
	}
}

func TestReplay_DerivesTextHoldRemovalAndLapses(t *testing.T) {
	path := write(t,
		cardRow(1, "raft", "f1", "b1"),
		cardRow(2, "raft", "f2", "b2"),
		cardRow(3, "", "f3", "b3"),
		reviewRow(1, Good, 1),  // learning
		reviewRow(1, Again, 2), // again on a step: not a lapse
		reviewRow(1, Good, 0),  // now in review
		reviewRow(1, Again, 1), // a lapse
		reviewRow(1, Good, 0),
		reviewRow(1, Again, 1), // a second lapse
		Row{Ev: EvEdit, N: 1, Front: "f1 reworded", Back: "b1 reworded"},
		Row{Ev: EvHold, N: 2, On: true},
		Row{Ev: EvHold, N: 2, On: false},
		Row{Ev: EvHold, N: 2, On: true},
		Row{Ev: EvRemove, N: 3},
		reviewRow(3, Good, 1), // lands after the rm row: ignored
	)
	d := load(t, path)

	c1 := d.Card(1)
	if c1.Front != "f1 reworded" || c1.Back != "b1 reworded" {
		t.Errorf("edit not applied: %q / %q", c1.Front, c1.Back)
	}
	if c1.Reviews != 6 || c1.Lapses != 2 || c1.Last == nil || c1.Last.Step != 1 {
		t.Errorf("card 1: reviews %d lapses %d last %+v", c1.Reviews, c1.Lapses, c1.Last)
	}
	if !d.Card(2).Held {
		t.Error("card 2: the latest hold row should win")
	}
	if c3 := d.Card(3); !c3.Removed || c3.Reviews != 0 {
		t.Errorf("card 3: removed %v reviews %d", c3.Removed, c3.Reviews)
	}
	if d.Card(4) != nil {
		t.Error("card 4 was never added")
	}
	if d.NextN() != 4 {
		t.Errorf("NextN = %d; a removed card keeps its number", d.NextN())
	}
	if got := d.OfConcept("raft"); len(got) != 2 || got[0].N != 1 || got[1].N != 2 {
		t.Errorf("OfConcept(raft) = %v", got)
	}
	if !d.Has("raft", "f2", "b2") || d.Has("", "f3", "b3") || d.Has("other", "f2", "b2") {
		t.Error("Has should match concept, front, and back of cards that are not removed")
	}
}

func TestCardState(t *testing.T) {
	passed := func(c string) bool { return c == "done" }
	learning, review := reviewRow(1, Good, 1), reviewRow(1, Good, 0)
	cases := []struct {
		name string
		card Card
		want State
	}{
		{"removed wins over everything", Card{Removed: true, Held: true, Concept: "open"}, StateRemoved},
		{"concept not passed", Card{Concept: "open", Held: true}, StateWaiting},
		{"held", Card{Concept: "done", Held: true, Last: &review}, StateHeld},
		{"never reviewed", Card{Concept: "done"}, StateNew},
		{"no concept is never waiting", Card{}, StateNew},
		{"on a step", Card{Last: &learning}, StateLearning},
		{"past the steps", Card{Last: &review}, StateReview},
	}
	for _, tc := range cases {
		if got := tc.card.State(passed); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestReplay_RefusesALineThatIsNotACardRow(t *testing.T) {
	good := `{"t":"2026-11-11T09:00:00Z","ev":"card","n":1,"concept":null,"origin":"user","front":"f","back":"b","src":null}`
	review := `{"t":"2026-11-11T09:00:00Z","ev":"review","n":1,"rating":3,"via":"term","s":2.3,"d":2.1,"step":1,"due":"2026-11-11T09:10:00Z"}`
	bad := map[string]string{
		"not JSON":              `hello`,
		"blank line":            ``,
		"no time":               `{"ev":"rm","n":1}`,
		"bad time":              `{"t":"yesterday","ev":"rm","n":1}`,
		"no number":             `{"t":"2026-11-11T09:00:00Z","ev":"rm"}`,
		"number below 1":        `{"t":"2026-11-11T09:00:00Z","ev":"rm","n":0}`,
		"number as text":        `{"t":"2026-11-11T09:00:00Z","ev":"rm","n":"1"}`,
		"unknown kind":          `{"t":"2026-11-11T09:00:00Z","ev":"note","n":1}`,
		"unknown card":          `{"t":"2026-11-11T09:00:00Z","ev":"rm","n":9}`,
		"number used twice":     good,
		"card: bad origin":      strings.Replace(strings.Replace(good, `"n":1`, `"n":2`, 1), `"user"`, `"web"`, 1),
		"card: empty front":     strings.Replace(strings.Replace(good, `"n":1`, `"n":2`, 1), `"front":"f"`, `"front":" "`, 1),
		"card: no back":         `{"t":"2026-11-11T09:00:00Z","ev":"card","n":2,"origin":"user","front":"f"}`,
		"card: front too long":  strings.Replace(strings.Replace(good, `"n":1`, `"n":2`, 1), `"front":"f"`, `"front":"`+strings.Repeat("x", MaxFieldChars+1)+`"`, 1),
		"review: rating 5":      strings.Replace(review, `"rating":3`, `"rating":5`, 1),
		"review: bad via":       strings.Replace(review, `"term"`, `"web"`, 1),
		"review: zero s":        strings.Replace(review, `"s":2.3`, `"s":0`, 1),
		"review: d above 10":    strings.Replace(review, `"d":2.1`, `"d":10.5`, 1),
		"review: negative step": strings.Replace(review, `"step":1`, `"step":-1`, 1),
		"review: no due":        strings.Replace(review, `,"due":"2026-11-11T09:10:00Z"`, ``, 1),
		"review: bad due":       strings.Replace(review, `"due":"2026-11-11T09:10:00Z"`, `"due":"soon"`, 1),
		"edit: no text":         `{"t":"2026-11-11T09:00:00Z","ev":"edit","n":1}`,
		"hold: no on":           `{"t":"2026-11-11T09:00:00Z","ev":"hold","n":1}`,
	}
	for name, line := range bad {
		_, err := Replay([]byte(good+"\n"+line+"\n"), "g.cards.jsonl")
		var re *RowError
		if !errors.As(err, &re) || re.Line != 2 {
			t.Errorf("%s: want a RowError on line 2, got %v", name, err)
			continue
		}
		if re.Error() != "g.cards.jsonl line 2 is not a card row" {
			t.Errorf("%s: message %q", name, re.Error())
		}
	}
	// The same valid rows replay cleanly, and a field at the length limit is fine.
	limit := strings.Replace(strings.Replace(good, `"n":1`, `"n":2`, 1), `"front":"f"`, `"front":"`+strings.Repeat("é", MaxFieldChars)+`"`, 1)
	if _, err := Replay([]byte(good+"\n"+review+"\n"+limit+"\n"), "g"); err != nil {
		t.Errorf("valid rows refused: %v", err)
	}
}

func TestLoad_NamesTheFileInARowError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.tm.mmd.cards.jsonl")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || err.Error() != "g.tm.mmd.cards.jsonl line 1 is not a card row" {
		t.Errorf("got %v", err)
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("want an error when the path is a directory")
	}
}

func TestCheckField(t *testing.T) {
	for in, want := range map[string]string{
		"ok":                                 "",
		"":                                   "is empty",
		strings.Repeat(" ", 3):               "is empty",
		strings.Repeat("x", MaxFieldChars):   "",
		strings.Repeat("x", MaxFieldChars+1): "is over 1,000 characters",
		strings.Repeat("日", MaxFieldChars):   "",
		strings.Repeat("日", MaxFieldChars+1): "is over 1,000 characters",
	} {
		if got := CheckField(in); got != want {
			t.Errorf("CheckField(%.10q...) = %q, want %q", in, got, want)
		}
	}
}

func TestParseImport_ReadsTheListFormat(t *testing.T) {
	text := "\ufeff# vocabulary, chapter 3\r\n" +
		"der Hund\tthe dog\r\n" +
		"\n" +
		"die Katze\tthe cat\tignored note\n" +
		"  spaced  \t kept as written \n"
	pairs, err := ParseImport(text)
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	want := []Pair{
		{2, "der Hund", "the dog"},
		{4, "die Katze", "the cat"},
		{5, "  spaced  ", " kept as written "},
	}
	if len(pairs) != len(want) {
		t.Fatalf("got %d pairs, want %d: %v", len(pairs), len(want), pairs)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Errorf("pair %d: got %+v, want %+v", i, pairs[i], want[i])
		}
	}
	if pairs, err := ParseImport(""); err != nil || len(pairs) != 0 {
		t.Errorf("empty list: %v, %v", pairs, err)
	}
}

func TestParseImport_RefusesAMalformedLineWithItsNumber(t *testing.T) {
	long := strings.Repeat("x", MaxFieldChars+1)
	for text, want := range map[string]string{
		"a\tb\nno tab here\n":    "line 2: no tab between front and back",
		"\tb\n":                  "line 1: front is empty",
		"a\t\n":                  "line 1: back is empty",
		"a\t \tc\n":              "line 1: back is empty",
		"# c\n\n" + long + "\tb": "line 3: front is over 1,000 characters",
		"a\t" + long:             "line 1: back is over 1,000 characters",
		"a\tb\n\xff\tb\n":        "line 2: not UTF-8",
	} {
		pairs, err := ParseImport(text)
		var ie *ImportError
		if !errors.As(err, &ie) || err.Error() != want {
			t.Errorf("%.20q: got %v, want %q", text, err, want)
		}
		if pairs != nil {
			t.Errorf("%.20q: a refused import must return no pairs", text)
		}
	}
}
