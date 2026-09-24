package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/source"
	"github.com/reithan/teach-me/internal/state"
)

// migrateContext carries per-run dependencies that migration rules need to
// convert citations. It is built once in migrateRun and passed to every rule.
type migrateContext struct {
	// file is the absolute path of the graph file being migrated.
	file string
	// dir is filepath.Dir(file).
	dir string
	// resolver is built the same way add.go builds it: source.NewResolver(dir).
	resolver *source.Resolver
	// eventForID returns the most-recent "add" or "q" event recorded for the
	// given concept/question ID in the graph's event log (<file>.jsonl). Rules
	// use it to reconstruct the original source info for a citation.
	// Returns nil when no matching event exists.
	eventForID func(id string) map[string]any
}

// migrateRule is a function that tries to convert a citation string to the
// format-2 locator form. id is the concept or question ID that owns the cite.
// It returns the new locator and a short reason when ok is true, or a reason
// string explaining why no conversion was done when ok is false. Later PRs
// append rules to the global migrateRules slice.
type migrateRule func(mctx *migrateContext, id, cite string, g *graph.Graph) (newLocator, reason string, ok bool)

// migrateRules is the ordered pipeline applied to each citation. PR 1 ships
// no rules; later PRs append entries here so only the new file needs to change.
var migrateRules []migrateRule

// buildMigrateContext initialises a migrateContext for the given graph file.
// Returns an error only when the source resolver cannot be constructed.
func buildMigrateContext(file string) (*migrateContext, error) {
	dir := filepath.Dir(file)
	resolver, err := source.NewResolver(dir)
	if err != nil {
		return nil, err
	}
	return &migrateContext{
		file:       file,
		dir:        dir,
		resolver:   resolver,
		eventForID: buildEventForID(file + ".jsonl"),
	}, nil
}

// buildEventForID reads the event log at logPath and returns a lookup function
// that maps a concept/question ID to its most-recent "add" or "q" event.
// A missing log file is silently ignored; the lookup function returns nil.
func buildEventForID(logPath string) func(id string) map[string]any {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return func(string) map[string]any { return nil }
	}
	index := make(map[string]map[string]any)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]any
		if jsonErr := json.Unmarshal([]byte(line), &ev); jsonErr != nil {
			continue
		}
		evType, _ := ev["ev"].(string)
		if evType != "add" && evType != "q" {
			continue
		}
		var id string
		if evType == "add" {
			id, _ = ev["id"].(string)
		} else {
			id, _ = ev["q"].(string)
		}
		if id == "" {
			continue
		}
		index[id] = ev // later events overwrite earlier ones; last wins
	}
	return func(id string) map[string]any { return index[id] }
}

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

	// Refuse if the graph is at or above the current format.
	if n := g.FormatN(); n >= graph.CurrentFormat {
		base := filepath.Base(file)
		if n == graph.CurrentFormat {
			ctx.ErrMsg = fmt.Sprintf("%s is already format %d", base, graph.CurrentFormat)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		} else {
			ctx.ErrMsg = fmt.Sprintf("%s is format %d, this is tm format %d", base, n, graph.CurrentFormat)
			ctx.FixMsg = "upgrade tm"
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		}
		return 1
	}

	fromN := g.FormatN()

	// Build the migration context (resolver + event lookup) for rules.
	mctx, mctxErr := buildMigrateContext(file)
	if mctxErr != nil {
		ctx.ErrMsg = fmt.Sprintf("source config: %v", mctxErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Gather all citations (concepts in all blocks, questions in testing).
	refs := gatherCitations(g)

	// Run the rule pipeline over each citation.
	results := runMigrateRules(mctx, refs, g)

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
// When no rule converts a citation, the last non-empty rule reason is used; if
// no rule even attempted the citation the reason is "plain path".
func runMigrateRules(mctx *migrateContext, refs []citationRef, g *graph.Graph) []migrateResult {
	results := make([]migrateResult, 0, len(refs))
	for _, ref := range refs {
		converted := false
		lastReason := "plain path"
		for _, rule := range migrateRules {
			newLoc, reason, ok := rule(mctx, ref.id, ref.cite, g)
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
			if reason != "" {
				lastReason = reason
			}
		}
		if !converted {
			results = append(results, migrateResult{
				id:        ref.id,
				oldCite:   ref.cite,
				reason:    lastReason,
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
