// Package cards implements the cards file of spec §17.2: an append-only
// <graph>.cards.jsonl that holds every flashcard and every review. The state
// of a card is derived by reading the file from the top (decision 86).
//
// The package reads and appends rows, derives card state, and parses the
// tab-separated import format of §17.3. It knows nothing about scheduling;
// a review row carries the scheduler's result as plain numbers.
package cards

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

// MaxFieldChars is the longest front or back a card may have (§7, §17.3).
const MaxFieldChars = 1000

// Row kinds, the values of a row's "ev" field.
const (
	EvCard   = "card"
	EvReview = "review"
	EvEdit   = "edit"
	EvHold   = "hold"
	EvRemove = "rm"
)

// Card origins (§17.1).
const (
	OriginSource  = "source"
	OriginUser    = "user"
	OriginTeacher = "teacher"
)

// Review paths, the values of a review row's "via" field.
const (
	ViaServe = "serve" // the tmcards review page
	ViaTerm  = "term"  // tmcards review
	ViaRate  = "rate"  // tm review <n> <rating>
	ViaPick  = "pick"  // tm review <n> --pick
)

// Ratings (§17.4).
const (
	Again = 1
	Hard  = 2
	Good  = 3
	Easy  = 4
)

// Path returns the cards file path for the given graph file (§3).
func Path(graphFile string) string {
	return graphFile + ".cards.jsonl"
}

// IsCardsFile reports whether path names a cards file. A cards file is state,
// never a source (§7).
func IsCardsFile(path string) bool {
	return strings.HasSuffix(path, ".cards.jsonl")
}

// ValidOrigin reports whether s is one of the three card origins.
func ValidOrigin(s string) bool {
	return s == OriginSource || s == OriginUser || s == OriginTeacher
}

func validVia(s string) bool {
	return s == ViaServe || s == ViaTerm || s == ViaRate || s == ViaPick
}

// Row is one line of the cards file. Ev selects which of the other fields are
// meaningful; the rest stay zero.
type Row struct {
	// T is the time the row was written. Append stamps it when zero.
	T  time.Time
	Ev string
	// N is the card number the row is about.
	N int

	// Card rows. Concept and Src are "" for JSON null. Edit rows carry Front
	// and Back too.
	Concept string
	Origin  string
	Front   string
	Back    string
	Src     string

	// Review rows: the rating given, the path it came by, and the scheduler's
	// result (§17.4). Step is the learning or relearning steps left, 0 for a
	// card in review.
	Rating int
	Via    string
	S      float64
	D      float64
	Step   int
	Due    time.Time

	// Hold rows.
	On bool
}

// The wire structs fix the key order of each row kind.
type (
	wireCard struct {
		T       string  `json:"t"`
		Ev      string  `json:"ev"`
		N       int     `json:"n"`
		Concept *string `json:"concept"`
		Origin  string  `json:"origin"`
		Front   string  `json:"front"`
		Back    string  `json:"back"`
		Src     *string `json:"src"`
	}
	wireReview struct {
		T      string  `json:"t"`
		Ev     string  `json:"ev"`
		N      int     `json:"n"`
		Rating int     `json:"rating"`
		Via    string  `json:"via"`
		S      float64 `json:"s"`
		D      float64 `json:"d"`
		Step   int     `json:"step"`
		Due    string  `json:"due"`
	}
	wireEdit struct {
		T     string `json:"t"`
		Ev    string `json:"ev"`
		N     int    `json:"n"`
		Front string `json:"front"`
		Back  string `json:"back"`
	}
	wireHold struct {
		T  string `json:"t"`
		Ev string `json:"ev"`
		N  int    `json:"n"`
		On bool   `json:"on"`
	}
	wireRemove struct {
		T  string `json:"t"`
		Ev string `json:"ev"`
		N  int    `json:"n"`
	}
)

// wireAny reads any row kind. Pointers tell an absent field from a zero one.
type wireAny struct {
	T       *string  `json:"t"`
	Ev      *string  `json:"ev"`
	N       *int     `json:"n"`
	Concept *string  `json:"concept"`
	Origin  *string  `json:"origin"`
	Front   *string  `json:"front"`
	Back    *string  `json:"back"`
	Src     *string  `json:"src"`
	Rating  *int     `json:"rating"`
	Via     *string  `json:"via"`
	S       *float64 `json:"s"`
	D       *float64 `json:"d"`
	Step    *int     `json:"step"`
	Due     *string  `json:"due"`
	On      *bool    `json:"on"`
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// encode returns the row as one JSON line, newline included.
func (r Row) encode() ([]byte, error) {
	t := r.T.UTC().Format(time.RFC3339)
	var v any
	switch r.Ev {
	case EvCard:
		v = wireCard{t, r.Ev, r.N, nullable(r.Concept), r.Origin, r.Front, r.Back, nullable(r.Src)}
	case EvReview:
		v = wireReview{t, r.Ev, r.N, r.Rating, r.Via, r.S, r.D, r.Step, r.Due.UTC().Format(time.RFC3339)}
	case EvEdit:
		v = wireEdit{t, r.Ev, r.N, r.Front, r.Back}
	case EvHold:
		v = wireHold{t, r.Ev, r.N, r.On}
	case EvRemove:
		v = wireRemove{t, r.Ev, r.N}
	default:
		return nil, errors.New("cards: unknown row kind " + r.Ev)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // card text is plain text; keep it readable
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeRow parses one line. ok is false when the line is not a valid row.
func decodeRow(line []byte) (Row, bool) {
	var w wireAny
	if err := json.Unmarshal(line, &w); err != nil {
		return Row{}, false
	}
	if w.T == nil || w.Ev == nil || w.N == nil || *w.N < 1 {
		return Row{}, false
	}
	t, err := time.Parse(time.RFC3339, *w.T)
	if err != nil {
		return Row{}, false
	}
	r := Row{T: t, Ev: *w.Ev, N: *w.N}
	switch r.Ev {
	case EvCard:
		if w.Origin == nil || !ValidOrigin(*w.Origin) || !validText(w.Front) || !validText(w.Back) {
			return Row{}, false
		}
		r.Concept, r.Origin, r.Src = deref(w.Concept), *w.Origin, deref(w.Src)
		r.Front, r.Back = *w.Front, *w.Back
	case EvReview:
		if w.Rating == nil || *w.Rating < Again || *w.Rating > Easy ||
			w.Via == nil || !validVia(*w.Via) ||
			w.S == nil || *w.S <= 0 || w.D == nil || *w.D < 1 || *w.D > 10 ||
			w.Step == nil || *w.Step < 0 || w.Due == nil {
			return Row{}, false
		}
		due, err := time.Parse(time.RFC3339, *w.Due)
		if err != nil {
			return Row{}, false
		}
		r.Rating, r.Via, r.S, r.D, r.Step, r.Due = *w.Rating, *w.Via, *w.S, *w.D, *w.Step, due
	case EvEdit:
		if !validText(w.Front) || !validText(w.Back) {
			return Row{}, false
		}
		r.Front, r.Back = *w.Front, *w.Back
	case EvHold:
		if w.On == nil {
			return Row{}, false
		}
		r.On = *w.On
	case EvRemove:
	default:
		return Row{}, false
	}
	return r, true
}

func validText(p *string) bool {
	return p != nil && CheckField(*p) == ""
}

// CheckField returns what is wrong with a card's front or back, or "" when it
// is acceptable: not empty or only whitespace, and at most MaxFieldChars
// characters (§7).
func CheckField(s string) string {
	if strings.TrimSpace(s) == "" {
		return "is empty"
	}
	if n := len([]rune(s)); n > MaxFieldChars {
		return "is over 1,000 characters"
	}
	return ""
}

// Append writes rows to the cards file at path in one O_APPEND write, creating
// the file if needed. A row with a zero T is stamped with now. The caller
// holds the graph lock when any row is a card row, because a card's number
// comes from reading the file (§17.2); other rows need no lock.
func Append(path string, now time.Time, rows ...Row) error {
	var data []byte
	for _, r := range rows {
		if r.T.IsZero() {
			r.T = now
		}
		line, err := r.encode()
		if err != nil {
			return err
		}
		data = append(data, line...)
	}
	if len(data) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
