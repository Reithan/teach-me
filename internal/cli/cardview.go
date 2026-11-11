package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/cards"
	"github.com/reithan/teach-me/internal/graph"
)

// cardsForView loads the deck and the card settings for tm status and
// tm show. Those commands orient the teacher and must keep working when the
// cards file or a card setting is broken, so a failure here prints the error
// on stderr and reports ok=false; the caller then leaves the card lines out
// and still exits 0.
func cardsForView(ctx *Context) (deck *cards.Deck, set cards.Settings, ok bool) {
	set, err := cards.LoadSettings()
	if err == nil {
		deck, err = cards.Load(cards.Path(ctx.GraphFile))
	}
	if err != nil {
		writeErrFix(ctx.ErrOut, err.Error(), "")
		return nil, set, false
	}
	return deck, set, true
}

// cardsDueSuffix is the "  cards due <n>" part of the status counts line,
// empty when no card is due (§17.6).
func cardsDueSuffix(deck *cards.Deck, set cards.Settings, passed map[string]bool) string {
	n := deck.Due(func(id string) bool { return passed[id] }, cardNow(), set.New).Count()
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("  cards due %d", n)
}

// leechSuffix is the "  leech <m>/<n> cards" part of a passed concept's
// status line: m of the concept's n cards have lapsed at least set.Leech
// times. It is empty when the concept has no leech.
func leechSuffix(deck *cards.Deck, set cards.Settings, concept string) string {
	own := deck.OfConcept(concept)
	leeches := 0
	for _, c := range own {
		if c.Lapses >= set.Leech {
			leeches++
		}
	}
	if leeches == 0 {
		return ""
	}
	return fmt.Sprintf("  leech %d/%d cards", leeches, len(own))
}

// showCards prints a concept's cards for tm show: a blank line, a count, and
// one line per card with its number, state, and front (§17.6). It prints
// nothing for a concept with no cards, and opens the cards file only for a
// concept that carries the %% tm:cards line.
func showCards(ctx *Context, g *graph.Graph, concept string) {
	if !hasCardsLine(g, concept) {
		return
	}
	deck, _, ok := cardsForView(ctx)
	if !ok {
		return
	}
	own := deck.OfConcept(concept)
	if len(own) == 0 {
		return
	}
	passed := graphNodeSets(g, false).PassedSet
	_, _ = fmt.Fprintf(ctx.Out, "\ncards %d\n", len(own))
	for _, c := range own {
		state := c.State(func(id string) bool { return passed[id] })
		_, _ = fmt.Fprintf(ctx.Out, "  %d %s  %s\n", c.N, state, oneLine(c.Front))
	}
}

// hasCardsLine reports whether the concept carries the %% tm:cards line.
func hasCardsLine(g *graph.Graph, concept string) bool {
	for _, block := range [][]*graph.ConceptNode{g.PassedConcepts, g.UntestedConcepts, g.ReserveConcepts} {
		for _, c := range block {
			if c.ID == concept {
				return c.Cards
			}
		}
	}
	return false
}
