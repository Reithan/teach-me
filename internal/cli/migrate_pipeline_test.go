package cli

import (
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// TestRunMigrateRules_PipelineSemantics verifies the two non-trivial exit
// paths of runMigrateRules using a stub rule injected at the front of the
// pipeline.
//
//   - A rule returning ok=false with an empty reason is treated as "not my
//     case" and the pipeline continues; with no other rule claiming the
//     citation the result is left with reason "plain path".
//   - A rule returning ok=false with a non-empty reason stops the pipeline;
//     the citation is left with exactly that reason.
func TestRunMigrateRules_PipelineSemantics(t *testing.T) {
	// Minimal context: stubs do not call eventForID or use the resolver.
	mctx := &migrateContext{
		eventForID: func(string) map[string]any { return nil },
	}
	ref := citationRef{id: "c1", cite: "abc123@doc.txt:1-3"}

	tests := []struct {
		name          string
		stub          migrateRule
		wantReason    string
		wantRewritten bool
	}{
		{
			name: "empty-reason stub: falls through to plain path",
			// Returning ok=false with an empty reason signals "not my case";
			// the next rule (if any) runs. With no claiming rule the
			// default reason "plain path" is applied.
			stub: func(_ *migrateContext, _ citationRef, _ *graph.Graph) (string, string, bool) {
				return "", "", false
			},
			wantReason:    "plain path",
			wantRewritten: false,
		},
		{
			name: "non-empty-reason stub: stops pipeline with that reason",
			// Returning ok=false with a non-empty reason means the rule owns
			// the citation but cannot convert it; the pipeline stops and the
			// reason is surfaced verbatim.
			stub: func(_ *migrateContext, _ citationRef, _ *graph.Graph) (string, string, bool) {
				return "", "needs: tm repo add myrepo /path/to/repo", false
			},
			wantReason:    "needs: tm repo add myrepo /path/to/repo",
			wantRewritten: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := migrateRules
			migrateRules = []migrateRule{tc.stub}
			t.Cleanup(func() { migrateRules = saved })

			results := runMigrateRules(mctx, []citationRef{ref}, nil)

			if len(results) != 1 {
				t.Fatalf("want 1 result, got %d", len(results))
			}
			r := results[0]
			if r.rewritten != tc.wantRewritten {
				t.Errorf("rewritten: want %v, got %v", tc.wantRewritten, r.rewritten)
			}
			if r.reason != tc.wantReason {
				t.Errorf("reason: want %q, got %q", tc.wantReason, r.reason)
			}
		})
	}
}
