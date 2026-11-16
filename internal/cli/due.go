package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/reithan/teach-me/internal/cards"
	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/fsrs"
)

// dueOptions is the most answer choices tm due prints for one card (§17.5).
const dueOptions = 4

// dueCard is one card in `tm due --format json`.
type dueCard struct {
	N       int      `json:"n"`
	Front   string   `json:"front"`
	Options []string `json:"options"`
}

// cardNow is the time card commands schedule by: the wall clock in the local
// time zone, which sets the day boundary (§17.4).
func cardNow() time.Time { return errlog.RealClock.Now().Local() }

// cardSettings loads the flashcard settings; a bad value is a usage error.
func cardSettings(ctx *Context) (cards.Settings, int) {
	set, err := cards.LoadSettings()
	if err != nil {
		return set, cardErr(ctx, 3, err.Error())
	}
	return set, 0
}

// loadCards loads the graph and the cards file for a read-only command and
// returns the deck with a test for passed concepts.
func loadCards(ctx *Context) (deck *cards.Deck, passed func(string) bool, file string, code int) {
	s, file, code := loadStateCtx(ctx)
	if code != 0 {
		return nil, nil, "", code
	}
	deck, code = loadDeck(ctx, file)
	if code != 0 {
		return nil, nil, "", code
	}
	set := graphNodeSets(s.Graph(), false).PassedSet
	return deck, func(id string) bool { return set[id] }, file, 0
}

// dueRun is the Run handler for `tm due [--format lines|json] [--limit N]`.
//
// Read-only. Prints up to --limit cards that are due now, in a random order
// that is new on every call, each with its number, front, and answer options
// (§17.5). A card with no other card to draw options from has none.
//
// Exit codes:
//
//	0  ok (nothing printed in lines format when no card is due)
//	2  the cards file has a line that is not a card row
//	3  usage error
func dueRun(ctx *Context) int {
	limit := dueOptions
	if v := ctx.Flags["limit"]; len(v) > 0 {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 1 {
			return cardUsageErr(ctx, "due", fmt.Sprintf("--limit %q is not a positive number", v[0]))
		}
		limit = n
	}
	set, code := cardSettings(ctx)
	if code != 0 {
		return code
	}
	deck, passed, _, code := loadCards(ctx)
	if code != 0 {
		return code
	}

	rng := cards.NewRand()
	queue := deck.Due(passed, cardNow(), set.New).Session(rng)
	queue = queue[:min(len(queue), limit)]

	out := make([]dueCard, len(queue))
	for i, c := range queue {
		opts := deck.Options(c, dueOptions, rng)
		if opts == nil {
			opts = []string{}
		}
		out[i] = dueCard{N: c.N, Front: c.Front, Options: opts}
	}

	if len(ctx.Flags["format"]) > 0 && ctx.Flags["format"][0] == "json" {
		data, _ := json.Marshal(out)
		_, _ = fmt.Fprintln(ctx.Out, string(data))
		return 0
	}
	for _, c := range out {
		fields := append([]string{strconv.Itoa(c.N), c.Front}, c.Options...)
		for i, f := range fields {
			fields[i] = oneLine(f)
		}
		_, _ = fmt.Fprintln(ctx.Out, strings.Join(fields, " | "))
	}
	return 0
}

// oneLine folds line breaks into spaces for the lines format.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ratingNames maps the words of `tm review` to ratings.
var ratingNames = map[string]int{
	"again": fsrs.Again,
	"hard":  fsrs.Hard,
	"good":  fsrs.Good,
	"easy":  fsrs.Easy,
}

// reviewRun is the Run handler for:
//
//	tm review <n> again|hard|good|easy
//	tm review <n> --pick "<text>"
//
// The first form records the learner's own rating and prints "ok". The second
// compares the text with the card's back, records good when they are equal
// and again when they are not, and prints "right" or "wrong | <back>"; the
// teacher never decides whether a pick was right (§17.5). A review is one
// appended row; it takes no lock and writes neither the graph nor the event
// log.
//
// Exit codes:
//
//	0  ok
//	1  the card is waiting or held
//	2  the cards file has a line that is not a card row
//	3  usage error, unknown or removed card
func reviewRun(ctx *Context) int {
	pick, picked := "", len(ctx.Flags["pick"]) > 0
	if picked {
		pick = ctx.Flags["pick"][0]
	}
	rated := len(ctx.Positionals) > 1
	if rated == picked {
		return cardUsageErr(ctx, "review", "give a rating or --pick, not both and not neither")
	}
	set, code := cardSettings(ctx)
	if code != 0 {
		return code
	}
	deck, passed, file, code := loadCards(ctx)
	if code != 0 {
		return code
	}
	c, code := liveCard(ctx, "review", ctx.Positionals[0], deck)
	if code != 0 {
		return code
	}
	switch c.State(passed) {
	case cards.StateWaiting:
		ctx.ErrMsg = fmt.Sprintf("card %d is waiting: %s is not passed", c.N, c.Concept)
		ctx.FixMsg = fmt.Sprintf("pass %s first", c.Concept)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 1
	case cards.StateHeld:
		return cardErr(ctx, 1, fmt.Sprintf("card %d is held", c.N))
	}

	rating, via, reply := fsrs.Good, cards.ViaPick, "right"
	switch {
	case rated:
		via, reply = cards.ViaRate, "ok"
		rating = ratingNames[ctx.Positionals[1]]
	case strings.TrimSpace(pick) != strings.TrimSpace(c.Back):
		rating, reply = fsrs.Again, "wrong | "+c.Back
	}

	now := cardNow()
	if err := cards.Append(cards.Path(file), now, c.ReviewRow(rating, via, now, set.Retention)); err != nil {
		return cardErr(ctx, 3, fmt.Sprintf("cannot write cards file: %v", err))
	}
	_, _ = fmt.Fprintln(ctx.Out, reply)
	return 0
}
