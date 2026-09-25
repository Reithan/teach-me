package cite

import (
	"fmt"
	"regexp"
	"strings"
)

// GitLocator holds a parsed git: locator (§4.4).
//
// Forms:
//   - file at ref:   Alias + Ref + Path  (RefB empty)
//   - commit:        Alias + Ref only    (RefB, Path empty)
//   - diff:          Alias + Ref + RefB  (Path empty)
//   - diff for path: Alias + Ref + RefB + Path
type GitLocator struct {
	Alias string // matches aliasRe; never empty
	Ref   string // left side of ".." or the single ref; never empty
	RefB  string // right side of ".."; empty for file-at-ref and commit
	Path  string // percent-decoded file path; empty for commit and plain diff
}

// aliasRe is the accepted pattern for a repo alias.
var aliasRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// IsGit reports whether the locator has a "git:" prefix.
func IsGit(locator string) bool {
	return strings.HasPrefix(locator, "git:")
}

// ValidAlias reports whether s is a syntactically valid repo alias.
func ValidAlias(s string) bool {
	return s != "" && aliasRe.MatchString(s)
}

// ParseGit parses a git: locator. The :START-END range must have already been
// stripped by the outer Parse. Errors are returned for:
//   - missing "git:" prefix
//   - no "@" (missing alias/ref boundary)
//   - empty or non-matching alias
//   - empty ref, or empty side of ".."
//   - a raw ":" in the path (lint check 14: use %3A instead)
func ParseGit(locator string) (GitLocator, error) {
	if !strings.HasPrefix(locator, "git:") {
		return GitLocator{}, fmt.Errorf("git locator %q: missing git: prefix", locator)
	}
	rest := locator[len("git:"):]

	// Alias up to first "@".
	atIdx := strings.IndexByte(rest, '@')
	if atIdx < 0 {
		return GitLocator{}, fmt.Errorf("git locator %q: missing @ between alias and ref", locator)
	}
	alias := rest[:atIdx]
	afterAt := rest[atIdx+1:]

	if alias == "" {
		return GitLocator{}, fmt.Errorf("git locator %q: empty alias", locator)
	}
	if !aliasRe.MatchString(alias) {
		return GitLocator{}, fmt.Errorf("git locator %q: alias %q must match [A-Za-z0-9_-]+", locator, alias)
	}

	// Ref part up to first ":", remainder is the path.
	var refPart, rawPath string
	if colonIdx := strings.IndexByte(afterAt, ':'); colonIdx >= 0 {
		refPart = afterAt[:colonIdx]
		rawPath = afterAt[colonIdx+1:]
	} else {
		refPart = afterAt
	}

	// §11.14 – raw ":" cannot appear in the path; use %3A.
	if strings.ContainsRune(rawPath, ':') {
		return GitLocator{}, fmt.Errorf("raw : in git path; percent-encode : as %%3A")
	}

	// Percent-decode %3A → ":".
	decodedPath := strings.ReplaceAll(rawPath, "%3A", ":")

	// Parse ref part: split on ".." when present.
	var ref, refB string
	if idx := strings.Index(refPart, ".."); idx >= 0 {
		ref = refPart[:idx]
		refB = refPart[idx+2:]
		if ref == "" {
			return GitLocator{}, fmt.Errorf("git locator %q: empty left side of range", locator)
		}
		if refB == "" {
			return GitLocator{}, fmt.Errorf("git locator %q: empty right side of range", locator)
		}
	} else {
		ref = refPart
	}

	if ref == "" {
		return GitLocator{}, fmt.Errorf("git locator %q: empty ref", locator)
	}

	return GitLocator{
		Alias: alias,
		Ref:   ref,
		RefB:  refB,
		Path:  decodedPath,
	}, nil
}

// FormatGit re-encodes a GitLocator to a locator string.
// ":" in Path is percent-encoded as "%3A".
func FormatGit(gl GitLocator) string {
	path := strings.ReplaceAll(gl.Path, ":", "%3A")
	switch {
	case gl.RefB != "" && path != "":
		return "git:" + gl.Alias + "@" + gl.Ref + ".." + gl.RefB + ":" + path
	case gl.RefB != "":
		return "git:" + gl.Alias + "@" + gl.Ref + ".." + gl.RefB
	case path != "":
		return "git:" + gl.Alias + "@" + gl.Ref + ":" + path
	default:
		return "git:" + gl.Alias + "@" + gl.Ref
	}
}
