package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/reithan/teach-me/internal/docver"
	"github.com/reithan/teach-me/internal/version"
)

// baselineHelp writes the baseline help output to out (stdout). It is invoked
// for bare `tm`, `tm --help`, and unknown subcommands.
//
// With $TM_DOC set, it prints only:
//
//	see <path> (tm <version>)
//
// and, when the doc's tm-version marker is missing or differs from the CLI's
// own marker, appends:
//
//	err: <path> is for tm <x>, this is tm <y>
//
// These lines go to out (stdout) because they are requested help, not
// diagnostic errors. They are NOT logged to ERRORS.jsonl (they are not
// command-invocation errors).
//
// Without $TM_DOC, it prints one usage line per §6 command.
func baselineHelp(out io.Writer) {
	docPath := os.Getenv("TM_DOC")
	ver := version.Version()

	if docPath != "" {
		_, _ = fmt.Fprintf(out, "see %s (tm %s)\n", docPath, ver)

		// Docver check: read the file and compare markers.
		data, err := os.ReadFile(docPath)
		if err == nil {
			marker := docver.Marker()
			docVer, ok := docver.DocVersion(data)
			if !ok {
				// Missing marker: use "?" as the document's reported version.
				_, _ = fmt.Fprintf(out, "err: %s is for tm ?, this is tm %s\n", docPath, marker)
			} else if docVer != marker {
				_, _ = fmt.Fprintf(out, "err: %s is for tm %s, this is tm %s\n", docPath, docVer, marker)
			}
		}
		// If the file cannot be read, skip the version check silently (best-effort).
		return
	}

	// No TM_DOC: print one usage line per command.
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
