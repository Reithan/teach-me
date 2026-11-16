package cards

import (
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/reithan/teach-me/internal/fsrs"
)

// EarlyWindow is how far ahead a card on a step may be shown when nothing
// else is left (§17.4, "Showing early").
const EarlyWindow = 20 * time.Minute

// stepSpread is how many of the next cards a due step card is mixed among.
const stepSpread = 3

// Sched returns the card as the scheduler sees it before its next review.
func (c *Card) Sched() fsrs.Card {
	if c.Last == nil {
		return fsrs.Card{}
	}
	return fsrs.Card{
		Reviewed: true,
		S:        c.Last.S,
		D:        c.Last.D,
		Step:     c.Last.Step,
		Lapsed:   c.Lapses > 0,
		Last:     c.Last.T,
	}
}

// ReviewRow schedules a review of c and returns the row that records it.
// now carries the local time zone, which sets the day boundary. Stability
// and difficulty are stored to four decimal places.
func (c *Card) ReviewRow(rating int, via string, now time.Time, retention float64) Row {
	res := fsrs.Next(c.Sched(), rating, now, retention)
	return Row{
		T:      now,
		Ev:     EvReview,
		N:      c.N,
		Rating: rating,
		Via:    via,
		S:      round4(res.S),
		D:      round4(res.D),
		Step:   res.Step,
		Due:    res.Due,
	}
}

func round4(x float64) float64 { return math.Round(x*1e4) / 1e4 }

// Due is the set of cards that can be reviewed at one moment, in number
// order. It is a snapshot; Session turns it into an order to show.
type Due struct {
	Review []*Card // cards in review whose due day has come
	New    []*Card // new cards inside today's limit
	Steps  []*Card // cards on a step whose due time has passed
	Soon   []*Card // cards on a step due within EarlyWindow
}

// Count is the number of cards due now: the `cards due` figure of tm status.
// Cards in Soon are not counted.
func (q Due) Count() int { return len(q.Review) + len(q.New) + len(q.Steps) }

// Due sorts the deck's cards into what can be reviewed at now. passed reports
// whether a concept is in the graph's passed block. newLimit is the number of
// cards that may get their first review on one local day; cards first
// reviewed earlier today use it up, and the lowest-numbered new cards take
// what is left.
func (d *Deck) Due(passed func(string) bool, now time.Time, newLimit int) Due {
	var q Due
	room := newLimit
	for _, c := range d.Cards {
		if c.Reviews > 0 && !c.First.After(now) && fsrs.Days(c.First, now) == 0 {
			room--
		}
	}
	for _, c := range d.Cards {
		switch c.State(passed) {
		case StateNew:
			if room > 0 {
				q.New = append(q.New, c)
				room--
			}
		case StateLearning:
			switch wait := c.Last.Due.Sub(now); {
			case wait <= 0:
				q.Steps = append(q.Steps, c)
			case wait <= EarlyWindow:
				q.Soon = append(q.Soon, c)
			}
		case StateReview:
			// Due from the start of the local day that due falls on.
			if !c.Last.Due.After(now) || fsrs.Days(now, c.Last.Due.In(now.Location())) == 0 {
				q.Review = append(q.Review, c)
			}
		}
	}
	return q
}

// Session returns the cards to show, in a fresh random order every call
// (§17.4, "Order"). Review and new cards are shuffled into one queue. A card
// on a step is placed at a random position among the next few cards. When
// nothing is due, the cards in Soon are shown early.
func (q Due) Session(rng *rand.Rand) []*Card {
	out := make([]*Card, 0, q.Count())
	out = append(out, q.Review...)
	out = append(out, q.New...)
	shuffle(rng, out)

	steps := append([]*Card(nil), q.Steps...)
	shuffle(rng, steps)
	for _, c := range steps {
		at := rng.IntN(min(len(out), stepSpread) + 1)
		out = append(out, nil)
		copy(out[at+1:], out[at:])
		out[at] = c
	}

	if len(out) == 0 {
		out = append(out, q.Soon...)
		shuffle(rng, out)
	}
	return out
}

// Options returns the answer choices for c: its back and up to max-1 backs
// of other cards that are not removed, cards of the same concept first, in
// random order. It returns nil when no other card has a different back,
// because one choice is no choice (§17.5).
func (d *Deck) Options(c *Card, limit int, rng *rand.Rand) []string {
	var same, other []string
	seen := map[string]bool{c.Back: true}
	for _, o := range d.Cards {
		if o.Removed || seen[o.Back] {
			continue
		}
		seen[o.Back] = true
		if o.Concept == c.Concept {
			same = append(same, o.Back)
		} else {
			other = append(other, o.Back)
		}
	}
	shuffle(rng, same)
	shuffle(rng, other)
	pool := slices.Concat(same, other)
	if len(pool) == 0 {
		return nil
	}
	opts := append([]string{c.Back}, pool[:min(len(pool), limit-1)]...)
	shuffle(rng, opts)
	return opts
}

// NewRand returns a generator seeded from the system's random source.
func NewRand() *rand.Rand {
	return rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) //nolint:gosec // card order, not a secret
}

func shuffle[T any](rng *rand.Rand, s []T) {
	rng.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
}
