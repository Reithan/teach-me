package cite_test

import (
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/cite"
)

// FuzzParse verifies two properties of cite.Parse:
//
//  1. Inputs that parse successfully must round-trip through Format: the
//     re-formatted string must parse to the same Citation.
//  2. The parse-format-parse chain must be idempotent (no infinite growth).
func FuzzParse(f *testing.F) {
	seeds := []string{
		// Legacy hashless forms.
		"raft.txt:1-5",
		"src/raft.txt:120-188",
		"file.txt:5-5",
		// New hashed forms.
		"3f9a1c2b7e0d@raft.txt:202-215",
		"3f9a1c2b7e0d@/home/me/doc.go:10-24",
		"3f9a1c2b7e0d@https://example.com/doc:88-104",
		// Edge cases.
		"ab12cd34ef56@some/path/to/file.md:1-1",
		"000000000000@/abs/path:1-100",
		// Invalid inputs.
		"",
		"nocoion",
		":1-2",
		"f:0-5",
		"f:10-5",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		// Property 1: no panic.
		c, err := cite.Parse(s)
		if err != nil {
			return // invalid input is fine
		}

		// Property 2: Format must produce a string that re-parses to the same Citation.
		formatted := cite.Format(c)
		c2, err2 := cite.Parse(formatted)
		if err2 != nil {
			t.Fatalf("Parse(Format(Parse(%q))) failed: %v", s, err2)
		}
		if c != c2 {
			t.Fatalf("Parse(Format(Parse(%q))) round-trip failed:\n  first  = %+v\n  second = %+v", s, c, c2)
		}

		// Property 3: Hash field, if present, must be 12 lowercase hex chars.
		if c.Hash != "" {
			if len(c.Hash) != 12 {
				t.Fatalf("Parse(%q): hash length = %d, want 12", s, len(c.Hash))
			}
			for _, ch := range c.Hash {
				if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
					t.Fatalf("Parse(%q): hash %q contains non-hex char %q", s, c.Hash, ch)
				}
			}
		}

		// Property 4: Start and End must be positive with Start <= End.
		if c.Start < 1 {
			t.Fatalf("Parse(%q): Start=%d < 1", s, c.Start)
		}
		if c.End < c.Start {
			t.Fatalf("Parse(%q): End=%d < Start=%d", s, c.End, c.Start)
		}

		// Property 5: File must not contain newlines or raw double-quotes.
		if strings.ContainsAny(c.File, "\n\r\"") {
			t.Fatalf("Parse(%q): File=%q contains invalid character", s, c.File)
		}
	})
}
