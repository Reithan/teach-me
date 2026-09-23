// Package cite parses, resolves, and reads citation ranges as described in
// spec sections 4.4, 9, and 11.
package cite

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Citation is a parsed source citation consisting of a locator and an
// inclusive line range [Start, End].
//
// Hash is the first 12 lowercase hex characters of the SHA-256 of the
// normalized cited text; it is empty for legacy hashless citations.
//
// File holds the locator: a path relative to TM_SRC_ROOT, an absolute path
// (/... or a Windows drive letter), or a URI with a scheme (https://...).
type Citation struct {
	Hash  string // 12 lowercase hex chars; empty if hashless (legacy)
	File  string // locator: relative path, absolute path, or URI
	Start int
	End   int
}

// uriScheme matches a leading URI scheme per RFC 3986: letter followed by
// letters, digits, +, -, or ., then ://
var uriScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+\-.]*://`)

// IsURI reports whether the locator has a URI scheme (e.g. https://).
func IsURI(locator string) bool {
	return uriScheme.MatchString(locator)
}

// IsAbsPath reports whether the locator is an absolute filesystem path:
// either starting with / (Unix) or a Windows drive letter (C:\ or C:/).
func IsAbsPath(locator string) bool {
	if len(locator) == 0 {
		return false
	}
	if locator[0] == '/' {
		return true
	}
	// Windows drive letter: X:\ or X:/
	if len(locator) >= 3 && locator[1] == ':' && (locator[2] == '\\' || locator[2] == '/') {
		c := locator[0]
		return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}
	return false
}

// isLowerHex12 reports whether s is exactly 12 lowercase hexadecimal characters.
func isLowerHex12(s string) bool {
	if len(s) != 12 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// Normalize normalizes text for hashing per spec section 3.2:
// CRLF to LF, trailing whitespace stripped per line, lines LF-joined,
// no trailing newline. Internal whitespace is preserved.
func Normalize(text string) string {
	// CRLF → LF
	s := strings.ReplaceAll(text, "\r\n", "\n")
	// Strip trailing whitespace per line
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	// Join with LF, drop trailing empty lines and newline
	result := strings.Join(lines, "\n")
	return strings.TrimRight(result, "\n")
}

// Hash returns the first 12 lowercase hex characters of the SHA-256 of the
// normalized text. The caller should pass Normalize(text) for citation hashing.
func Hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:12]
}

// Format formats a citation back to its canonical string representation.
// If the citation has a hash, the output is "hash@locator:START-END".
// Without a hash, the output is the legacy "locator:START-END" form.
func Format(c Citation) string {
	if c.Hash != "" {
		return fmt.Sprintf("%s@%s:%d-%d", c.Hash, c.File, c.Start, c.End)
	}
	return fmt.Sprintf("%s:%d-%d", c.File, c.Start, c.End)
}

// Parse parses a citation string in one of two forms:
//
//   - New form: "<hash>@<locator>:START-END" where hash is exactly 12 lowercase
//     hex characters.
//   - Legacy form: "<locator>:START-END" (no hash).
//
// The locator may be a relative path, an absolute path (/... or C:\...), or a
// URI (https://...). A '"' character in the locator is rejected; use %22.
// The range is split on the LAST colon, so scheme separators, ports, and drive
// letters in the locator are harmless.
func Parse(s string) (Citation, error) {
	var hash string
	rest := s

	// Detect hash prefix: exactly 12 lowercase hex chars followed by '@'.
	if len(s) > 13 && s[12] == '@' && isLowerHex12(s[:12]) {
		hash = s[:12]
		rest = s[13:]
	}

	// Split rest on the LAST colon to separate locator from range.
	idx := strings.LastIndex(rest, ":")
	if idx < 0 {
		return Citation{}, fmt.Errorf("citation %q: missing line range", s)
	}

	locator := rest[:idx]
	rangeStr := rest[idx+1:]

	if locator == "" {
		return Citation{}, fmt.Errorf("citation %q: empty file", s)
	}
	if strings.ContainsRune(locator, '\n') {
		return Citation{}, fmt.Errorf("citation %q: file contains newline", s)
	}
	if strings.ContainsRune(locator, '"') {
		return Citation{}, fmt.Errorf("citation %q: locator contains raw \"; use %%22", s)
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

	return Citation{Hash: hash, File: locator, Start: start, End: end}, nil
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
//
// For relative locators, the path is joined with srcRoot.
// For absolute locators, the path is returned as-is (cleaned).
// For URI locators, the locator string is returned unchanged; the caller
// must not pass URI citations to os.ReadFile.
func Resolve(c Citation, srcRoot string) string {
	if IsURI(c.File) {
		return c.File
	}
	if IsAbsPath(c.File) {
		return filepath.Clean(filepath.FromSlash(c.File))
	}
	return filepath.Clean(filepath.Join(srcRoot, filepath.FromSlash(c.File)))
}

// ReadRange reads the lines [c.Start, c.End] inclusive from the file named
// by c under srcRoot, and returns them joined by newlines with no trailing
// newline.
//
// A single trailing newline at the end of the file is trimmed before line
// counting so that a file "a\nb\n" is treated as having 2 lines, not 3.
//
// Errors are returned when:
//   - The citation is a URI (M10 adds URI fetch support).
//   - The file cannot be read.
//   - Start < 1.
//   - End exceeds the file's line count.
func ReadRange(c Citation, srcRoot string) (string, error) {
	// M9: URI fetch not yet implemented; M10 adds this support.
	if IsURI(c.File) {
		return "", fmt.Errorf("citation %q: URI resolution is not yet supported (M10)", c.File)
	}

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

// HashCitation resolves a citation string, computes its content hash, and
// returns the canonical hashed form "<hash>@<locator>:START-END".
//
// Rules:
//   - If citeStr is already hashed and the hash matches the resolved text,
//     the original citeStr is returned unchanged.
//   - If citeStr is already hashed and the hash does NOT match, an error is
//     returned (hash mismatch).
//   - If citeStr is hashless (legacy form), the hash is computed and the
//     hashed form is returned.
//   - URI locators cannot be resolved in M9; they are accepted only when a
//     hash is already present and returned as-is. A URI without a hash is
//     refused. (M10 adds URI fetch support.)
func HashCitation(citeStr, srcRoot string) (string, error) {
	c, err := Parse(citeStr)
	if err != nil {
		return "", err
	}

	if IsURI(c.File) {
		// M9 temporary rule: URI locators cannot be fetched; accept only when
		// a hash is already supplied and store as-is. M10 will add fetch support.
		if c.Hash == "" {
			return "", fmt.Errorf("citation %q: URI locator requires a hash in M9; use <hash>@<uri>:START-END", citeStr)
		}
		return citeStr, nil
	}

	// Local (relative or absolute) citation: resolve, read, and hash.
	text, readErr := ReadRange(c, srcRoot)
	if readErr != nil {
		return "", readErr
	}

	computed := Hash(Normalize(text))

	if c.Hash != "" && c.Hash != computed {
		return "", fmt.Errorf(
			"citation %q: hash mismatch (stored %s, file hashes to %s)",
			citeStr, c.Hash, computed,
		)
	}

	c.Hash = computed
	return Format(c), nil
}

// CheckDrift resolves a local citation and reports whether the stored hash
// still matches the file content. Returns (drifted, error).
//
// drifted is false for URI citations (cannot resolve in M9) and for hashless
// citations (no hash to compare). A read error is returned as an error, not
// as drift.
func CheckDrift(citeStr, srcRoot string) (drifted bool, err error) {
	c, parseErr := Parse(citeStr)
	if parseErr != nil {
		return false, parseErr
	}
	if c.Hash == "" || IsURI(c.File) {
		return false, nil
	}

	text, readErr := ReadRange(c, srcRoot)
	if readErr != nil {
		return false, readErr
	}

	computed := Hash(Normalize(text))
	return computed != c.Hash, nil
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
