package cards_test

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/reithan/teach-me/internal/cards"
)

var (
	west = time.FixedZone("west", -8*3600)
	// noon is the moment the queue tests run at: midday on 11 November in a
	// zone eight hours behind UTC.
	noon = time.Date(2026, 11, 11, 12, 0, 0, 0, west)
)

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func cardLine(n int, concept string) string {
	c := "null"
	if concept != "" {
		c = `"` + concept + `"`
	}
	return fmt.Sprintf(`{"t":"2026-11-01T00:00:00Z","ev":"card","n":%d,"concept":%s,"origin":"user","front":"f%d","back":"b%d","src":null}`,
		n, c, n, n)
}

func reviewLine(n int, at time.Time, rating, step int, due time.Time) string {
	return fmt.Sprintf(`{"t":"%s","ev":"review","n":%d,"rating":%d,"via":"rate","s":5,"d":5,"step":%d,"due":"%s"}`,
		stamp(at), n, rating, step, stamp(due))
}

func deckOf(t *testing.T, lines ...string) *cards.Deck {
	t.Helper()
	d, err := cards.Replay([]byte(strings.Join(lines, "\n")+"\n"), "x.cards.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func numbers(cs []*cards.Card) []int {
	out := make([]int, len(cs))
	for i, c := range cs {
		out[i] = c.N
	}
	return out
}

func passedOnly(id string) bool { return id == "done" }

func TestDue_SortsCardsByWhatCanBeReviewedNow(t *testing.T) {
	yesterday := noon.Add(-24 * time.Hour)
	d := deckOf(t,
		cardLine(1, ""), cardLine(2, "done"), cardLine(3, ""),
		cardLine(4, ""), cardLine(5, ""), cardLine(6, ""), cardLine(7, ""), cardLine(8, ""),
		cardLine(9, "todo"), cardLine(10, ""), cardLine(11, ""), cardLine(12, ""),
		// 4: in review, due at 23:00 tonight local time: due all day.
		reviewLine(4, yesterday, 3, 0, time.Date(2026, 11, 11, 23, 0, 0, 0, west)),
		// 5: in review, due half an hour after local midnight: not due.
		reviewLine(5, yesterday, 3, 0, time.Date(2026, 11, 12, 0, 30, 0, 0, west)),
		// 6: on a step whose time has passed.
		reviewLine(6, yesterday, 3, 1, noon.Add(-time.Minute)),
		// 7: on a step due in 15 minutes. 8: in 30 minutes.
		reviewLine(7, yesterday, 3, 1, noon.Add(15*time.Minute)),
		reviewLine(8, yesterday, 3, 1, noon.Add(30*time.Minute)),
		// 10 is held, 11 removed.
		`{"t":"2026-11-02T00:00:00Z","ev":"hold","n":10,"on":true}`,
		`{"t":"2026-11-02T00:00:00Z","ev":"rm","n":11}`,
		// 12: in review, due last week.
		reviewLine(12, yesterday, 3, 0, noon.Add(-7*24*time.Hour)),
	)
	q := d.Due(passedOnly, noon, 2)
	for name, got := range map[string][2][]int{
		"review": {numbers(q.Review), {4, 12}},
		"new":    {numbers(q.New), {1, 2}},
		"steps":  {numbers(q.Steps), {6}},
		"soon":   {numbers(q.Soon), {7}},
	} {
		if !slices.Equal(got[0], got[1]) {
			t.Errorf("%s: got %v, want %v", name, got[0], got[1])
		}
	}
	if q.Count() != 5 {
		t.Errorf("count %d, want 5", q.Count())
	}
}

func TestDue_CardsFirstReviewedTodayUseUpTheNewLimit(t *testing.T) {
	lateLastNight := time.Date(2026, 11, 10, 23, 50, 0, 0, west) // already the 11th in UTC
	thisMorning := time.Date(2026, 11, 11, 8, 0, 0, 0, west)
	far := noon.Add(48 * time.Hour)
	d := deckOf(t,
		cardLine(1, ""), cardLine(2, ""), cardLine(3, ""), cardLine(4, ""), cardLine(5, ""),
		reviewLine(1, lateLastNight, 4, 0, far),
		reviewLine(2, thisMorning, 4, 0, far),
		reviewLine(2, thisMorning.Add(time.Hour), 4, 0, far),
	)
	if got := numbers(d.Due(passedOnly, noon, 2).New); !slices.Equal(got, []int{3}) {
		t.Errorf("limit 2 with one card introduced today: new = %v, want [3]", got)
	}
	if got := numbers(d.Due(passedOnly, noon, 1).New); len(got) != 0 {
		t.Errorf("limit 1 already used: new = %v, want none", got)
	}
	if got := numbers(d.Due(passedOnly, noon, 0).New); len(got) != 0 {
		t.Errorf("limit 0: new = %v, want none", got)
	}
}

func TestSession_OrderIsRandomAndCompleteEveryTime(t *testing.T) {
	lines := make([]string, 0, 12)
	for n := 1; n <= 10; n++ {
		lines = append(lines, cardLine(n, ""))
	}
	lines = append(lines,
		reviewLine(9, noon.Add(-time.Hour), 3, 1, noon.Add(-time.Minute)),
		reviewLine(10, noon.Add(-time.Hour), 3, 1, noon.Add(-time.Minute)))
	q := deckOf(t, lines...).Due(passedOnly, noon, 20)

	rng := rand.New(rand.NewPCG(1, 2))
	orders := map[string]bool{}
	for range 40 {
		got := numbers(q.Session(rng))
		orders[fmt.Sprint(got)] = true
		for at, n := range got {
			if n >= 9 && at > 4 {
				t.Fatalf("step card %d at position %d, want it among the next few: %v", n, at, got)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
			t.Fatalf("session lost or repeated a card: %v", got)
		}
	}
	if len(orders) < 30 {
		t.Errorf("40 sessions gave only %d different orders", len(orders))
	}
}

func TestSession_ShowsAStepCardEarlyOnlyWhenNothingElseIsLeft(t *testing.T) {
	soon := reviewLine(1, noon.Add(-time.Minute), 3, 1, noon.Add(9*time.Minute))
	rng := rand.New(rand.NewPCG(3, 4))

	alone := deckOf(t, cardLine(1, ""), soon).Due(passedOnly, noon, 20)
	if got := numbers(alone.Session(rng)); !slices.Equal(got, []int{1}) || alone.Count() != 0 {
		t.Errorf("alone: session %v count %d, want [1] and 0", got, alone.Count())
	}
	withNew := deckOf(t, cardLine(1, ""), cardLine(2, ""), soon).Due(passedOnly, noon, 20)
	if got := numbers(withNew.Session(rng)); !slices.Equal(got, []int{2}) {
		t.Errorf("with a new card: session %v, want [2]", got)
	}
}

func TestOptions(t *testing.T) {
	d := deckOf(t,
		cardLine(1, "done"), cardLine(2, "done"), cardLine(3, "done"),
		cardLine(4, ""), cardLine(5, ""), cardLine(6, ""),
		`{"t":"2026-11-02T00:00:00Z","ev":"edit","n":3,"front":"f3","back":"b2"}`, // same back as card 2
		`{"t":"2026-11-02T00:00:00Z","ev":"rm","n":6}`,
	)
	rng := rand.New(rand.NewPCG(5, 6))
	seenFirst := map[string]bool{}
	for range 30 {
		opts := d.Options(d.Card(1), 3, rng)
		seenFirst[opts[0]] = true
		slices.Sort(opts)
		// b1 is the answer, b2 the only other back of the same concept, and
		// one of b4 or b5 fills the third place. b6 is removed.
		if len(opts) != 3 || opts[0] != "b1" || opts[1] != "b2" || (opts[2] != "b4" && opts[2] != "b5") {
			t.Fatalf("options %v", opts)
		}
	}
	if len(seenFirst) < 2 {
		t.Errorf("the options always start with the same choice: %v", seenFirst)
	}
	if got := d.Options(d.Card(1), 4, rng); len(got) != 4 {
		t.Errorf("want 4 options when 3 others exist, got %v", got)
	}

	lone := deckOf(t, cardLine(1, ""), cardLine(2, ""), `{"t":"2026-11-02T00:00:00Z","ev":"edit","n":2,"front":"x","back":"b1"}`)
	if got := lone.Options(lone.Card(1), 4, rng); got != nil {
		t.Errorf("a card with no different back to choose from should get no options, got %v", got)
	}
}

func TestReviewRow(t *testing.T) {
	d := deckOf(t, cardLine(1, ""))
	row := d.Card(1).ReviewRow(cards.Good, cards.ViaRate, noon, 0.9)
	if row.Ev != cards.EvReview || row.N != 1 || row.Rating != 3 || row.Via != "rate" ||
		row.S != 2.3065 || row.D != 2.1181 || row.Step != 1 || !row.Due.Equal(noon.Add(10*time.Minute)) {
		t.Errorf("first good: %+v", row)
	}

	// A card that lapsed is relearning: hard is 15 minutes, not 6.
	lapsed := deckOf(t, cardLine(1, ""),
		reviewLine(1, noon.Add(-72*time.Hour), 4, 0, noon.Add(-24*time.Hour)),
		reviewLine(1, noon.Add(-time.Hour), 1, 1, noon.Add(-50*time.Minute)))
	row = lapsed.Card(1).ReviewRow(cards.Hard, cards.ViaTerm, noon, 0.9)
	if row.Step != 1 || !row.Due.Equal(noon.Add(15*time.Minute)) {
		t.Errorf("relearning hard: %+v", row)
	}
}

func TestNewRand_IsSeededAfresh(t *testing.T) {
	a, b := cards.NewRand(), cards.NewRand()
	if a.Uint64() == b.Uint64() {
		t.Errorf("two generators gave the same first number")
	}
}

func TestLoadSettings(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	for _, name := range []string{"TM_CARD_RETENTION", "TM_CARD_NEW", "TM_CARD_LEECH"} {
		t.Setenv(name, "")
	}

	got, err := cards.LoadSettings()
	if err != nil || got != (cards.Settings{Retention: 0.9, New: 20, Leech: 4}) {
		t.Fatalf("defaults: %+v, %v", got, err)
	}

	config := "card-retention = 0.85\ncard-new = 5\ncard-leech = 6\n"
	if err := os.WriteFile(filepath.Join(dir, ".tmconfig"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = cards.LoadSettings()
	if err != nil || got != (cards.Settings{Retention: 0.85, New: 5, Leech: 6}) {
		t.Fatalf("config keys: %+v, %v", got, err)
	}

	t.Setenv("TM_CARD_RETENTION", "0.95")
	t.Setenv("TM_CARD_NEW", "0")
	t.Setenv("TM_CARD_LEECH", "1")
	got, err = cards.LoadSettings()
	if err != nil || got != (cards.Settings{Retention: 0.95, New: 0, Leech: 1}) {
		t.Fatalf("environment over config: %+v, %v", got, err)
	}

	for name, bad := range map[string]string{
		"TM_CARD_RETENTION": "0.99",
		"TM_CARD_NEW":       "-1",
		"TM_CARD_LEECH":     "0",
	} {
		t.Setenv(name, bad)
		if _, err := cards.LoadSettings(); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s=%s: want an error naming the variable, got %v", name, bad, err)
		}
		t.Setenv(name, "")
	}

	if err := os.WriteFile(filepath.Join(dir, ".tmconfig"), []byte("card-retention = high\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cards.LoadSettings(); err == nil || !strings.Contains(err.Error(), "card-retention") {
		t.Errorf("bad config value: want an error naming the key, got %v", err)
	}
}
