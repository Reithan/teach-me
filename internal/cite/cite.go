// Package cite parses, resolves, and reads citation ranges as described in
// spec sections 4.4, 9, and 11.
package cite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Citation is a parsed source citation consisting of a file path and an
// inclusive line range [Start, End].
type Citation struct {
	File  string
	Start int
	End   int
}

// Parse parses a citation string of the form "file:START-END".
//
// The file part is everything before the last colon, so relative paths
// containing slashes are accepted (e.g. "src/raft.txt:1-2"). A citation
// without a colon, with an empty file part, with a newline in the file part,
// without a dash separator in the range, or with non-positive or
// out-of-order line numbers is an error.
func Parse(s string) (Citation, error) {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return Citation{}, fmt.Errorf("citation %q: missing line range", s)
	}

	file := s[:idx]
	rangeStr := s[idx+1:]

	if file == "" {
		return Citation{}, fmt.Errorf("citation %q: empty file", s)
	}
	if strings.ContainsRune(file, '\n') {
		return Citation{}, fmt.Errorf("citation %q: file contains newline", s)
	}

	dashIdx := strings.Index(rangeStr, "-")
	if dashIdx < 0 {
		return Citation{}, fmt.Errorf("citation %q: line range must be START-END", s)
	}

	startStr := rangeStr[:dashIdx]
	endStr := rangeStr[dashIdx+1:]

	if startStr == "" {
		return Citation{}, fmt.Errorf("citation %q: line range must be START-END", s)
	}
	if endStr == "" {
		return Citation{}, fmt.Errorf("citation %q: line range must be START-END", s)
	}
	if !isDigits(startStr) {
		return Citation{}, fmt.Errorf("citation %q: line range must be START-END", s)
	}
	if !isDigits(endStr) {
		return Citation{}, fmt.Errorf("citation %q: line range must be START-END", s)
	}

	start := parseDigits(startStr)
	end := parseDigits(endStr)

	if start < 1 {
		return Citation{}, fmt.Errorf("citation %q: line numbers must be positive", s)
	}
	if end < start {
		return Citation{}, fmt.Errorf("citation %q: end line %d precedes start line %d", s, end, start)
	}

	return Citation{File: file, Start: start, End: end}, nil
}

// SrcRoot returns the source root directory for resolving citations. It
// returns $TM_SRC_ROOT if the environment variable is set and non-empty,
// otherwise it returns graphDir.
func SrcRoot(graphDir string) string {
	if root := os.Getenv("TM_SRC_ROOT"); root != "" {
		return root
	}
	return graphDir
}

// Resolve returns the on-disk path for citation c under srcRoot.
func Resolve(c Citation, srcRoot string) string {
	return filepath.Clean(filepath.Join(srcRoot, filepath.FromSlash(c.File)))
}

// ReadRange reads the lines [c.Start, c.End] inclusive from the file named
// by c under srcRoot, and returns them joined by newlines with no trailing
// newline.
//
// A single trailing newline at the end of the file is trimmed before line
// counting so that a file "a\nb\n" is treated as having 2 lines, not 3.
//
// Errors are returned when the file cannot be read, when Start < 1, or when
// End exceeds the file's line count.
func ReadRange(c Citation, srcRoot string) (string, error) {
	path := Resolve(c, srcRoot)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("citation %q: %w", c.File, err)
	}

	content := strings.TrimSuffix(string(raw), "\n")
	lines := strings.Split(content, "\n")

	if c.Start < 1 {
		return "", fmt.Errorf("citation %q: line numbers must be positive", c.File)
	}
	if c.End > len(lines) {
		return "", fmt.Errorf("citation %q: lines %d-%d out of bounds (file has %d lines)",
			c.File, c.Start, c.End, len(lines))
	}

	return strings.Join(lines[c.Start-1:c.End], "\n"), nil
}

func isDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseDigits(s string) int {
	n := 0
	for i := range len(s) {
		n = n*10 + int(s[i]-'0')
	}
	return n
}
