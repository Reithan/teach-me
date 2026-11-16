package cards

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// State is the derived state of a card (§17.2).
type State string

// Card states, in the order the §17.2 table tests them.
const (
	StateRemoved  State = "removed"
	StateWaiting  State = "waiting"
	StateHeld     State = "held"
	StateNew      State = "new"
	StateLearning State = "learning"
	StateReview   State = "review"
)

// Card is one card with everything the rows say about it so far.
type Card struct {
	N       int
	Concept string // "" when the card belongs to no concept
	Origin  string
	Src     string // "" when the card has no citation
	// Front and Back are the current text: the card row's, or the latest
	// edit row's.
	Front string
	Back  string

	Removed bool
	Held    bool
	// Reviews is the number of review rows.
	Reviews int
	// Lapses is the number of again ratings the card got while in review
	// (§17.4).
	Lapses int
	// First is the time of the first review row, zero for a card never
	// reviewed. The daily limit on new cards counts by it (§17.4).
	First time.Time
	// Last is the latest review row, nil for a card never reviewed. The
	// scheduler reads this row and nothing earlier.
	Last *Row
}

// State derives the card's state. passed reports whether a concept is in the
// graph's passed block; a card tied to a concept that is not passed, or that
// no longer exists, is waiting.
func (c *Card) State(passed func(concept string) bool) State {
	switch {
	case c.Removed:
		return StateRemoved
	case c.Concept != "" && !passed(c.Concept):
		return StateWaiting
	case c.Held:
		return StateHeld
	case c.Last == nil:
		return StateNew
	case c.Last.Step > 0:
		return StateLearning
	default:
		return StateReview
	}
}

// Deck is the state of every card in one cards file.
type Deck struct {
	// Cards holds every card ever added, removed ones included, in number
	// order.
	Cards []*Card
	byN   map[int]*Card
	max   int
}

// RowError reports a line of the cards file that is not a valid row. Its
// message is the §7 refusal text.
type RowError struct {
	File string // base name of the cards file
	Line int    // 1-based
}

func (e *RowError) Error() string {
	return fmt.Sprintf("%s line %d is not a card row", e.File, e.Line)
}

// Load reads the cards file at path and replays it. A missing file is an
// empty deck. A line that is not a valid row returns a *RowError.
func Load(path string) (*Deck, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Replay(nil, "")
		}
		return nil, err
	}
	return Replay(data, filepath.Base(path))
}

// Replay derives a deck from the bytes of a cards file. name is the file name
// used in a *RowError.
func Replay(data []byte, name string) (*Deck, error) {
	d := &Deck{byN: make(map[int]*Card)}
	lines := bytes.Split(data, []byte{'\n'})
	if n := len(lines); n > 0 && len(lines[n-1]) == 0 {
		lines = lines[:n-1] // the file ends with a newline
	}
	for i, line := range lines {
		row, ok := decodeRow(line)
		if !ok || !d.apply(row) {
			return nil, &RowError{File: name, Line: i + 1}
		}
	}
	return d, nil
}

// apply folds one row into the deck. It returns false when the row cannot
// follow the rows before it: a card number used twice, or a row about a card
// that was never added.
func (d *Deck) apply(r Row) bool {
	if r.Ev == EvCard {
		if d.byN[r.N] != nil {
			return false
		}
		c := &Card{N: r.N, Concept: r.Concept, Origin: r.Origin, Src: r.Src, Front: r.Front, Back: r.Back}
		d.byN[r.N] = c
		d.Cards = append(d.Cards, c)
		if r.N > d.max {
			d.max = r.N
		}
		return true
	}
	c := d.byN[r.N]
	if c == nil {
		return false
	}
	if c.Removed {
		// tmcards appends without the lock, so a row can land after an rm
		// row written at the same moment. It changes nothing.
		return true
	}
	switch r.Ev {
	case EvReview:
		if r.Rating == Again && c.Last != nil && c.Last.Step == 0 {
			c.Lapses++
		}
		if c.Reviews == 0 {
			c.First = r.T
		}
		c.Reviews++
		row := r
		c.Last = &row
	case EvEdit:
		c.Front, c.Back = r.Front, r.Back
	case EvHold:
		c.Held = r.On
	case EvRemove:
		c.Removed = true
	}
	return true
}

// Card returns card n, or nil when no such card was ever added. A removed
// card is returned with Removed set.
func (d *Deck) Card(n int) *Card {
	return d.byN[n]
}

// NextN returns the number the next card gets. Numbers start at 1 and are
// never reused, so a removed card keeps its number.
func (d *Deck) NextN() int {
	return d.max + 1
}

// Has reports whether a card that is not removed has this concept, front, and
// back (§17.3).
func (d *Deck) Has(concept, front, back string) bool {
	for _, c := range d.Cards {
		if !c.Removed && c.Concept == concept && c.Front == front && c.Back == back {
			return true
		}
	}
	return false
}

// OfConcept returns the cards of one concept that are not removed, in number
// order.
func (d *Deck) OfConcept(concept string) []*Card {
	var out []*Card
	for _, c := range d.Cards {
		if !c.Removed && c.Concept == concept {
			out = append(out, c)
		}
	}
	return out
}
