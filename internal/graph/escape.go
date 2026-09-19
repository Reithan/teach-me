package graph

import "strings"

// Escape encodes s for embedding in a Mermaid label field.
//
// The # character is escaped first so that the # characters later escapes
// introduce are not themselves re-escaped.  Raw newline characters (LF, CR,
// CRLF) are collapsed to a single space; this is intentionally lossy —
// Unescape(Escape(s)) == s only for inputs that do not contain raw newlines.
func Escape(s string) string {
	// Collapse newlines before any substitution.
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	// Escape # first; subsequent substitutions introduce # characters that
	// must not be treated as the start of an escape sequence.
	s = strings.ReplaceAll(s, "#", "#35;")
	s = strings.ReplaceAll(s, `"`, "#quot;")
	s = strings.ReplaceAll(s, "'", "#39;")
	s = strings.ReplaceAll(s, "<", "#lt;")
	s = strings.ReplaceAll(s, ">", "#gt;")
	return s
}

// Unescape decodes a Mermaid label field.
//
// The #35; sequence is decoded last so that encoded # characters do not
// accidentally match #quot; or other sequences.
//
// Unescape(Escape(s)) == s for any s that contains no raw newline
// characters; newline collapsing in Escape is intentionally lossy.
func Unescape(s string) string {
	s = strings.ReplaceAll(s, "#quot;", `"`)
	s = strings.ReplaceAll(s, "#39;", "'")
	s = strings.ReplaceAll(s, "#lt;", "<")
	s = strings.ReplaceAll(s, "#gt;", ">")
	// Decode # last.
	s = strings.ReplaceAll(s, "#35;", "#")
	return s
}
