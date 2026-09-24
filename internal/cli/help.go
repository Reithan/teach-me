package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/reithan/teach-me/internal/config"
	"github.com/reithan/teach-me/internal/docver"
	"github.com/reithan/teach-me/internal/version"
)

// baselineHelp writes the baseline help output to out (stdout) and returns
// the docver mismatch err string (without the "err: " prefix) when the
// configured doc ($TM_DOC or the `doc` config key) carries a marker that is
// missing or differs from the CLI's own marker. It returns "" when there is
// no mismatch or when no doc is configured.
//
// The caller is responsible for logging the mismatch to ERRORS.jsonl with
// the exit code that the surrounding call path returns (§10.1). Keeping the
// log call in run.go means each site can supply the correct exit code (0 for
// bare tm / --help, 3 for the unknown-subcommand path).
//
// With $TM_DOC set, prints only:
//
//	see <path> (tm <version>)
//
// and, on a mismatch:
//
//	err: <path> is for tm <x>, this is tm <y>
//
// Without $TM_DOC, prints one usage line per §6 command.
func baselineHelp(out io.Writer) string {
	docPath := config.Doc()
	ver := version.Version()

	if docPath != "" {
		_, _ = fmt.Fprintf(out, "see %s (tm %s)\n", docPath, ver)

		// Docver check: read the file and compare markers.
		data, err := os.ReadFile(docPath)
		if err == nil {
			marker := docver.Marker()
			docVer, ok := docver.DocVersion(data)

			var mismatch string
			if !ok {
				// Missing marker: use "?" as the document's reported version.
				mismatch = fmt.Sprintf("%s is for tm ?, this is tm %s", docPath, marker)
			} else if docVer != marker {
				mismatch = fmt.Sprintf("%s is for tm %s, this is tm %s", docPath, docVer, marker)
			}

			if mismatch != "" {
				_, _ = fmt.Fprintf(out, "err: %s\n", mismatch)
				return mismatch
			}
		}
		// If the file cannot be read, skip the version check silently (best-effort).
		return ""
	}

	// No doc configured: print one usage line per command.
	commandList(out)
	return ""
}

// commandList prints one usage line per §6 command. It backs baseline help
// without a doc and `tm --help --all`, which the skill file uses to inline
// the command reference regardless of the doc setting.
func commandList(out io.Writer) {
	for _, cmd := range Table {
		_, _ = fmt.Fprintln(out, cmd.Usage())
	}
}

// specificHelp writes the usage line for the named command to out. When
// flagName is non-empty it writes one line describing that flag instead.
// Called for `tm --help <command>`, `tm <command> --help`, and
// `tm <command> --help <flag>`.
func specificHelp(cmd *Command, flagName string, out io.Writer) {
	if flagName != "" {
		flagHelpLine(cmd, flagName, out)
		return
	}
	_, _ = fmt.Fprintln(out, cmd.Usage())
}

// flagHelpLine writes one line describing flagName within cmd, or an
// "unknown flag" note when the flag is not declared.
func flagHelpLine(cmd *Command, flagName string, out io.Writer) {
	// Strip leading "--" if the caller passed the raw token.
	name := flagName
	for len(name) > 0 && name[0] == '-' {
		name = name[1:]
	}

	for _, f := range cmd.Flags {
		if f.Name == name {
			if f.TakesValue {
				_, _ = fmt.Fprintf(out, "--%s %s\n", f.Name, f.ValueName)
			} else {
				_, _ = fmt.Fprintf(out, "--%s\n", f.Name)
			}
			return
		}
	}
	_, _ = fmt.Fprintf(out, "unknown flag --%s for tm %s\n", name, cmd.Name)
}
