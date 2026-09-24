package cli

import (
	"errors"
	"fmt"

	icite "github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/source"
)

func init() {
	migrateRules = append(migrateRules, migrateRule2)
}

// migrateRule2 rewrites plain-path citations whose most-recent add/q event
// logged a `url` field but no `commit` field (§13.2 rule 2).
//
// For each such citation it fetches the URL through the resolver (so the
// conversion cache applies), compares the computed content hash against the
// hash stored in the plain-path citation, and:
//   - equal hash   → ok=true, newLocator = hashed URL citation
//   - hash differs → ok=false, reason = "hash differs at <url>"
//   - fetch refused → ok=false, reason = refusal Err text
//   - no url in event → ok=false, reason = "" (pass to next rule)
func migrateRule2(mctx *migrateContext, ref citationRef, _ *graph.Graph) (newLocator, reason string, ok bool) {
	c, err := icite.Parse(ref.cite)
	if err != nil {
		return "", "", false
	}
	// Only plain paths; git: and URI locators are handled by other rules.
	if icite.IsGit(c.File) || icite.IsURI(c.File) {
		return "", "", false
	}

	// Require an event for this id.
	ev := mctx.eventForID(ref.id)
	if ev == nil {
		return "", "", false
	}
	// If the event has a commit, rule 1 should have handled it already.
	if commit, _ := ev["commit"].(string); commit != "" {
		return "", "", false
	}
	url, _ := ev["url"].(string)
	if url == "" {
		// No url in the event: not this rule's case.
		return "", "", false
	}

	// Build a URL citation covering the same range and hash it via the resolver.
	// The cache is consulted (and populated) by readURI inside HashCitation.
	urlCiteStr := fmt.Sprintf("%s:%d-%d", url, c.Start, c.End)
	hashedCite, _, hashErr := mctx.resolver.HashCitation(urlCiteStr)
	if hashErr != nil {
		var refusalErr *source.RefusalError
		if errors.As(hashErr, &refusalErr) {
			return "", refusalErr.Err, false
		}
		return "", fmt.Sprintf("cannot fetch %s: %v", url, hashErr), false
	}

	// When the plain-path citation carries a content hash, verify it matches
	// what the URL currently serves.  A mismatch means the saved copy and the
	// live URL have diverged; leave the citation in place with a clear reason.
	if c.Hash != "" {
		newC, parseErr := icite.Parse(hashedCite)
		if parseErr != nil {
			return "", fmt.Sprintf("internal parse after hash: %v", parseErr), false
		}
		if newC.Hash != c.Hash {
			return "", fmt.Sprintf("hash differs at %s", url), false
		}
	}

	return hashedCite, "saved copy", true
}
