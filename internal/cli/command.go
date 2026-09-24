// Package cli implements the §16.4 declarative command table that drives
// argument parsing, usage-line generation, help output, and the TM_ROLE guard
// for the tm CLI. No argument-parsing library is used.
package cli

import (
	"io"
	"strings"
)

// PosArg describes one positional argument in a command's signature.
type PosArg struct {
	// Name is the display token used in the usage line, e.g. "<file>",
	// "pass|fail|unclear", or "[<file>]".
	Name string
	// Optional marks arguments wrapped in [] in the usage line. Optional
	// positionals must come after all required ones.
	Optional bool
	// Stdin marks arguments that accept "-" to read from stdin.
	Stdin bool
	// Values lists the allowed enum values. Empty means unrestricted.
	Values []string
}

// FlagSpec describes one optional flag in a command's signature.
type FlagSpec struct {
	// Name is the flag name without leading --, e.g. "kind", "re", "teach".
	Name string
	// TakesValue indicates that the flag expects a following value token.
	TakesValue bool
	// ValueName is the display label for the value, e.g. "concept|q|a", "<qid>".
	ValueName string
	// Values lists the allowed enum values for the flag. Empty means unrestricted.
	Values []string
	// Repeatable indicates the flag may appear more than once.
	Repeatable bool
}

// Context is the per-invocation runtime context passed to a command handler.
// It carries the dispatcher-validated arguments and serves as the
// communication channel between a handler and the centralized errlog logic.
type Context struct {
	// Positionals holds the parsed positional argument values, in order.
	Positionals []string
	// Flags holds parsed flag values. Boolean flags map to [""]. Repeatable
	// flags may hold multiple values.
	Flags map[string][]string
	// FileFlag holds the value of the global --file flag, if supplied.
	// The dispatcher extracts it from the raw args before per-command parsing
	// so handlers receive it without needing to declare it in their FlagSpec.
	// Priority per §3: --file > $TM_FILE > .tmconfig.
	FileFlag string
	// Out is the stdout writer for command output.
	Out io.Writer
	// ErrOut is the stderr writer for err:/fix: diagnostic lines.
	ErrOut io.Writer

	// The following fields are populated by handlers so the dispatcher can
	// build the centralized ERRORS.jsonl row (§10.1).

	// ErrMsg is the error text the handler printed (without the "err: " prefix).
	ErrMsg string
	// FixMsg is the fix text the handler printed (without the "fix: " prefix),
	// or "" when no fix line was emitted.
	FixMsg string
	// Violations is populated by the lint handler for the errlog row.
	Violations []string
	// GraphFile is set by the handler to the resolved graph file path.
	// The dispatcher uses it to derive the ERRORS.jsonl directory.
	GraphFile string
}

// Command is one row in the declarative command table. A single package-level
// Table slice holds every §6 command and drives the entire CLI.
type Command struct {
	Name string

	// PosArgs is the ordered list of positional arguments.
	PosArgs []PosArg

	// Flags is the list of optional flags.
	Flags []FlagSpec

	// ForbidGrader is true when TM_ROLE=grader must refuse this command (§7).
	// All commands except check, grade, and show set this to true.
	ForbidGrader bool

	// ForbidTeacher is true when TM_ROLE=teacher must refuse this command (§7).
	// Only grade sets this to true.
	ForbidTeacher bool

	// SkipFormatCheck is true when the command must not be refused for an
	// old-format graph. Set for migrate (upgrades the graph), lint (diagnoses
	// it), and new (creates the file). help/--version never reach the check.
	// load is left false so the loadRun handler can check the positional file.
	SkipFormatCheck bool

	// Run is the command handler. A nil Run means the command is declared with
	// full usage metadata but is not yet implemented in this build. The
	// dispatcher emits "err: <name> is not implemented in this build" (exit 3)
	// and logs the row. Later sub-PRs replace nil with real implementations.
	Run func(ctx *Context) int
}

// Usage returns the generated one-line usage string for c, for example:
//
//	tm q <concept> <cite> "<narrow scope>" [--re <qid>] [--teach]
//
// The string is generated from PosArgs and Flags; it is never hand-written
// prose (§1, §16.4).
func (c Command) Usage() string {
	var b strings.Builder
	b.WriteString("tm ")
	b.WriteString(c.Name)
	for _, p := range c.PosArgs {
		b.WriteByte(' ')
		b.WriteString(p.Name)
	}
	for _, f := range c.Flags {
		b.WriteString(" [--")
		b.WriteString(f.Name)
		if f.TakesValue {
			b.WriteByte(' ')
			b.WriteString(f.ValueName)
		}
		b.WriteByte(']')
	}
	return b.String()
}
