package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/reithan/teach-me/internal/cards"
	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/source"
	"github.com/reithan/teach-me/internal/state"
)

// The tm card commands (§6, §17). add and import write card rows, which takes
// the graph lock because a card's number comes from reading the cards file
// (§17.2); they run inside ops.Mutate for that lock and to set %% tm:cards on
// the concept. edit and rm append one row without the lock. show only reads.

// cardUsageErr prints a usage error for a card command and returns exit 3.
func cardUsageErr(ctx *Context, cmd, msg string) int {
	ctx.ErrMsg = msg
	ctx.FixMsg = FindCommand(cmd).Usage()
	writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
	return 3
}

// cardErr prints an error with no fix line and returns code.
func cardErr(ctx *Context, code int, msg string) int {
	ctx.ErrMsg = msg
	writeErrFix(ctx.ErrOut, msg, "")
	return code
}

// cardFile resolves the graph file for a card command.
func cardFile(ctx *Context) (string, int) {
	file, err := state.ResolveFile(ctx.FileFlag)
	if err != nil {
		return "", cardErr(ctx, 3, err.Error())
	}
	ctx.GraphFile = file
	return file, 0
}

// checkCardText refuses a front or back that is empty or too long (§7).
func checkCardText(front, back string) string {
	if what := cards.CheckField(front); what != "" {
		return "front " + what
	}
	if what := cards.CheckField(back); what != "" {
		return "back " + what
	}
	return ""
}

// deckRefusal turns a cards-file load error into a refusal. A line that is
// not a card row is exit 2, like a graph that fails lint: the state on disk is
// broken and no argument fixes it.
func deckRefusal(err error) *ops.Refusal {
	var re *cards.RowError
	if errors.As(err, &re) {
		return &ops.Refusal{Err: re.Error(), Exit: 2}
	}
	return &ops.Refusal{Err: fmt.Sprintf("cannot read cards file: %v", err), Exit: 3}
}

// loadDeck reads the cards file beside the graph, printing the refusal on
// failure.
func loadDeck(ctx *Context, file string) (*cards.Deck, int) {
	deck, err := cards.Load(cards.Path(file))
	if err != nil {
		r := deckRefusal(err)
		return nil, cardErr(ctx, r.Exit, r.Err)
	}
	return deck, 0
}

// cardMutate runs apply under the graph lock and reports a refusal the way
// runMutation does, but prints nothing on success.
func cardMutate(ctx *Context, file string, apply ops.Apply) int {
	_, refusal, engErr := ops.Mutate(file, state.ConfigFromEnv(), buildLintConfig(file), errlog.RealClock, apply)
	if engErr != nil {
		return cardErr(ctx, 3, fmt.Sprintf("cannot mutate %s: %v", filepath.Base(file), engErr))
	}
	if refusal != nil {
		ctx.ErrMsg = refusal.Err
		ctx.FixMsg = refusal.Fix
		writeErrFix(ctx.ErrOut, refusal.Err, refusal.Fix)
		return refusal.Exit
	}
	return 0
}

// markCards returns g with %% tm:cards set on the concept, or g itself when
// the line is already there.
func markCards(g *graph.Graph, concept string) *graph.Graph {
	cn := findConceptNode(g, concept)
	if cn == nil || cn.Cards {
		return g
	}
	marked := *cn
	marked.Cards = true
	return replaceConceptInGraph(g, &marked)
}

// conceptField is the event log's concept value: the ID, or null.
func conceptField(concept string) any {
	if concept == "" {
		return nil
	}
	return concept
}

// cardAddRun is the Run handler for:
//
//	tm card add <concept> <cite> "<front>" "<back>"
//
// Adds one card to a passed concept and prints its number.
//
// Exit codes:
//
//	0  ok; prints the card number
//	1  the concept is not passed, or the citation refuses
//	2  the cards file has a line that is not a card row
//	3  usage error, unknown concept, bad citation, bad card text
func cardAddRun(ctx *Context) int {
	concept, citeStr := ctx.Positionals[0], ctx.Positionals[1]
	front, back := ctx.Positionals[2], ctx.Positionals[3]

	if msg := checkCardText(front, back); msg != "" {
		return cardUsageErr(ctx, "card add", msg)
	}
	file, code := cardFile(ctx)
	if code != 0 {
		return code
	}
	resolver, err := source.NewResolver(filepath.Dir(file))
	if err != nil {
		return cardErr(ctx, 3, fmt.Sprintf("source config: %v", err))
	}
	// The citation resolves as it does for tm q, without the size cap (§7).
	hashed, _, err := resolver.HashCitation(citeStr)
	if err != nil {
		return citeHashError(ctx, citeStr, aidRefusalWithID(err, concept), FindCommand("card add").Usage())
	}

	var n int
	apply := func(g *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		ns := graphNodeSets(g, true)
		if !ns.AllConcepts[concept] {
			return nil, nil, &ops.Refusal{Err: fmt.Sprintf("unknown concept %q", concept), Exit: 3}
		}
		if !ns.PassedSet[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is not passed",
				Fix:  fmt.Sprintf("pass it first, or attach a list with tm card import <locator> --concept %s", concept),
				Exit: 1,
			}
		}
		path := cards.Path(file)
		deck, err := cards.Load(path)
		if err != nil {
			return nil, nil, deckRefusal(err)
		}
		n = deck.NextN()
		row := cards.Row{
			Ev: cards.EvCard, N: n, Concept: concept, Origin: cards.OriginSource,
			Front: front, Back: back, Src: hashed,
		}
		if err := cards.Append(path, errlog.RealClock.Now(), row); err != nil {
			return nil, nil, &ops.Refusal{Err: fmt.Sprintf("cannot write cards file: %v", err), Exit: 3}
		}
		ev := eventlog.NewRow("card", map[string]any{"action": "add", "cards": []int{n}, "concept": concept})
		return markCards(g, concept), []eventlog.Row{ev}, nil
	}
	if code := cardMutate(ctx, file, apply); code != 0 {
		return code
	}
	_, _ = fmt.Fprintln(ctx.Out, n)
	return 0
}

// importText reads the list for tm card import and returns its text and the
// citation each card gets ("" unless the origin is source).
//
// With origin source the locator resolves as tm src resolves one, and the
// citation covers the whole file. With user or teacher the locator is a plain
// file path, or - for stdin, and is not treated as a source (§17.3).
func importText(ctx *Context, file, locator, origin string) (text, src string, code int) {
	if origin != cards.OriginSource {
		var data []byte
		var err error
		if locator == "-" {
			data, err = io.ReadAll(ctx.In)
		} else {
			data, err = os.ReadFile(locator)
		}
		if err != nil {
			return "", "", cardErr(ctx, 3, fmt.Sprintf("cannot read %s: %v", locator, err))
		}
		return string(data), "", 0
	}
	usage := FindCommand("card import").Usage()
	if locator == "-" {
		return "", "", cardUsageErr(ctx, "card import", "stdin is not a source; give --origin user or --origin teacher")
	}
	resolver, err := source.NewResolver(filepath.Dir(file))
	if err != nil {
		return "", "", cardErr(ctx, 3, fmt.Sprintf("source config: %v", err))
	}
	text, _, err = resolver.ReadAll(locator)
	if err != nil {
		return "", "", citeHashError(ctx, locator, err, usage)
	}
	whole := fmt.Sprintf("%s:1-%d", locator, strings.Count(text, "\n")+1)
	src, _, err = resolver.HashCitation(whole)
	if err != nil {
		return "", "", citeHashError(ctx, whole, err, usage)
	}
	return text, src, 0
}

// cardImportRun is the Run handler for:
//
//	tm card import <locator> [--concept <id>] [--origin source|user|teacher]
//
// Adds one card per line of a tab-separated list and prints
// "cards <added> skipped <n>". A malformed line refuses the whole import.
//
// Exit codes:
//
//	0  ok
//	1  the locator refuses as a source
//	2  the cards file has a line that is not a card row
//	3  usage error, unknown concept, unreadable or malformed list
func cardImportRun(ctx *Context) int {
	locator := ctx.Positionals[0]
	concept, origin := "", cards.OriginSource
	if v := ctx.Flags["concept"]; len(v) > 0 {
		concept = v[0]
	}
	if v := ctx.Flags["origin"]; len(v) > 0 {
		origin = v[0]
	}

	file, code := cardFile(ctx)
	if code != 0 {
		return code
	}
	text, src, code := importText(ctx, file, locator, origin)
	if code != 0 {
		return code
	}
	pairs, err := cards.ParseImport(text)
	if err != nil {
		return cardErr(ctx, 3, err.Error())
	}

	added, skipped := 0, 0
	apply := func(g *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		// A list may attach to a concept in any block; its cards wait until
		// the concept is passed.
		if concept != "" && !graphNodeSets(g, true).AllConcepts[concept] {
			return nil, nil, &ops.Refusal{Err: fmt.Sprintf("unknown concept %q", concept), Exit: 3}
		}
		path := cards.Path(file)
		deck, err := cards.Load(path)
		if err != nil {
			return nil, nil, deckRefusal(err)
		}
		var rows []cards.Row
		var numbers []int
		seen := make(map[[2]string]bool)
		next := deck.NextN()
		for _, p := range pairs {
			key := [2]string{p.Front, p.Back}
			if seen[key] || deck.Has(concept, p.Front, p.Back) {
				skipped++
				continue
			}
			seen[key] = true
			rows = append(rows, cards.Row{
				Ev: cards.EvCard, N: next, Concept: concept, Origin: origin,
				Front: p.Front, Back: p.Back, Src: src,
			})
			numbers = append(numbers, next)
			next++
		}
		added = len(rows)
		if added == 0 {
			return g, nil, nil
		}
		if err := cards.Append(path, errlog.RealClock.Now(), rows...); err != nil {
			return nil, nil, &ops.Refusal{Err: fmt.Sprintf("cannot write cards file: %v", err), Exit: 3}
		}
		ev := eventlog.NewRow("card", map[string]any{
			"action": "import", "cards": numbers, "concept": conceptField(concept),
			"src": locator, "origin": origin,
		})
		if concept != "" {
			g = markCards(g, concept)
		}
		return g, []eventlog.Row{ev}, nil
	}
	if code := cardMutate(ctx, file, apply); code != 0 {
		return code
	}
	_, _ = fmt.Fprintf(ctx.Out, "cards %d skipped %d\n", added, skipped)
	return 0
}

// liveCard parses a card number argument and returns that card. An unknown or
// removed card is exit 3 (§7).
func liveCard(ctx *Context, cmd, arg string, deck *cards.Deck) (*cards.Card, int) {
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 {
		return nil, cardUsageErr(ctx, cmd, fmt.Sprintf("%q is not a card number", arg))
	}
	c := deck.Card(n)
	if c == nil {
		return nil, cardErr(ctx, 3, fmt.Sprintf("unknown card %d", n))
	}
	if c.Removed {
		return nil, cardErr(ctx, 3, fmt.Sprintf("card %d was removed", n))
	}
	return c, 0
}

// orDash prints an absent value as "-".
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// cardShowRun is the Run handler for `tm card show <n>`: one card, one field
// per line. It reads the graph only to tell a waiting card from an active one.
//
// Exit codes: 0 ok; 2 broken cards file; 3 usage error, unknown or removed card.
func cardShowRun(ctx *Context) int {
	s, file, code := loadStateCtx(ctx)
	if code != 0 {
		return code
	}
	deck, code := loadDeck(ctx, file)
	if code != 0 {
		return code
	}
	c, code := liveCard(ctx, "card show", ctx.Positionals[0], deck)
	if code != 0 {
		return code
	}
	passed := graphNodeSets(s.Graph(), false).PassedSet
	_, _ = fmt.Fprintf(ctx.Out, "card %d\nstate %s\norigin %s\nconcept %s\nsrc %s\nfront %s\nback %s\n",
		c.N, c.State(func(id string) bool { return passed[id] }), c.Origin,
		orDash(c.Concept), orDash(c.Src), c.Front, c.Back)
	return 0
}

// cardAppend loads the deck, finds the card named by the first positional,
// appends the row build returns for it, logs a card event, and prints ok.
// edit and rm rows take no lock (§17.2).
func cardAppend(ctx *Context, cmd, action string, build func(n int) cards.Row) int {
	file, code := cardFile(ctx)
	if code != 0 {
		return code
	}
	deck, code := loadDeck(ctx, file)
	if code != 0 {
		return code
	}
	c, code := liveCard(ctx, cmd, ctx.Positionals[0], deck)
	if code != 0 {
		return code
	}
	if err := cards.Append(cards.Path(file), errlog.RealClock.Now(), build(c.N)); err != nil {
		return cardErr(ctx, 3, fmt.Sprintf("cannot write cards file: %v", err))
	}
	eventlog.New(eventlog.Path(file), errlog.RealClock).Append(eventlog.NewRow("card", map[string]any{
		"action": action, "cards": []int{c.N}, "concept": conceptField(c.Concept),
	}))
	_, _ = fmt.Fprintln(ctx.Out, "ok")
	return 0
}

// cardEditRun is the Run handler for `tm card edit <n> "<front>" "<back>"`.
// It rewords a card; the review history and schedule are kept (decision 97).
//
// Exit codes: 0 ok; 2 broken cards file; 3 usage error, bad text, unknown or
// removed card.
func cardEditRun(ctx *Context) int {
	front, back := ctx.Positionals[1], ctx.Positionals[2]
	if msg := checkCardText(front, back); msg != "" {
		return cardUsageErr(ctx, "card edit", msg)
	}
	return cardAppend(ctx, "card edit", "edit", func(n int) cards.Row {
		return cards.Row{Ev: cards.EvEdit, N: n, Front: front, Back: back}
	})
}

// cardRmRun is the Run handler for `tm card rm <n>`. The concept keeps its
// %% tm:cards line, which is removed only with the concept (§4.6).
//
// Exit codes: 0 ok; 2 broken cards file; 3 usage error, unknown or removed card.
func cardRmRun(ctx *Context) int {
	return cardAppend(ctx, "card rm", "rm", func(n int) cards.Row {
		return cards.Row{Ev: cards.EvRemove, N: n}
	})
}
