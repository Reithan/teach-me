package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reithan/teach-me/internal/config"
	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/lint"
)

// newRun is the Run handler for
// `tm new <file> [--title "<t>"] [--src-root <dir>] [--local]`.
//
// Creates a skeleton graph file and makes it active by writing the `file`
// pointer (and `src-root` when given) into the user config, or into .tmconfig
// in the working directory with --local (§3). Output on success: "ok\n".
//
// Exit codes:
//
//	0  ok
//	1  file already exists (invariant refusal)
//	2  skeleton fails lint (internal error — the skeleton must always pass)
//	3  file or config write error
func newRun(ctx *Context) int {
	file := ctx.Positionals[0]
	ctx.GraphFile = file

	// Build the skeleton graph with the canonical subgraph titles (§4.1).
	g := &graph.Graph{
		Frontmatter:   buildNewFrontmatter(ctx.Flags["title"]),
		PassedTitle:   "Concepts User understands",
		UntestedTitle: "Concepts User has not been tested on",
		TestingTitle:  "Open tests validating and teaching User understanding",
	}
	out := graph.Write(g)

	// Lint the output as a bug-guard: an empty skeleton must pass lint.
	// A failure here is an internal error in the writer, not a user error.
	lintCfg := buildLintConfig(file)
	if viols := lint.Check(out, lintCfg); len(viols) > 0 {
		msgs := make([]string, len(viols))
		for i, v := range viols {
			if v.Line > 0 {
				msgs[i] = fmt.Sprintf("line %d: %s", v.Line, v.Msg)
			} else {
				msgs[i] = v.Msg
			}
		}
		ctx.ErrMsg = "output fails lint (internal error): " + strings.Join(msgs, "; ")
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 2
	}

	// Create the file with O_EXCL so the check and create are atomic.
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			ctx.ErrMsg = file + " already exists"
			ctx.FixMsg = "tm load " + file
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 1
		}
		ctx.ErrMsg = fmt.Sprintf("cannot create %s: %v", file, err)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	if _, werr := f.Write(out); werr != nil {
		_ = f.Close()
		ctx.ErrMsg = fmt.Sprintf("cannot write %s: %v", file, werr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	if cerr := f.Close(); cerr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot close %s: %v", file, cerr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Write the active-graph pointer so subsequent commands resolve it (§3).
	if werr := writeActivePointer(ctx, file); werr != nil {
		ctx.ErrMsg = werr.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Append the new event to the event log (§10).
	evlog := eventlog.New(eventlog.Path(file), errlog.RealClock)
	evlog.Append(eventlog.NewRow("new", map[string]any{"file": file}))

	_, _ = fmt.Fprintln(ctx.Out, "ok")
	return 0
}

// buildNewFrontmatter builds the §4.1 frontmatter block for a new skeleton.
// titleVals is ctx.Flags["title"]; if non-empty, its first element is inserted
// as a YAML scalar immediately after the opening "---" and before "config:".
func buildNewFrontmatter(titleVals []string) string {
	var b strings.Builder
	b.WriteString("---\n")
	if len(titleVals) > 0 && titleVals[0] != "" {
		b.WriteString("title: ")
		b.WriteString(yamlStringScalar(titleVals[0]))
		b.WriteByte('\n')
	}
	b.WriteString("config:\n")
	b.WriteString("  look: classic\n")
	b.WriteString("  darkMode: true\n")
	b.WriteString("  theme: dark\n")
	b.WriteString("  layout: elk\n")
	b.WriteString("  elk:\n")
	b.WriteString("    mergeEdges: true\n")
	b.WriteString("    nodePlacementStrategy: NETWORK_SIMPLEX\n")
	b.WriteString("---\n")
	return b.String()
}

// yamlStringScalar emits t as a YAML scalar value. If t contains ':' or '#',
// or begins with a YAML special character, it is double-quoted with embedded
// '"' and '\' escaped. Otherwise it is returned verbatim.
func yamlStringScalar(t string) string {
	needsQuotes := false
	if len(t) > 0 {
		// These characters are special in YAML when they appear at the start of
		// a plain scalar value and would require quoting.
		const specialFirst = `-{[|>'\"!%@` + "`&*"
		for _, c := range specialFirst {
			if rune(t[0]) == c {
				needsQuotes = true
				break
			}
		}
	}
	if !needsQuotes {
		for _, r := range t {
			if r == ':' || r == '#' {
				needsQuotes = true
				break
			}
		}
	}
	if !needsQuotes {
		return t
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range t {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// writeActivePointer records file as the active graph, plus the source root
// when --src-root was given (§3). By default both go into the user config as
// absolute paths, so the pointer holds from any working directory. With
// --local they go into .tmconfig in the working directory, as given, which is
// the per-directory mode for two lessons on one machine.
func writeActivePointer(ctx *Context, file string) error {
	_, local := ctx.Flags["local"]
	kv := map[string]string{"file": file}
	if v := ctx.Flags["src-root"]; len(v) > 0 && v[0] != "" {
		kv["src-root"] = v[0]
	}
	path := config.UserPath()
	if local {
		path = config.LocalName
	} else {
		for k, v := range kv {
			abs, err := filepath.Abs(v)
			if err != nil {
				return fmt.Errorf("cannot resolve %s: %v", v, err)
			}
			kv[k] = abs
		}
	}
	if err := config.Set(path, kv); err != nil {
		return fmt.Errorf("cannot write %s: %v", path, err)
	}
	return nil
}
