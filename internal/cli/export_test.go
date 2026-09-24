package cli

import "github.com/reithan/teach-me/internal/graph"

// BuildEventForID exports the unexported buildEventForID for use by external
// test packages (migrate_test.go). Only compiled into the test binary.
var BuildEventForID = buildEventForID

// SetMigrateRulesForTest replaces the migrateRules pipeline with stubs for
// testing. Each stub receives (id, cite string) and returns (newLocator,
// reason string, ok bool). The returned func restores the original pipeline
// and must be passed to t.Cleanup.
func SetMigrateRulesForTest(stubs []func(id, cite string) (string, string, bool)) func() {
	saved := migrateRules
	rules := make([]migrateRule, len(stubs))
	for i, fn := range stubs {
		fn := fn // capture loop variable
		rules[i] = func(_ *migrateContext, ref citationRef, _ *graph.Graph) (string, string, bool) {
			return fn(ref.id, ref.cite)
		}
	}
	migrateRules = rules
	return func() { migrateRules = saved }
}
