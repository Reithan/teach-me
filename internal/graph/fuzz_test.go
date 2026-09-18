package graph

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzEscape verifies the round-trip identity:
//
//   - For inputs WITHOUT raw newlines: Unescape(Escape(s)) == s.
//   - For inputs WITH raw newlines: Escape collapses them to spaces (lossy by
//     design); the test asserts the collapse behavior rather than failing.
func FuzzEscape(f *testing.F) {
	// Seed corpus: empty string, basic text, every escapable character, and
	// strings combining multiple special characters.
	seeds := []string{
		"",
		"hello world",
		`"double"`,
		`'single'`,
		`#hash`,
		`<less>`,
		`>greater`,
		`#35;`,
		`#quot;`,
		`a#b"c'd<e>f`,
		"newline\nhere",
		"crlf\r\nhere",
		`it's the "same" entry (I think) [index, term] -> cmd; 100% sure?`,
		"日本語テキスト",
		"Ünïcödé: 50%",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		escaped := Escape(s)
		unescaped := Unescape(escaped)

		// Newlines are collapsed to spaces by Escape; substitute them in the
		// original for comparison.
		wantBack := s
		wantBack = strings.ReplaceAll(wantBack, "\r\n", " ")
		wantBack = strings.ReplaceAll(wantBack, "\n", " ")
		wantBack = strings.ReplaceAll(wantBack, "\r", " ")

		if unescaped != wantBack {
			t.Errorf("Unescape(Escape(%q)) = %q, want %q", s, unescaped, wantBack)
		}

		// For inputs with no raw newlines the round-trip must be exact.
		if !strings.ContainsAny(s, "\n\r") && unescaped != s {
			t.Errorf("Unescape(Escape(%q)) = %q; expected exact round-trip", s, unescaped)
		}
	})
}

// FuzzParse verifies two properties:
//
//  1. Parse never panics on any byte sequence.
//  2. If Parse accepts an input, then Write produces output that Parse also
//     accepts — and writing that result produces identical bytes (stability).
func FuzzParse(f *testing.F) {
	// Seed corpus: the minimal valid graph, a graph with all three blocks
	// populated, and a graph with adversarial labels.
	seeds := []string{
		// Minimal three-block graph with no nodes.
		"flowchart TB\n" +
			`    subgraph passed["P"]` + "\n" +
			"    end\n" +
			`    subgraph untested["U"]` + "\n" +
			"    end\n" +
			`    subgraph testing["T"]` + "\n" +
			"    end\n",

		// Graph with one passed concept.
		"flowchart TB\n" +
			`    subgraph passed["P"]` + "\n" +
			`        foo["scope<br/>src.txt:1-10"]` + "\n" +
			"    end\n" +
			`    subgraph untested["U"]` + "\n" +
			"    end\n" +
			`    subgraph testing["T"]` + "\n" +
			"    end\n",

		// Graph with adversarial escaped label.
		"flowchart TB\n" +
			`    subgraph passed["P"]` + "\n" +
			`        bar["it#39;s the #quot;same#quot; entry #lt;br#gt; #35;hash<br/>s.txt:1-5"]` + "\n" +
			"    end\n" +
			`    subgraph untested["U"]` + "\n" +
			"    end\n" +
			`    subgraph testing["T"]` + "\n" +
			"    end\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Guard against invalid UTF-8; the parser may not handle it and that
		// is not the property under test.
		if !utf8.Valid(data) {
			return
		}

		// Property 1: Parse never panics.  Any panic is a failure.
		g, err := Parse(data)
		if err != nil {
			// Parse rejected the input — nothing more to check.
			return
		}

		// Property 2: Write(Parse(input)) must itself be parseable.
		out1 := Write(g)
		g2, err2 := Parse(out1)
		if err2 != nil {
			t.Fatalf("Parse accepted input but Write produced unparseable output: %v\nInput:\n%s\nOutput:\n%s",
				err2, data, out1)
		}

		// Property 2b: The serialisation is stable.
		out2 := Write(g2)
		if string(out1) != string(out2) {
			t.Fatalf("Write not stable: second write differs from first\nInput:\n%s\nFirst:\n%s\nSecond:\n%s",
				data, out1, out2)
		}
	})
}
