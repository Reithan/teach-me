// Package eventlog implements the §10 event log: an append-only <graph>.jsonl
// writer. Each mutation appends one JSON object per event. The logger is
// best-effort; a write failure never crashes or changes the caller's exit code.
package eventlog

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"time"

	"github.com/reithan/teach-me/internal/errlog"
)

// Path returns the event log path for the given graph file.
func Path(graphFile string) string {
	return graphFile + ".jsonl"
}

// Row is one event in the event log.
//
// Ev is the event name (e.g. "add", "pass"). Fields holds per-event fields
// from the §10 table. The common fields t and role are stamped by Logger.Append
// and must not be set by the caller.
type Row struct {
	// t and role are set by Logger.Append; callers use NewRow.
	t    string
	role *string

	Ev     string
	Fields map[string]any
}

// NewRow constructs a Row for the given event name and fields.
// The t and role fields are stamped by Logger.Append.
func NewRow(ev string, fields map[string]any) Row {
	return Row{Ev: ev, Fields: fields}
}

// MarshalJSON emits a JSON object with fields in this order:
//
//	t, ev, role, then all Fields keys in sorted order.
//
// Deterministic key order makes the log reproducible across runs.
func (r Row) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteByte('{')

	// t
	buf.WriteString(`"t":`)
	tBytes, err := json.Marshal(r.t)
	if err != nil {
		return nil, err
	}
	buf.Write(tBytes)

	// ev
	buf.WriteString(`,"ev":`)
	evBytes, err := json.Marshal(r.Ev)
	if err != nil {
		return nil, err
	}
	buf.Write(evBytes)

	// role (null when unset)
	buf.WriteString(`,"role":`)
	if r.role != nil {
		roleBytes, err := json.Marshal(*r.role)
		if err != nil {
			return nil, err
		}
		buf.Write(roleBytes)
	} else {
		buf.WriteString("null")
	}

	// Per-event fields in sorted key order.
	keys := make([]string, 0, len(r.Fields))
	for k := range r.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		buf.WriteByte(',')
		kBytes, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kBytes)
		buf.WriteByte(':')
		vBytes, err := json.Marshal(r.Fields[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vBytes)
	}

	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Logger appends event rows to a <graph>.jsonl file.
type Logger struct {
	path  string
	clock errlog.Clock
}

// New returns a Logger that writes to path using clk.
func New(path string, clk errlog.Clock) *Logger {
	return &Logger{path: path, clock: clk}
}

// Append stamps t and role on r, marshals it as one JSON line, and appends it
// to the log file. All errors are swallowed; a logging failure never affects
// the mutation's exit code.
func (l *Logger) Append(r Row) {
	r.t = l.clock.Now().UTC().Format(time.RFC3339)

	roleStr := os.Getenv("TM_ROLE")
	if roleStr != "" {
		r.role = &roleStr
	}

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
