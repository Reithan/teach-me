package cli

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/source"
	"github.com/reithan/teach-me/internal/state"
)

// srcRun is the Run handler for:
//
//	tm src <locator> [START-END] [--find <regex>]
//
// Resolves a source locator through the same resolver pipeline as tm add
// (src-root, repo aliases, converters, cache, aids-dir refusal) and prints the
// converted text with 1-based line numbers in the form:
//
//	N\t<text>
//
// An optional START-END positional restricts output to that inclusive range.
// --find <regex> further filters to lines matching the Go RE2 pattern (original
// line numbers are preserved).
//
// A git: locator prints a resolved-locator header first:
//
//	src: <resolved locator>
//
// A URI locator prints a header when a redirect changed the URL:
//
//	src: <final URL>
//
// Plain paths print no header.
//
// Exit codes:
//
//	0  ok
//	1  resolver refusal (aids-dir, missing file, fetch failure, unknown alias, …)
//	3  usage error (bad range, bad --find regex, bad locator form)
func srcRun(ctx *Context) int {
	locator := ctx.Positionals[0]

	// Locators must not contain a hash@ prefix or :start-end range suffix;
	// tm src operates on the whole file, with range as a separate positional.
	if !cite.IsGit(locator) && strings.Contains(locator, "@") && len(locator) > 13 && locator[12] == '@' {
		// Looks like it has a hash prefix — refuse with a usage hint.
		ctx.ErrMsg = "locator must not include a hash prefix"
		ctx.FixMsg = FindCommand("src").Usage()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}

	// Validate: no :N-M range embedded in the locator itself.
	// For plain paths, git:, and URIs the locator is accepted as-is.

	// Resolve graph file for GraphDir (aids-dir refusal) and src-root.
	// If no graph file is configured, proceed with an empty GraphDir —
	// the resolver skips the aids-dir check when GraphDir is "".
	var graphDir string
	if file, fileErr := state.ResolveFile(ctx.FileFlag); fileErr == nil {
		graphDir = filepath.Dir(file)
		ctx.GraphFile = file
	}

	var r *source.Resolver
	var resolverErr error
	if graphDir != "" {
		r, resolverErr = source.NewResolver(graphDir)
	} else {
		cfg, cfgErr := source.LoadConfig()
		if cfgErr != nil {
			ctx.ErrMsg = fmt.Sprintf("source config: %v", cfgErr)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
			return 3
		}
		r = source.NewResolverWithConfig(cfg, "")
	}
	if resolverErr != nil {
		ctx.ErrMsg = fmt.Sprintf("source config: %v", resolverErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Fetch full converted text.
	text, meta, readErr := r.ReadAll(locator)
	if readErr != nil {
		return citeHashError(ctx, locator, readErr, FindCommand("src").Usage())
	}

	// Split into lines (ReadAll already normalises CRLF and trailing newline).
	allLines := strings.Split(text, "\n")
	totalLines := len(allLines)

	// Parse optional START-END range.
	start := 1
	end := totalLines
	if len(ctx.Positionals) >= 2 {
		rangeStr := ctx.Positionals[1]
		s, e, parseErr := parseLineRange(rangeStr)
		if parseErr != nil {
			ctx.ErrMsg = parseErr.Error()
			ctx.FixMsg = FindCommand("src").Usage()
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 3
		}
		if s < 1 || e < s {
			ctx.ErrMsg = "line range must be START-END with 1 <= START <= END"
			ctx.FixMsg = FindCommand("src").Usage()
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 3
		}
		if e > totalLines {
			ctx.ErrMsg = fmt.Sprintf("lines %d-%d out of bounds (file has %d lines)", s, e, totalLines)
			ctx.FixMsg = FindCommand("src").Usage()
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 3
		}
		start = s
		end = e
	}

	// Parse optional --find regex.
	var findRE *regexp.Regexp
	if findVals := ctx.Flags["find"]; len(findVals) > 0 {
		pat := findVals[0]
		var reErr error
		findRE, reErr = regexp.Compile(pat)
		if reErr != nil {
			ctx.ErrMsg = fmt.Sprintf("bad --find: %v", reErr)
			ctx.FixMsg = FindCommand("src").Usage()
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 3
		}
	}

	// Print header for git: (always resolved locator) or URI with redirect.
	if cite.IsGit(locator) && meta.ResolvedLocator != "" {
		_, _ = fmt.Fprintf(ctx.Out, "src: %s\n", meta.ResolvedLocator)
	} else if cite.IsURI(locator) && meta.URL != "" && meta.URL != locator {
		_, _ = fmt.Fprintf(ctx.Out, "src: %s\n", meta.URL)
	}

	// Print numbered lines (1-indexed, within the range, filtered by --find).
	for i := start; i <= end; i++ {
		line := allLines[i-1]
		if findRE != nil && !findRE.MatchString(line) {
			continue
		}
		_, _ = fmt.Fprintf(ctx.Out, "%d\t%s\n", i, line)
	}

	return 0
}

// parseLineRange parses a "START-END" string and returns (start, end, error).
// Returns an error if the format is not N-M or if values are non-positive integers.
func parseLineRange(s string) (int, int, error) {
	dashIdx := strings.Index(s, "-")
	if dashIdx < 0 {
		return 0, 0, fmt.Errorf("range must be START-END (e.g. 3-7), got %q", s)
	}
	startStr := s[:dashIdx]
	endStr := s[dashIdx+1:]
	if startStr == "" || endStr == "" {
		return 0, 0, fmt.Errorf("range must be START-END (e.g. 3-7), got %q", s)
	}
	start, errS := strconv.Atoi(startStr)
	if errS != nil {
		return 0, 0, fmt.Errorf("range must be START-END (e.g. 3-7), got %q", s)
	}
	end, errE := strconv.Atoi(endStr)
	if errE != nil {
		return 0, 0, fmt.Errorf("range must be START-END (e.g. 3-7), got %q", s)
	}
	return start, end, nil
}
