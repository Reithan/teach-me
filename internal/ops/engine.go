// Package ops implements the shared mutation engine used by every graph-mutating
// command (add, link, edit, drop, gap, reopen, …).
//
// Mutate is the sole entry point. It serialises mutations through a lock,
// lints the graph before and after applying the caller-supplied Apply function,
// writes atomically, and appends event rows to the event log.
//
// No CLI command is wired in this file. Command implementations live in
// separate files in this package (one per command), each supplying an Apply
// function to Mutate.
package ops

import (
	"fmt"
	"os"
	"strings"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/lint"
	"github.com/reithan/teach-me/internal/lockfile"
	"github.com/reithan/teach-me/internal/state"
)

// Refusal is a structured refusal returned when a mutation cannot proceed due
// to invariant violations. Exit is 1 for invariant / pre-condition failures and
// 3 for usage errors; 2 is reserved for output-lint failures (also a Refusal).
type Refusal struct {
	Err  string
	Fix  string
	Exit int
}

// Error implements the error interface so Refusal can be treated as an error
// when convenient.
func (r *Refusal) Error() string { return r.Err }

// Apply is a command-specific mutation. It receives the parsed current graph g
// and the derived state s, and returns:
//   - newG: the mutated graph to write (ignored when refusal != nil)
//   - rows: event-log rows to append after a successful write
//   - refusal: non-nil when the mutation is refused; no file is written
type Apply func(g *graph.Graph, s *state.State) (newG *graph.Graph, rows []eventlog.Row, refusal *Refusal)

// Mutate executes a guarded mutation of graphFile in the following order:
//
//  1. Acquire the advisory lock (lockfile.Acquire). Failure → error.
//  2. Defer lock release.
//  3. Read the current graph bytes from disk.
//  4. Lint the current bytes (lint.Check). Violations → Refusal{Exit:1}.
//  5. Load state (state.Load); graph is obtained via s.Graph().
//  6. Call apply(g, s). Non-nil refusal → return it unchanged; no write.
//  7. Serialize the new graph (graph.Write).
//  8. Lint the serialized bytes. Violations → Refusal{Exit:2} (bug guard).
//  9. Atomic write: lockfile.WriteTempAndRename.
//  10. Append event rows to the event log (best-effort; errors are swallowed).
//  11. Return rows, nil refusal, nil error.
func Mutate(
	graphFile string,
	stateCfg state.Config,
	lintCfg lint.Config,
	clk errlog.Clock,
	apply Apply,
) (rows []eventlog.Row, refusal *Refusal, err error) {
	// 1. Acquire the lock.
	lk, err := lockfile.Acquire(graphFile, clk)
	if err != nil {
		return nil, nil, fmt.Errorf("ops: %w", err)
	}

	// 2. Defer release (best-effort; ignore error at defer site).
	defer func() { _ = lk.Release() }()

	// 3. Read current graph bytes.
	data, err := os.ReadFile(graphFile)
	if err != nil {
		return nil, nil, fmt.Errorf("ops: read graph: %w", err)
	}

	// 4. Lint the current on-disk graph.
	if viols := lint.Check(data, lintCfg); len(viols) > 0 {
		msgs := make([]string, len(viols))
		for i, v := range viols {
			msgs[i] = v.Msg
		}
		return nil, &Refusal{
			Err:  "graph fails lint: " + strings.Join(msgs, "; "),
			Exit: 1,
		}, nil
	}

	// 5. Load state (also parses the graph internally).
	s, err := state.Load(graphFile, stateCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("ops: load state: %w", err)
	}
	g := s.Graph()

	// 6. Apply the command-specific mutation.
	newG, rows, refusal := apply(g, s)
	if refusal != nil {
		return nil, refusal, nil
	}

	// 7. Serialize the mutated graph.
	outBytes := graph.Write(newG)

	// 8. Lint the output bytes (bug guard — our writer must not emit invalid graphs).
	if viols := lint.Check(outBytes, lintCfg); len(viols) > 0 {
		msgs := make([]string, len(viols))
		for i, v := range viols {
			msgs[i] = v.Msg
		}
		return nil, &Refusal{
			Err:  "output fails lint (internal error): " + strings.Join(msgs, "; "),
			Exit: 2,
		}, nil
	}

	// 9. Atomic rename over the graph file.
	if err := lockfile.WriteTempAndRename(graphFile, outBytes); err != nil {
		return nil, nil, fmt.Errorf("ops: write graph: %w", err)
	}

	// 10. Append event rows to the event log (best-effort).
	if len(rows) > 0 {
		el := eventlog.New(eventlog.Path(graphFile), clk)
		for _, row := range rows {
			el.Append(row)
		}
	}

	// 11. Return.
	return rows, nil, nil
}
