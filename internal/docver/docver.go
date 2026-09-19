// Package docver implements the §16.4 TM_DOC frontmatter read and the
// doc-version check described in §1 (line 19 of the spec).
//
// The public API is minimal and I/O-free:
//
//	DocVersion(data []byte) (string, bool)  – extract tm-version from frontmatter
//	Marker() string                         – CLI's own major.minor (e.g. "0.1")
//
// The CLI reads the file and passes []byte; that keeps docver pure and
// trivially testable without mocks.
package docver

import (
	"bytes"
	"strings"

	"github.com/reithan/teach-me/internal/version"
)

// DocVersion scans the YAML frontmatter of data and returns the value of
// metadata.tm-version and true. It returns ("", false) when:
//   - there is no frontmatter block (first non-empty line is not exactly "---")
//   - the frontmatter block is never closed
//   - there is no top-level metadata: key
//   - there is no tm-version: key indented under metadata:
//
// Values may be double-quoted, single-quoted, or unquoted; quotes are stripped.
// CRLF line endings are accepted.
func DocVersion(data []byte) (string, bool) {
	lines := splitLines(data)

	// The opening "---" must be the first non-empty line.
	start := -1
	for i, l := range lines {
		if l == "" {
			continue
		}
		if l == "---" {
			start = i
		}
		break
	}
	if start < 0 {
		return "", false
	}

	// Find the closing "---".
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", false
	}

	// Scan the frontmatter body for metadata: then an indented tm-version:.
	inMetadata := false
	for i := start + 1; i < end; i++ {
		line := lines[i]

		if line == "metadata:" {
			inMetadata = true
			continue
		}

		// A new zero-indentation key resets the metadata context.
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			inMetadata = false
			continue
		}

		if inMetadata {
			trimmed := strings.TrimLeft(line, " \t")
			const prefix = "tm-version:"
			if strings.HasPrefix(trimmed, prefix) {
				val := strings.TrimSpace(trimmed[len(prefix):])
				val = unquote(val)
				return val, true
			}
		}
	}

	return "", false
}

// Marker returns the CLI's own major.minor derived from version.Version().
// For example, "0.1.0" becomes "0.1".
func Marker() string {
	v := version.Version()
	dot1 := strings.Index(v, ".")
	if dot1 < 0 {
		return v
	}
	dot2 := strings.Index(v[dot1+1:], ".")
	if dot2 < 0 {
		return v
	}
	return v[:dot1+1+dot2]
}

// splitLines splits data on LF, stripping trailing CR from each line.
func splitLines(data []byte) []string {
	raw := bytes.Split(data, []byte("\n"))
	out := make([]string, len(raw))
	for i, b := range raw {
		out[i] = string(bytes.TrimRight(b, "\r"))
	}
	return out
}

// unquote strips a surrounding "..." or '...' pair from s, if present.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
