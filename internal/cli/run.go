package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/state"
	"github.com/reithan/teach-me/internal/version"
)

// Run is the primary entry point for the tm CLI. It accepts os.Args[1:] and
// returns the process exit code. cmd/tm/main.go calls os.Exit(cli.Run(os.Args[1:])).
func Run(args []string) int {
	return RunWithWriters(args, os.Stdout, os.Stderr)
}

// RunWithWriters is the testable entry point. out receives command output
// (stdout); errOut receives err:/fix: diagnostic lines (stderr).
//
// Output routing policy:
//   - Requested help (bare tm/--help, specific --help, --version): stdout.
//   - err:/fix: diagnostics on bad input or invariant refusal: stderr.
//   - Baseline help shown alongside an unknown-subcommand error: stdout.
//
// Exit codes (§6):
//
//	0  ok
//	1  invariant refusal (includes role-guard refusal)
//	2  graph fails lint
//	3  usage error, unknown command, or not-yet-implemented command
func RunWithWriters(args []string, out, errOut io.Writer) int {
	// Role is read at the top so every logging path below can include it.
	role := os.Getenv("TM_ROLE")

	// ── No args: bare tm invocation → baseline help, exit 0 (same as --help).
	if len(args) == 0 {
		if mismatch := baselineHelp(out); mismatch != "" {
			appendErrLog(role, args, 0, mismatch, nil)
		}
		return 0
	}

	first := args[0]

	// ── --version / -version / version ──────────────────────────────────────
	if first == "--version" || first == "-version" || first == "version" {
		_, _ = fmt.Fprintln(out, version.Version())
		return 0
	}

	// ── Bare --help / -h (no command) → baseline help, exit 0 ───────────────
	if (first == "--help" || first == "-h") && len(args) == 1 {
		if mismatch := baselineHelp(out); mismatch != "" {
			appendErrLog(role, args, 0, mismatch, nil)
		}
		return 0
	}

	// ── --help --all: one usage line per command, whatever TM_DOC says ──────
	if (first == "--help" || first == "-h") && len(args) == 2 && args[1] == "--all" {
		commandList(out)
		return 0
	}

	// ── --help <command> [<flag>] ────────────────────────────────────────────
	if (first == "--help" || first == "-h") && len(args) >= 2 {
		cmd := findCommand(args[1])
		if cmd == nil {
			writeUnknown(errOut, args[1], args, role)
			if mismatch := baselineHelp(out); mismatch != "" {
				appendErrLog(role, args, 3, mismatch, nil)
			}
			return 3
		}
		flagArg := ""
		if len(args) >= 3 {
			flagArg = args[2]
		}
		specificHelp(cmd, flagArg, out)
		return 0
	}

	// ── Dispatch on first token as command name ──────────────────────────────
	cmd := findCommand(first)
	if cmd == nil {
		writeUnknown(errOut, first, args, role)
		if mismatch := baselineHelp(out); mismatch != "" {
			appendErrLog(role, args, 3, mismatch, nil)
		}
		return 3
	}

	remaining := args[1:]

	// ── <command> --help [<flag>] ────────────────────────────────────────────
	if len(remaining) > 0 && (remaining[0] == "--help" || remaining[0] == "-h") {
		flagArg := ""
		if len(remaining) >= 2 {
			flagArg = remaining[1]
		}
		specificHelp(cmd, flagArg, out)
		return 0
	}

	// ── Role guard (§7) ─────────────────────────────────────────────────────
	if code := checkRole(cmd, role, args, errOut); code != 0 {
		return code
	}

	// ── Global --file extraction (§3) ───────────────────────────────────────
	// Strip --file <value> from remaining before per-command parsing so that
	// individual commands do not need to declare it in their FlagSpec.
	var fileFlag string
	filtered := remaining[:0:len(remaining)]
	for i := 0; i < len(remaining); i++ {
		if remaining[i] == "--file" {
			if i+1 >= len(remaining) {
				errMsg := "--file requires a value"
				fixMsg := cmd.Usage()
				writeErrFix(errOut, errMsg, fixMsg)
				appendErrLog(role, args, 3, errMsg, &fixMsg)
				return 3
			}
			fileFlag = remaining[i+1]
			i++ // consume the value token
		} else {
			filtered = append(filtered, remaining[i])
		}
	}
	remaining = filtered

	// ── Structural arg parse ─────────────────────────────────────────────────
	pos, flags, parseErrMsg := parseArgs(cmd, remaining)
	if parseErrMsg != "" {
		fixMsg := cmd.Usage()
		writeErrFix(errOut, parseErrMsg, fixMsg)
		appendErrLog(role, args, 3, parseErrMsg, &fixMsg)
		return 3
	}

	// ── Nil-Run placeholder ──────────────────────────────────────────────────
	if cmd.Run == nil {
		errMsg := fmt.Sprintf("%s is not implemented in this build", cmd.Name)
		fixMsg := cmd.Usage()
		writeErrFix(errOut, errMsg, fixMsg)
		appendErrLog(role, args, 3, errMsg, &fixMsg)
		return 3
	}

	// ── Run the handler ──────────────────────────────────────────────────────
	ctx := &Context{
		Positionals: pos,
		Flags:       flags,
		FileFlag:    fileFlag,
		Out:         out,
		ErrOut:      errOut,
	}
	code := cmd.Run(ctx)

	// ── Centralized errlog: one row per failed command (§10.1, §1 line 13) ───
	if code != 0 {
		graphDir := ""
		if ctx.GraphFile != "" {
			graphDir = filepath.Dir(ctx.GraphFile)
		}
		var filePtr *string
		if ctx.GraphFile != "" {
			s := ctx.GraphFile
			filePtr = &s
		}
		var fixPtr *string
		if ctx.FixMsg != "" {
			s := ctx.FixMsg
			fixPtr = &s
		}
		appendErrLogFull(graphDir, role, filePtr, args, code, ctx.ErrMsg, fixPtr, ctx.Violations)
	}

	return code
}

// checkRole enforces the TM_ROLE guard (§7). Returns 0 when the command may
// run; 1 on refusal (exit 1 = invariant refusal).
func checkRole(cmd *Command, role string, argv []string, errOut io.Writer) int {
	if role == "" {
		return 0
	}
	var errMsg, fixMsg string
	switch {
	case cmd.ForbidGrader && role == "grader":
		errMsg = fmt.Sprintf("%s is not available when TM_ROLE=grader", cmd.Name)
		fixMsg = "unset TM_ROLE or run as TM_ROLE=teacher"
	case cmd.ForbidTeacher && role == "teacher":
		errMsg = fmt.Sprintf("%s is not available when TM_ROLE=teacher", cmd.Name)
		fixMsg = "unset TM_ROLE or run as TM_ROLE=grader"
	default:
		return 0
	}
	writeErrFix(errOut, errMsg, fixMsg)
	appendErrLog(role, argv, 1, errMsg, &fixMsg)
	return 1
}

// parseArgs parses the raw args for cmd after the command name has been
// stripped. It returns the ordered positional values, the flag map, and a
// non-empty error string when the args do not structurally match the command's
// declaration.
//
// Flag parsing rules:
//   - "--flag" for boolean flags (TakesValue=false): maps to [""]
//   - "--flag value" for value flags (TakesValue=true): maps to ["value"]
//   - Repeatable flags: values accumulate
//   - Unknown flags: error
//   - Enum flags: value must be in FlagSpec.Values
//
// Positional parsing rules:
//   - Required positionals: any non-flag token fills slots in order
//   - Optional positionals: filled when extra tokens remain
//   - Too few required: error
//   - Too many (beyond optional): error
//   - Enum positionals (PosArg.Values): value must be in set
func parseArgs(cmd *Command, args []string) (pos []string, flags map[string][]string, errMsg string) {
	flags = make(map[string][]string)

	// Build flag lookup map.
	flagMap := make(map[string]*FlagSpec, len(cmd.Flags))
	for i := range cmd.Flags {
		flagMap[cmd.Flags[i].Name] = &cmd.Flags[i]
	}

	// Separate flags from positionals, consuming values for TakesValue flags.
	var positionals []string
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if strings.HasPrefix(tok, "--") {
			name := tok[2:]
			spec, ok := flagMap[name]
			if !ok {
				return nil, nil, fmt.Sprintf("unknown flag --%s", name)
			}
			if spec.TakesValue {
				if i+1 >= len(args) {
					return nil, nil, fmt.Sprintf("--%s requires a value", name)
				}
				i++
				val := args[i]
				if len(spec.Values) > 0 && !contains(spec.Values, val) {
					return nil, nil, fmt.Sprintf("--%s must be one of: %s", name, strings.Join(spec.Values, "|"))
				}
				flags[name] = append(flags[name], val)
			} else {
				flags[name] = append(flags[name], "")
			}
		} else {
			positionals = append(positionals, tok)
		}
	}

	// Count required and optional positional slots.
	required := 0
	optional := 0
	for _, p := range cmd.PosArgs {
		if p.Optional {
			optional++
		} else {
			required++
		}
	}

	if len(positionals) < required {
		// Identify the first missing positional name.
		missing := cmd.PosArgs[len(positionals)].Name
		return nil, nil, fmt.Sprintf("missing argument %s", missing)
	}
	maxPos := required + optional
	if len(positionals) > maxPos {
		return nil, nil, fmt.Sprintf("unexpected argument %q", positionals[maxPos])
	}

	// Validate enum positionals.
	for i, val := range positionals {
		if i >= len(cmd.PosArgs) {
			break
		}
		spec := cmd.PosArgs[i]
		if len(spec.Values) > 0 && !contains(spec.Values, val) {
			return nil, nil, fmt.Sprintf("argument %d must be one of: %s", i+1, strings.Join(spec.Values, "|"))
		}
	}

	return positionals, flags, ""
}

// writeErrFix prints an err: line and, when fix is non-empty, a fix: line to w.
// Writing to stderr rarely fails; errors are intentionally discarded.
func writeErrFix(w io.Writer, errMsg, fixMsg string) {
	_, _ = fmt.Fprintf(w, "err: %s\n", errMsg)
	if fixMsg != "" {
		_, _ = fmt.Fprintf(w, "fix: %s\n", fixMsg)
	}
}

// writeUnknown prints the unknown-command error to errOut and logs one row.
func writeUnknown(errOut io.Writer, name string, argv []string, role string) {
	errMsg := fmt.Sprintf("unknown command %q", name)
	_, _ = fmt.Fprintf(errOut, "err: %s\n", errMsg)
	appendErrLog(role, argv, 3, errMsg, nil)
}

// appendErrLog logs one error row to ERRORS.jsonl for dispatcher-level errors
// (before a graph file is resolved). Violations is always nil at this point.
func appendErrLog(role string, argv []string, exit int, errMsg string, fix *string) {
	// Best effort: when a graph is configured, log beside it rather than in
	// the working directory (§3).
	graphDir := ""
	if file, err := state.ResolveFile(""); err == nil && file != "" {
		graphDir = filepath.Dir(file)
	}
	appendErrLogFull(graphDir, role, nil, argv, exit, errMsg, fix, nil)
}

// appendErrLogFull appends one row to ERRORS.jsonl. Logging is best-effort:
// failures are silently ignored (errlog.Logger already swallows them).
func appendErrLogFull(graphDir, role string, file *string, argv []string, exit int, errMsg string, fix *string, violations []string) {
	path := errlog.Path(graphDir)
	logger := errlog.New(path, errlog.RealClock)

	var rolePtr *string
	if role != "" {
		s := role
		rolePtr = &s
	}

	// Prepend "tm" to produce the full invocation.
	fullArgv := make([]string, 0, len(argv)+1)
	fullArgv = append(fullArgv, "tm")
	fullArgv = append(fullArgv, argv...)

	logger.Append(errlog.Row{
		Role:       rolePtr,
		File:       file,
		Argv:       fullArgv,
		Exit:       exit,
		Err:        errMsg,
		Fix:        fix,
		Violations: violations,
	})
}

// contains reports whether s is in the slice vals.
func contains(vals []string, s string) bool {
	for _, v := range vals {
		if v == s {
			return true
		}
	}
	return false
}
