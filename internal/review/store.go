// Package review is tmcards, the learner's flashcard review tool of spec
// §17.5: a terminal loop and a local web page over the cards file. It reads
// the graph to learn which concepts are passed, reads and appends to the
// cards file, and never writes the graph or the event log.
package review

import (
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"time"

	"github.com/reithan/teach-me/internal/cards"
	"github.com/reithan/teach-me/internal/graph"
)

// stepSpread is how many of the next cards a card that has come due is mixed
// among (§17.4, "Order").
const stepSpread = 3

// Store is one graph's cards as tmcards sees them. Every read loads the graph
// and the cards file afresh, because tm and other tmcards processes append to
// the cards file at any time.
type Store struct {
	GraphFile string
	Set       cards.Settings
	// Now returns the current time in the local zone, which sets the day
	// boundary.
	Now func() time.Time
	Rng *rand.Rand
}

// NewStore returns a store for the graph at graphFile with the wall clock and
// a freshly seeded random source.
func NewStore(graphFile string, set cards.Settings) *Store {
	return &Store{GraphFile: graphFile, Set: set, Now: time.Now, Rng: cards.NewRand()}
}

// View is the deck at one moment, with what is due.
type View struct {
	Deck   *cards.Deck
	Passed func(concept string) bool
	Due    cards.Due
	Now    time.Time
}

// Load reads the graph and the cards file.
func (st *Store) Load() (*View, error) {
	data, err := os.ReadFile(st.GraphFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", st.GraphFile, err)
	}
	g, err := graph.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", st.GraphFile, err)
	}
	passedSet := make(map[string]bool, len(g.PassedConcepts))
	for _, c := range g.PassedConcepts {
		passedSet[c.ID] = true
	}
	passed := func(id string) bool { return passedSet[id] }

	deck, err := cards.Load(cards.Path(st.GraphFile))
	if err != nil {
		return nil, err
	}
	now := st.Now()
	return &View{Deck: deck, Passed: passed, Due: deck.Due(passed, now, st.Set.New), Now: now}, nil
}

// NotReviewableError reports a review of a card that cannot take one.
type NotReviewableError struct {
	N     int
	State cards.State // "" when the card does not exist
}

func (e *NotReviewableError) Error() string {
	switch e.State {
	case "":
		return fmt.Sprintf("unknown card %d", e.N)
	case cards.StateRemoved:
		return fmt.Sprintf("card %d was removed", e.N)
	default:
		return fmt.Sprintf("card %d is %s", e.N, e.State)
	}
}

// live returns card n when it exists and is not removed.
func (v *View) live(n int) (*cards.Card, error) {
	c := v.Deck.Card(n)
	if c == nil {
		return nil, &NotReviewableError{N: n}
	}
	if c.Removed {
		return nil, &NotReviewableError{N: n, State: cards.StateRemoved}
	}
	return c, nil
}

// Rate records a review of card n: one appended row, no lock. A waiting,
// held, removed, or unknown card returns a *NotReviewableError.
func (st *Store) Rate(n, rating int, via string) (cards.Row, error) {
	v, err := st.Load()
	if err != nil {
		return cards.Row{}, err
	}
	c, err := v.live(n)
	if err != nil {
		return cards.Row{}, err
	}
	if s := c.State(v.Passed); s == cards.StateWaiting || s == cards.StateHeld {
		return cards.Row{}, &NotReviewableError{N: n, State: s}
	}
	row := c.ReviewRow(rating, via, v.Now, st.Set.Retention)
	return row, st.append(row)
}

func (st *Store) append(row cards.Row) error {
	if err := cards.Append(cards.Path(st.GraphFile), st.Now(), row); err != nil {
		return fmt.Errorf("cannot write cards file: %w", err)
	}
	return nil
}

// Session is one sitting: a queue of card numbers in an order that is random
// and new for every session. It holds numbers, not cards, and checks them
// against the files on every step, so a card reviewed, held, or removed
// elsewhere drops out.
type Session struct {
	st    *Store
	queue []int
}

// NewSession starts a sitting with every card that is due now.
func (st *Store) NewSession() (*Session, error) {
	v, err := st.Load()
	if err != nil {
		return nil, err
	}
	s := &Session{st: st}
	for _, c := range v.Due.Session(st.Rng) {
		s.queue = append(s.queue, c.N)
	}
	return s, nil
}

// Next returns the card to show and how many cards are left including it, or
// nil when the sitting is over. It does not remove the card: a rating does,
// because a rated card stops being due.
//
// The queue is brought up to date first. Cards no longer due are dropped. A
// card that has come due since (a step whose time passed) goes to a random
// place among the next few cards, never to the front by rule. When nothing
// is left, a card on a step due within cards.EarlyWindow is shown early.
func (s *Session) Next() (*cards.Card, int, error) {
	v, err := s.st.Load()
	if err != nil {
		return nil, 0, err
	}
	due := make(map[int]bool, v.Due.Count())
	for _, group := range [][]*cards.Card{v.Due.Review, v.Due.New, v.Due.Steps} {
		for _, c := range group {
			due[c.N] = true
		}
	}
	s.queue = slices.DeleteFunc(s.queue, func(n int) bool { return !due[n] })

	late := make([]int, 0, len(due))
	for n := range due {
		if !slices.Contains(s.queue, n) {
			late = append(late, n)
		}
	}
	slices.Sort(late) // map order is not seeded by Rng; sort, then place at random
	for _, n := range late {
		at := s.st.Rng.IntN(min(len(s.queue), stepSpread) + 1)
		s.queue = slices.Insert(s.queue, at, n)
	}

	if len(s.queue) > 0 {
		return v.Deck.Card(s.queue[0]), len(s.queue), nil
	}
	if soon := v.Due.Soon; len(soon) > 0 {
		return soon[s.st.Rng.IntN(len(soon))], len(soon), nil
	}
	return nil, 0, nil
}
