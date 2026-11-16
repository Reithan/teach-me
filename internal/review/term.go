package review

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/reithan/teach-me/internal/cards"
)

// ratingKeys maps the keys of the terminal loop to ratings.
var ratingKeys = map[string]int{
	"1": cards.Again,
	"2": cards.Hard,
	"3": cards.Good,
	"4": cards.Easy,
}

// Terminal runs `tmcards review`: for each due card it prints the front,
// waits for Enter, prints the back, and reads a rating from 1 to 4. q, or
// the end of input, stops the sitting. It returns the number of reviews
// recorded.
func (st *Store) Terminal(in io.Reader, out io.Writer) (int, error) {
	s, err := st.NewSession()
	if err != nil {
		return 0, err
	}
	lines := bufio.NewScanner(in)
	read := func() (string, bool) {
		if !lines.Scan() {
			return "", false
		}
		return strings.ToLower(strings.TrimSpace(lines.Text())), true
	}

	done := 0
	for {
		c, left, err := s.Next()
		if err != nil {
			return done, err
		}
		if c == nil {
			break
		}
		say(out, "\n%d left   card %d   %s\n\n%s\n\n", left, c.N, provenance(c), c.Front)
		say(out, "Enter shows the back, q quits > ")
		if key, ok := read(); !ok || key == "q" {
			break
		}
		say(out, "\n%s\n\n", c.Back)

		rating := 0
		for rating == 0 {
			say(out, "1 again  2 hard  3 good  4 easy  q quit > ")
			key, ok := read()
			if !ok || key == "q" {
				return finish(out, done), nil
			}
			rating = ratingKeys[key]
		}
		if _, err := st.Rate(c.N, rating, cards.ViaTerm); err != nil {
			return done, err
		}
		done++
	}
	return finish(out, done), nil
}

func finish(out io.Writer, done int) int {
	if done == 0 {
		say(out, "no cards reviewed\n")
	} else {
		say(out, "\nreviewed %d\n", done)
	}
	return done
}

// provenance says where a card's text came from, so the learner can tell a
// card taken from a source from one the teacher or the learner wrote.
func provenance(c *cards.Card) string {
	if c.Src == "" {
		return "origin " + c.Origin
	}
	return "origin " + c.Origin + "   " + c.Src
}

func say(out io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(out, format, args...)
}
