package review_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/reithan/teach-me/internal/cards"
	"github.com/reithan/teach-me/internal/review"
)

const graphText = `---
config:
  look: classic
  darkMode: true
  theme: dark
  layout: elk
  elk:
    mergeEdges: true
    nodePlacementStrategy: NETWORK_SIMPLEX
---
flowchart TB
    subgraph passed["Concepts User understands"]
        done["Done concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 3
        todo["Todo concept<br/>f5ca3875b379@src.txt:1-5"]
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

// clock is a settable time source.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// setup writes a graph and count loose cards plus one waiting card (the
// last), and returns a store on a fixed clock with the path of the cards
// file.
func setup(t *testing.T, count int) (*review.Store, *clock, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "g.tm.mmd")
	if err := os.WriteFile(file, []byte(graphText), 0o644); err != nil {
		t.Fatal(err)
	}
	ck := &clock{time.Date(2026, 11, 11, 12, 0, 0, 0, time.FixedZone("west", -8*3600))}
	rows := make([]cards.Row, 0, count+1)
	for n := 1; n <= count; n++ {
		rows = append(rows, cards.Row{
			Ev: cards.EvCard, N: n, Origin: cards.OriginUser,
			Front: fmt.Sprintf("front %d", n), Back: fmt.Sprintf("back %d", n),
		})
	}
	rows = append(rows, cards.Row{
		Ev: cards.EvCard, N: count + 1, Concept: "todo", Origin: cards.OriginSource,
		Front: "waiting front", Back: "waiting back", Src: "abc@src.txt:1-2",
	})
	if err := cards.Append(cards.Path(file), ck.t.Add(-time.Hour), rows...); err != nil {
		t.Fatal(err)
	}
	st := &review.Store{
		GraphFile: file,
		Set:       cards.Settings{Retention: 0.9, New: 20, Leech: 4},
		Now:       ck.now,
		Rng:       rand.New(rand.NewPCG(7, 8)),
	}
	return st, ck, cards.Path(file)
}

func reviewRows(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Contains(line, `"ev":"review"`) {
			out = append(out, line)
		}
	}
	return out
}

func TestRate(t *testing.T) {
	st, _, path := setup(t, 2)
	row, err := st.Rate(1, cards.Good, cards.ViaTerm)
	if err != nil || row.Step != 1 || row.S != 2.3065 {
		t.Fatalf("rate: %+v, %v", row, err)
	}
	if rows := reviewRows(t, path); len(rows) != 1 || !strings.Contains(rows[0], `"n":1,"rating":3,"via":"term"`) {
		t.Errorf("review rows: %v", rows)
	}

	if err := cards.Append(path, time.Time{}, cards.Row{Ev: cards.EvHold, N: 2, On: true}); err != nil {
		t.Fatal(err)
	}
	for n, want := range map[int]string{
		2: "card 2 is held",
		3: "card 3 is waiting",
		9: "unknown card 9",
	} {
		_, err := st.Rate(n, cards.Good, cards.ViaTerm)
		var nr *review.NotReviewableError
		if !errors.As(err, &nr) || err.Error() != want {
			t.Errorf("card %d: got %v, want %q", n, err, want)
		}
	}
	if err := cards.Append(path, time.Time{}, cards.Row{Ev: cards.EvRemove, N: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Rate(1, cards.Good, cards.ViaTerm); err == nil || err.Error() != "card 1 was removed" {
		t.Errorf("removed card: %v", err)
	}
	if rows := reviewRows(t, path); len(rows) != 1 {
		t.Errorf("a refused review wrote a row: %v", rows)
	}
}

func TestSession_OrderDiffersBetweenSittings(t *testing.T) {
	st, _, _ := setup(t, 8)
	orders := map[string]bool{}
	for range 20 {
		s, err := st.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		var order []int
		for {
			c, left, err := s.Next()
			if err != nil {
				t.Fatal(err)
			}
			if c == nil {
				break
			}
			if left != 8-len(order) {
				t.Fatalf("left = %d after %d cards", left, len(order))
			}
			order = append(order, c.N)
			// Easy sends the card straight to review, so it leaves the queue.
			if _, err := st.Rate(c.N, cards.Easy, cards.ViaTerm); err != nil {
				t.Fatal(err)
			}
		}
		orders[fmt.Sprint(order)] = true
		sorted := slices.Clone(order)
		slices.Sort(sorted)
		if !slices.Equal(sorted, []int{1, 2, 3, 4, 5, 6, 7, 8}) {
			t.Fatalf("sitting showed %v", order)
		}
		// Start over: a fresh cards file for the next sitting.
		st2, _, _ := setup(t, 8)
		st2.Rng = st.Rng
		st = st2
	}
	if len(orders) < 15 {
		t.Errorf("20 sittings gave only %d different orders", len(orders))
	}
}

func TestSession_StepCardsComeBackAndEndTheSittingInReview(t *testing.T) {
	st, ck, path := setup(t, 3)
	s, err := st.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	// Rate every new card good: each goes to the 10-minute step.
	for range 3 {
		c, _, err := s.Next()
		if err != nil || c == nil {
			t.Fatalf("next: %v, %v", c, err)
		}
		if _, err := st.Rate(c.N, cards.Good, cards.ViaTerm); err != nil {
			t.Fatal(err)
		}
		ck.t = ck.t.Add(10 * time.Second)
	}
	// Nothing is due, but all three are within 20 minutes, so they are
	// shown early; a second good moves each into review.
	for i := range 3 {
		c, left, err := s.Next()
		if err != nil || c == nil {
			t.Fatalf("early card %d: %v, %v", i, c, err)
		}
		if left != 3-i {
			t.Errorf("left = %d, want %d", left, 3-i)
		}
		if _, err := st.Rate(c.N, cards.Good, cards.ViaTerm); err != nil {
			t.Fatal(err)
		}
	}
	if c, _, err := s.Next(); c != nil || err != nil {
		t.Errorf("the sitting should be over: %v, %v", c, err)
	}
	if rows := reviewRows(t, path); len(rows) != 6 {
		t.Errorf("want 6 review rows, got %d", len(rows))
	}

	// Again on a card sends it to the 1-minute step; once its time passes it
	// rejoins the queue of a sitting that still has cards.
	st, ck, _ = setup(t, 6)
	s, _ = st.NewSession()
	first, _, _ := s.Next()
	if _, err := st.Rate(first.N, cards.Again, cards.ViaTerm); err != nil {
		t.Fatal(err)
	}
	if c, left, _ := s.Next(); c.N == first.N || left != 5 {
		t.Errorf("a card on a step that is not due yet should be out of the queue: card %d, left %d", c.N, left)
	}
	ck.t = ck.t.Add(2 * time.Minute)
	seen := false
	for at := 0; ; at++ {
		c, _, _ := s.Next()
		if c == nil {
			break
		}
		if c.N == first.N {
			seen = true
			if at > 3 {
				t.Errorf("the step card came back at position %d, want it among the next few", at)
			}
		}
		if _, err := st.Rate(c.N, cards.Easy, cards.ViaTerm); err != nil {
			t.Fatal(err)
		}
	}
	if !seen {
		t.Errorf("the step card never came back")
	}
}

func TestSession_DropsACardHeldOrRemovedElsewhere(t *testing.T) {
	st, _, path := setup(t, 3)
	s, _ := st.NewSession()
	if err := cards.Append(path, time.Time{},
		cards.Row{Ev: cards.EvHold, N: 1, On: true}, cards.Row{Ev: cards.EvRemove, N: 2}); err != nil {
		t.Fatal(err)
	}
	if c, left, err := s.Next(); err != nil || c == nil || c.N != 3 || left != 1 {
		t.Errorf("want only card 3 left, got %v, %d, %v", c, left, err)
	}
}

func TestTerminal(t *testing.T) {
	st, _, path := setup(t, 2)
	// Card A: Enter, a bad key, then 3. Card B: Enter, 4. Then card A again
	// early (it is on the 10-minute step): Enter, q.
	in := strings.NewReader("\nx\n3\n\n4\n\nq\n")
	var out bytes.Buffer
	done, err := st.Terminal(in, &out)
	if err != nil || done != 2 {
		t.Fatalf("done %d, err %v\n%s", done, err, out.String())
	}
	text := out.String()
	for _, want := range []string{"2 left", "1 left", "front 1", "back 1", "front 2", "back 2", "origin user", "reviewed 2"} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "waiting front") {
		t.Errorf("a waiting card was shown:\n%s", text)
	}
	if strings.Index(text, "front") > strings.Index(text, "back") {
		t.Errorf("the back was printed before the front:\n%s", text)
	}
	if n := strings.Count(text, "1 again  2 hard"); n != 4 {
		t.Errorf("want the rating prompt 4 times (one repeat after the bad key), got %d", n)
	}
	rows := reviewRows(t, path)
	if len(rows) != 2 || !strings.Contains(rows[0], `"rating":3,"via":"term"`) || !strings.Contains(rows[1], `"rating":4,"via":"term"`) {
		t.Errorf("review rows: %v", rows)
	}
}

func TestTerminal_EndOfInputAndNothingDue(t *testing.T) {
	st, _, path := setup(t, 1)
	var out bytes.Buffer
	if done, err := st.Terminal(strings.NewReader(""), &out); done != 0 || err != nil {
		t.Errorf("end of input before the back: %d, %v", done, err)
	}
	if done, err := st.Terminal(strings.NewReader("\n"), &out); done != 0 || err != nil {
		t.Errorf("end of input before a rating: %d, %v", done, err)
	}
	if rows := reviewRows(t, path); len(rows) != 0 {
		t.Errorf("no rating was given, but rows were written: %v", rows)
	}

	st.Set.New = 0
	out.Reset()
	if done, err := st.Terminal(strings.NewReader("\n3\n"), &out); done != 0 || err != nil || out.String() != "no cards reviewed\n" {
		t.Errorf("nothing due: %d, %v, %q", done, err, out.String())
	}
}

func TestRun(t *testing.T) {
	st, _, path := setup(t, 1)
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, name := range []string{"TM_FILE", "TM_CARD_RETENTION", "TM_CARD_NEW", "TM_CARD_LEECH"} {
		t.Setenv(name, "")
	}
	run := func(in string, args ...string) (string, string, int) {
		var out, errOut bytes.Buffer
		code := review.Run(t.Context(), args, strings.NewReader(in), &out, &errOut)
		return out.String(), errOut.String(), code
	}

	if out, _, code := run("", "--version"); code != 0 || strings.TrimSpace(out) == "" {
		t.Errorf("--version: %q, exit %d", out, code)
	}
	for _, tc := range []struct {
		want string
		args []string
	}{
		{"tmcards needs a command", nil},
		{`unknown command "sing"`, []string{"sing"}},
		{"flag provided but not defined", []string{"--loud"}},
		{"flag provided but not defined", []string{"review", "--loud"}},
		{`unexpected argument "now"`, []string{"review", "now"}},
	} {
		if _, errOut, code := run("", tc.args...); code != 3 || !strings.Contains(errOut, "err: "+tc.want) || !strings.Contains(errOut, "fix: tmcards serve") || !strings.Contains(errOut, "\ntmcards review [") {
			t.Errorf("%v: exit %d, stderr %q", tc.args, code, errOut)
		}
	}
	if _, errOut, code := run("", "review"); code != 3 || !strings.Contains(errOut, "err: ") {
		t.Errorf("no graph configured: exit %d, stderr %q", code, errOut)
	}

	// --file works before or after the command, and TM_FILE without it.
	if out, errOut, code := run("\n3\n", "--file", st.GraphFile, "review"); code != 0 || !strings.Contains(out, "reviewed 1") {
		t.Errorf("--file first: exit %d, %q, %q", code, out, errOut)
	}
	if out, _, code := run("\nq\n", "review", "--file", st.GraphFile); code != 0 || !strings.Contains(out, "front 1") {
		t.Errorf("--file last: exit %d, %q", code, out)
	}
	t.Setenv("TM_FILE", st.GraphFile)
	if out, _, code := run("q\n", "review"); code != 0 || !strings.Contains(out, "front 1") {
		t.Errorf("TM_FILE: exit %d, %q", code, out)
	}

	t.Setenv("TM_CARD_RETENTION", "2")
	if _, errOut, code := run("", "review"); code != 3 || !strings.Contains(errOut, "TM_CARD_RETENTION") {
		t.Errorf("bad setting: exit %d, %q", code, errOut)
	}
	t.Setenv("TM_CARD_RETENTION", "")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("not a row\n")
	_ = f.Close()
	if _, errOut, code := run("", "review"); code != 2 || !strings.Contains(errOut, "is not a card row") {
		t.Errorf("broken cards file: exit %d, %q", code, errOut)
	}
	if _, errOut, code := run("", "review", "--file", filepath.Join(t.TempDir(), "none.tm.mmd")); code != 3 || !strings.Contains(errOut, "cannot read") {
		t.Errorf("missing graph: exit %d, %q", code, errOut)
	}
	bad := filepath.Join(t.TempDir(), "bad.tm.mmd")
	if err := os.WriteFile(bad, []byte("not a graph"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := run("", "review", "--file", bad); code != 3 || !strings.Contains(errOut, "cannot parse") {
		t.Errorf("bad graph: exit %d, %q", code, errOut)
	}
}

func TestRun_Serve(t *testing.T) {
	st, _, path := setup(t, 1)
	run := func(ctx context.Context, args ...string) (string, string, int) {
		var out, errOut bytes.Buffer
		code := review.Run(ctx, args, strings.NewReader(""), &out, &errOut)
		return out.String(), errOut.String(), code
	}
	for _, tc := range []struct {
		want string
		args []string
	}{
		{"needs --port", []string{"serve", "--bind", "0.0.0.0"}},
		{"not host:port", []string{"serve", "--public", "nowhere"}},
		{"--idle must be more than zero", []string{"serve", "--idle", "0s"}},
		{"invalid value", []string{"serve", "--idle", "soon"}},
		{"invalid value", []string{"serve", "--port", "http"}},
		{"flag provided but not defined", []string{"review", "--port", "1"}},
	} {
		args := append([]string{"--file", st.GraphFile}, tc.args...)
		if _, errOut, code := run(t.Context(), args...); code != 3 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "fix: tmcards serve") {
			t.Errorf("%v: exit %d, stderr %q", tc.args, code, errOut)
		}
	}

	out, errOut, code := run(t.Context(), "serve", "--file", st.GraphFile, "--idle", "50ms", "--public", "localhost:8765")
	if code != 0 || !strings.HasPrefix(out, "http://localhost:8765/") || strings.Count(out, "\n") != 1 {
		t.Errorf("serve: exit %d, %q, %q", code, out, errOut)
	}
	if _, errOut, code := run(t.Context(), "serve", "--file", st.GraphFile, "--bind", "192.0.2.1", "--port", "8765"); code != 3 || !strings.Contains(errOut, "cannot listen") {
		t.Errorf("cannot listen: exit %d, %q", code, errOut)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("not a row\n")
	_ = f.Close()
	if out, errOut, code := run(t.Context(), "serve", "--file", st.GraphFile); code != 2 || out != "" || !strings.Contains(errOut, "is not a card row") {
		t.Errorf("broken cards file: exit %d, %q, %q", code, out, errOut)
	}
}
