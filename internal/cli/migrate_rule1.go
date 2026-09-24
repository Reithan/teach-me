package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	icite "github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/source"
)

func init() {
	migrateRules = append(migrateRules, migrateRule1)
}

// migrateRule1 rewrites plain-path citations that have a stored commit SHA in
// the event log to the git: locator form (§13.2 rule 1).
//
// Returns ok=true on a successful rewrite. Returns ok=false with a non-empty
// reason to stop the pipeline (this rule owns the citation but cannot convert
// it). Returns ok=false with an empty reason when the citation is not a plain
// path (other rules may handle it).
func migrateRule1(mctx *migrateContext, ref citationRef, _ *graph.Graph) (newLocator, reason string, ok bool) {
	c, err := icite.Parse(ref.cite)
	if err != nil {
		return "", "", false
	}
	// Only plain paths; git: and URI locators pass to other rules (empty reason).
	if icite.IsGit(c.File) || icite.IsURI(c.File) {
		return "", "", false
	}

	// Look up the commit from the most-recent add/q event for this id.
	ev := mctx.eventForID(ref.id)
	if ev == nil {
		return "", "no event found for id", false
	}
	commit, _ := ev["commit"].(string)
	if commit == "" {
		return "", "no commit in event log", false
	}

	// Compute the absolute path the same way readPath does: via cite.Resolve.
	absPath := icite.Resolve(c, mctx.resolver.SrcRoot)
	if !filepath.IsAbs(absPath) {
		var absErr error
		absPath, absErr = filepath.Abs(absPath)
		if absErr != nil {
			return "", fmt.Sprintf("cannot resolve path: %v", absErr), false
		}
	}

	// Find the registered alias whose repo path is the longest prefix of absPath.
	repos := mctx.resolver.Cfg.Repos
	aliases := make([]string, 0, len(repos))
	for a := range repos {
		aliases = append(aliases, a)
	}
	sort.Strings(aliases) // deterministic tie-breaking

	bestAlias := ""
	bestPrefix := ""
	for _, alias := range aliases {
		repoPath := repos[alias]
		if !filepath.IsAbs(repoPath) {
			if abs2, abs2Err := filepath.Abs(repoPath); abs2Err == nil {
				repoPath = abs2
			}
		}
		// Add trailing separator so /foo/bar does not match /foo/barbaz.
		normalized := repoPath
		if !strings.HasSuffix(normalized, string(filepath.Separator)) {
			normalized += string(filepath.Separator)
		}
		if strings.HasPrefix(absPath, normalized) && len(normalized) > len(bestPrefix) {
			bestAlias = alias
			bestPrefix = normalized
		}
	}

	if bestAlias == "" {
		// No alias covers this path. Hint the user at the repo root.
		repoDir, _ := source.FindGitRepo(absPath)
		if repoDir == "" {
			repoDir = "<repo path>"
		}
		return "", fmt.Sprintf("needs: tm repo add <alias> %s", repoDir), false
	}

	// Build the repo-relative path.
	repoAbsPath := repos[bestAlias]
	if !filepath.IsAbs(repoAbsPath) {
		if abs2, abs2Err := filepath.Abs(repoAbsPath); abs2Err == nil {
			repoAbsPath = abs2
		}
	}
	relPath, relErr := filepath.Rel(repoAbsPath, absPath)
	if relErr != nil {
		return "", fmt.Sprintf("cannot compute relative path: %v", relErr), false
	}
	relPath = filepath.ToSlash(relPath)

	// Build the candidate git: citation string and hash it.
	// HashCitation reads the blob via git and returns the SHA-pinned form.
	gitLocator := fmt.Sprintf("git:%s@%s:%s", bestAlias, commit, relPath)
	newCiteStr := fmt.Sprintf("%s:%d-%d", gitLocator, c.Start, c.End)

	hashedCite, _, hashErr := mctx.resolver.HashCitation(newCiteStr)
	if hashErr != nil {
		return "", fmt.Sprintf("cannot hash git: form: %v", hashErr), false
	}

	// Verify content hash matches the original stored hash.
	if c.Hash != "" {
		newC, parseErr := icite.Parse(hashedCite)
		if parseErr != nil {
			return "", fmt.Sprintf("internal parse after hash: %v", parseErr), false
		}
		if newC.Hash != c.Hash {
			return "", fmt.Sprintf("hash mismatch: stored %s, git: form %s", c.Hash, newC.Hash), false
		}
	}

	return hashedCite, "git: locator", true
}
