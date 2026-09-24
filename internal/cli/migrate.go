package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// migrateRule is a function that tries to convert a citation string to the
// format-2 locator form. It returns the new locator and a short reason when
// ok is true, or a reason string explaining why no conversion was done when
// ok is false. Later PRs append rules to the global migrateRules slice.
type migrateRule func(cite string, g *graph.Graph) (newLocator, reason string, ok bool)

// migrateRules is the ordered pipeline applied to each citation. PR 1 ships
// no rules; later PRs append entries here so only the new file needs to change.
var migrateRules []migrateRule

// citationRef identifies one citation within the graph.
type citationRef struct {
	// id is the concept or question ID that owns this citation.
	id string
	// cite is the raw citation string.
	cite string
}

// migrateResult records the outcome for one citation.
type migrateResult struct {
	id        string
	oldCite   string
	newCite   string
	reason    string
	rewritten bool
}

// migrateRun is the Run handler for `tm migrate [<file>] [--dry-run]`.
//
// Upgrades a format-1 graph to format 2 by running the migration rule pipeline
// over every citation, setting %% tm:format 2, and writing the result. With
// --dry-run the same output is printed but nothing is written.
//
// Exit codes:
//
//	0  ok (including when some citations are left; partial migration is valid)
//	1  invariant refusal (already format 2, role guard)
//	3  resolve / read / write error
func migrateRun(ctx *Context) int {
	// Resolve file: positional arg beats config, like lint (§3).
	var file string
	if len(ctx.Positionals) > 0 {
		file = ctx.Positionals[0]
	} else {
		var err error
		file, err = state.ResolveFile(ctx.FileFlag)
		if err != nil {
			ctx.ErrMsg = err.Error()
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
			return 3
		}
	}
	ctx.GraphFile = file

	_, dryRun := ctx.Flags["dry-run"]

	// Read and parse the current graph to check its format and gather citations.
	data, err := os.ReadFile(file)
	if err != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot read %s: %v", filepath.Base(file), err)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	g, err := graph.Parse(data)
	if err != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot parse %s: %v", filepath.Base(file), err)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Refuse if the graph is already at the current format.
	if g.FormatN() >= graph.CurrentFormat {
		ctx.ErrMsg = fmt.Sprintf("%s is already format %d", filepath.Base(file), graph.CurrentFormat)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 1
	}

	fromN := g.FormatN()

	// Gather all citations (concepts in all blocks, questions in testing).
	refs := gatherCitations(g)

	// Run the rule pipeline over each citation.
	results := runMigrateRules(refs, g)

	// Separate rewrites from lefts.
	var rewrites, lefts []migrateResult
	for _, r := range results {
		if r.rewritten {
			rewrites = append(rewrites, r)
		} else {
			lefts = append(lefts, r)
		}
	}

	// Build the output lines.
	var outLines []string
	for _, r := range rewrites {
		outLines = append(outLines, fmt.Sprintf("ok %s %s -> %s", r.id, r.oldCite, r.newCite))
	}
	for _, r := range lefts {
		outLines = append(outLines, fmt.Sprintf("left %s %s: %s", r.id, r.oldCite, r.reason))
	}
	summaryLine := fmt.Sprintf(
		"migrated %s format %d -> %d: %d rewritten, %d left",
		filepath.Base(file), fromN, graph.CurrentFormat, len(rewrites), len(lefts),
	)
	outLines = append(outLines, summaryLine)

	if dryRun {
		for _, line := range outLines {
			_, _ = fmt.Fprintln(ctx.Out, line)
		}
		return 0
	}

	// Build the left IDs list for the event log.
	leftIDs := make([]string, 0, len(lefts))
	seen := make(map[string]bool)
	for _, r := range lefts {
		if !seen[r.id] {
			leftIDs = append(leftIDs, r.id)
			seen[r.id] = true
		}
	}

	// Apply the migration via ops.Mutate so the lock, lint, and rename apply.
	// Capture the citation map for rewrites so the Apply can update the graph.
	rewriteMap := buildRewriteMap(results)
	stateCfg := state.ConfigFromEnv()
	lintCfg := buildLintConfig(file)

	_, refusal, engErr := ops.Mutate(file, stateCfg, lintCfg, errlog.RealClock, func(mg *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		// Apply citation rewrites.
		applyRewrites(mg, rewriteMap)
		// Set the format marker.
		mg.Format = &graph.FormatMeta{N: graph.CurrentFormat}
		// Build the event row.
		row := eventlog.NewRow("migrate", map[string]any{
			"from":       fromN,
			"to":         graph.CurrentFormat,
			"rewritten":  len(rewrites),
			"left":       len(lefts),
			"unresolved": leftIDs,
		})
		return mg, []eventlog.Row{row}, nil
	})
	if engErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot migrate %s: %v", filepath.Base(file), engErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	if refusal != nil {
		ctx.ErrMsg = refusal.Err
		ctx.FixMsg = refusal.Fix
		writeErrFix(ctx.ErrOut, refusal.Err, refusal.Fix)
		return refusal.Exit
	}

	// Print output after successful write.
	for _, line := range outLines {
		_, _ = fmt.Fprintln(ctx.Out, line)
	}
	return 0
}

// gatherCitations collects all citations from all concepts and questions in g.
func gatherCitations(g *graph.Graph) []citationRef {
	var refs []citationRef
	for _, c := range g.PassedConcepts {
		for _, cite := range c.Cites {
			refs = append(refs, citationRef{id: c.ID, cite: cite})
		}
	}
	for _, c := range g.UntestedConcepts {
		for _, cite := range c.Cites {
			refs = append(refs, citationRef{id: c.ID, cite: cite})
		}
	}
	for _, c := range g.ReserveConcepts {
		for _, cite := range c.Cites {
			refs = append(refs, citationRef{id: c.ID, cite: cite})
		}
	}
	for _, item := range g.TestingItems {
		if item.Q != nil && item.Q.Cite != "" {
			refs = append(refs, citationRef{id: item.Q.ID, cite: item.Q.Cite})
		}
	}
	return refs
}

// runMigrateRules applies each rule in migrateRules to every citation.
// When no rule converts a citation it is left with reason "plain path".
func runMigrateRules(refs []citationRef, g *graph.Graph) []migrateResult {
	results := make([]migrateResult, 0, len(refs))
	for _, ref := range refs {
		converted := false
		for _, rule := range migrateRules {
			newLoc, reason, ok := rule(ref.cite, g)
			if ok {
				results = append(results, migrateResult{
					id:        ref.id,
					oldCite:   ref.cite,
					newCite:   newLoc,
					reason:    reason,
					rewritten: true,
				})
				converted = true
				break
			}
		}
		if !converted {
			results = append(results, migrateResult{
				id:        ref.id,
				oldCite:   ref.cite,
				reason:    "plain path",
				rewritten: false,
			})
		}
	}
	return results
}

// buildRewriteMap returns a map from (id, oldCite) → newCite for all rewrites.
func buildRewriteMap(results []migrateResult) map[string]string {
	m := make(map[string]string, len(results))
	for _, r := range results {
		if r.rewritten {
			m[rewriteKey(r.id, r.oldCite)] = r.newCite
		}
	}
	return m
}

// rewriteKey is a stable map key for a (conceptID, cite) pair.
func rewriteKey(id, cite string) string {
	return id + "\x00" + cite
}

// applyRewrites updates every citation in g according to rewriteMap.
func applyRewrites(g *graph.Graph, rewriteMap map[string]string) {
	rewriteCites := func(id string, cites []string) []string {
		out := make([]string, len(cites))
		for i, c := range cites {
			if newC, ok := rewriteMap[rewriteKey(id, c)]; ok {
				out[i] = newC
			} else {
				out[i] = c
			}
		}
		return out
	}
	for _, c := range g.PassedConcepts {
		c.Cites = rewriteCites(c.ID, c.Cites)
	}
	for _, c := range g.UntestedConcepts {
		c.Cites = rewriteCites(c.ID, c.Cites)
	}
	for _, c := range g.ReserveConcepts {
		c.Cites = rewriteCites(c.ID, c.Cites)
	}
	for _, item := range g.TestingItems {
		if item.Q != nil && item.Q.Cite != "" {
			if newC, ok := rewriteMap[rewriteKey(item.Q.ID, item.Q.Cite)]; ok {
				item.Q.Cite = newC
			}
		}
	}
}
