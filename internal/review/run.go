package review

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/reithan/teach-me/internal/cards"
	"github.com/reithan/teach-me/internal/state"
	"github.com/reithan/teach-me/internal/version"
)

const usage = `tmcards review [--file <graph>]
tmcards --version`

// Run is the tmcards command line. It returns the exit code: 0 ok, 2 a graph
// or cards file that cannot be read as one, 3 a usage error.
func Run(args []string, in io.Reader, out, errOut io.Writer) int {
	fail := func(code int, msg, fix string) int {
		say(errOut, "err: %s\n", msg)
		if fix != "" {
			say(errOut, "fix: %s\n", fix)
		}
		return code
	}

	top := flag.NewFlagSet("tmcards", flag.ContinueOnError)
	top.SetOutput(io.Discard)
	showVersion := top.Bool("version", false, "")
	file := top.String("file", "", "")
	if err := top.Parse(args); err != nil {
		return fail(3, err.Error(), usage)
	}
	if *showVersion {
		say(out, "%s\n", version.Version())
		return 0
	}
	if top.NArg() == 0 {
		return fail(3, "tmcards needs a command", usage)
	}

	name, rest := top.Arg(0), top.Args()[1:]
	sub := flag.NewFlagSet(name, flag.ContinueOnError)
	sub.SetOutput(io.Discard)
	sub.StringVar(file, "file", *file, "")
	if name != "review" {
		return fail(3, fmt.Sprintf("unknown command %q", name), usage)
	}
	if err := sub.Parse(rest); err != nil {
		return fail(3, err.Error(), usage)
	}
	if sub.NArg() > 0 {
		return fail(3, fmt.Sprintf("unexpected argument %q", sub.Arg(0)), usage)
	}

	st, code, msg := open(*file)
	if code != 0 {
		return fail(code, msg, "")
	}
	if _, err := st.Terminal(in, out); err != nil {
		return fail(loadCode(err), err.Error(), "")
	}
	return 0
}

// open resolves the graph file as tm does (spec §3) and loads the settings.
func open(fileFlag string) (st *Store, code int, msg string) {
	graphFile, err := state.ResolveFile(fileFlag)
	if err != nil {
		return nil, 3, err.Error()
	}
	set, err := cards.LoadSettings()
	if err != nil {
		return nil, 3, err.Error()
	}
	return NewStore(graphFile, set), 0, ""
}

// loadCode picks the exit code for a failed load: 2 for a cards file with a
// bad row, as in tm, and 3 for anything else.
func loadCode(err error) int {
	var re *cards.RowError
	if errors.As(err, &re) {
		return 2
	}
	return 3
}
