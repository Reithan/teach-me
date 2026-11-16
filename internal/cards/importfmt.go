package cards

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Pair is one card read from an import list.
type Pair struct {
	Line  int // 1-based line number in the list
	Front string
	Back  string
}

// ImportError reports the first malformed line of an import list. Its message
// is the §7 refusal text, "line <N>: <what is wrong>".
type ImportError struct {
	Line int
	What string
}

func (e *ImportError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.What)
}

// ParseImport reads the tab-separated list of §17.3: one card per line, front,
// a tab, back. Fields after the second are ignored, and so are blank lines and
// lines starting with #. Line endings may be LF or CRLF, and a leading byte
// order mark is dropped. A malformed line returns an *ImportError and no
// pairs, so an import writes all of its cards or none.
func ParseImport(text string) ([]Pair, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	var pairs []Pair
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		line = strings.TrimSuffix(line, "\r")
		if (strings.TrimSpace(line) == "" && !strings.Contains(line, "\t")) || strings.HasPrefix(line, "#") {
			continue
		}
		if !utf8.ValidString(line) {
			return nil, &ImportError{n, "not UTF-8"}
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			return nil, &ImportError{n, "no tab between front and back"}
		}
		if what := CheckField(fields[0]); what != "" {
			return nil, &ImportError{n, "front " + what}
		}
		if what := CheckField(fields[1]); what != "" {
			return nil, &ImportError{n, "back " + what}
		}
		pairs = append(pairs, Pair{Line: n, Front: fields[0], Back: fields[1]})
	}
	return pairs, nil
}
