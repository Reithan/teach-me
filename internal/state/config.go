// Package state computes the derived state defined in spec §5 from a parsed
// graph. Nothing in §5 is stored in the file; the CLI recomputes it on each
// call.
package state

import (
	"os"
)

// Config holds the runtime knobs used when computing derived state.
type Config struct {
	ProbeMin int
	ProbeMax int
	TeachMin int
	TeachMax int
	MaxFails int
	MaxTeach int
	MaxStall int
	// SrcRoot is the root directory for resolving citations. Load fills it in
	// from cite.SrcRoot(graphDir) when the field is empty.
	SrcRoot string
}

// ConfigFromEnv returns a Config populated from the TM_* environment variables
// defined in spec §13, with the spec defaults applied for any variable that is
// unset or cannot be parsed as a non-negative integer.
//
// SrcRoot is NOT set here; the caller (Load) populates it from
// cite.SrcRoot(graphDir) using the resolved graph-file directory.
func ConfigFromEnv() Config {
	return Config{
		ProbeMin: envInt("TM_PROBE_MIN", 2),
		ProbeMax: envInt("TM_PROBE_MAX", 5),
		TeachMin: envInt("TM_TEACH_MIN", 1),
		TeachMax: envInt("TM_TEACH_MAX", 3),
		MaxFails: envInt("TM_MAX_FAILS", 2),
		MaxTeach: envInt("TM_MAX_TEACH", 8),
		MaxStall: envInt("TM_MAX_STALL", 4),
	}
}

// envInt reads name from the environment as a non-negative integer, returning
// dflt when the variable is absent, empty, or not a valid non-negative integer.
func envInt(name string, dflt int) int {
	s := os.Getenv(name)
	if s == "" {
		return dflt
	}
	if !isDigits(s) {
		return dflt
	}
	return parseDigits(s)
}

func isDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseDigits(s string) int {
	n := 0
	for i := range len(s) {
		n = n*10 + int(s[i]-'0')
	}
	return n
}
