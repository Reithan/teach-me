// Package errlog implements the §10.1 error log.
//
// Every err: the CLI prints is also appended to ERRORS.jsonl. Logging is
// silent and best-effort: if the file cannot be written, the caller's output
// and exit code are unchanged.
package errlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/reithan/teach-me/internal/version"
)

// Clock supplies the current time. Tests substitute a fixed implementation.
// The interface is exported so the §10 event log (M5) can share the same type.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// RealClock is the production Clock implementation.
var RealClock Clock = realClock{}

// Row is one entry in ERRORS.jsonl. Field order is fixed per §10.1.
//
// Pointer fields (Role, File, Fix) serialize as JSON null when nil, satisfying
// the spec requirement that absent optional fields appear as null.
// Violations is omitted entirely for non-lint rows (omitempty on a nil slice).
type Row struct {
	T          string   `json:"t"`
	V          string   `json:"v"`
	Role       *string  `json:"role"`
	File       *string  `json:"file"`
	Argv       []string `json:"argv"`
	Exit       int      `json:"exit"`
	Err        string   `json:"err"`
	Fix        *string  `json:"fix"`
	Violations []string `json:"violations,omitempty"`
}

// Path returns the error-log path for the given graph directory.
//
// If $TM_ERRORS is set it is returned unchanged. Otherwise the file
// ERRORS.jsonl inside graphDir is returned. When graphDir is empty, "."
// (the current working directory) is used.
func Path(graphDir string) string {
	if v := os.Getenv("TM_ERRORS"); v != "" {
		return v
	}
	if graphDir == "" {
		graphDir = "."
	}
	return filepath.Join(graphDir, "ERRORS.jsonl")
}

// Logger appends error rows to an ERRORS.jsonl file.
type Logger struct {
	path  string
	clock Clock
}

// New returns a Logger that writes to path p using clock c.
func New(path string, c Clock) *Logger {
	return &Logger{path: path, clock: c}
}

// Append marshals r as one JSON line and appends it to the log file.
// t and v are overwritten by the logger. All errors are swallowed; the
// caller's output and exit code are unaffected by log failures.
func (l *Logger) Append(r Row) {
	r.T = l.clock.Now().Format(time.RFC3339)
	r.V = version.Version()

	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	data = append(data, '\n')

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(data)
}
