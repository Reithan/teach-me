package eventlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixedClock returns a fixed time.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestPath(t *testing.T) {
	got := Path("foo/graph.mmd")
	want := "foo/graph.mmd.jsonl"
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestAppendTwoRows(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "g.mmd.jsonl")

	clk := fixedClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	l := New(logPath, clk)

	t.Setenv("TM_ROLE", "")

	l.Append(NewRow("add", map[string]any{"id": "c1", "scope": "hello"}))
	l.Append(NewRow("pass", map[string]any{"concept": "c1", "unblocked": []string{"c2"}}))

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), data)
	}

	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Errorf("line %d is not valid JSON: %s", i, line)
		}
	}
}

func TestCommonFieldsPresent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "g.mmd.jsonl")

	clk := fixedClock{t: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)}
	l := New(logPath, clk)

	t.Setenv("TM_ROLE", "")

	l.Append(NewRow("new", map[string]any{"file": "g.mmd"}))

	data, _ := os.ReadFile(logPath)
	line := strings.TrimSpace(string(data))

	// Must contain t, ev, role.
	for _, field := range []string{`"t":`, `"ev":`, `"role":`} {
		if !strings.Contains(line, field) {
			t.Errorf("line missing %q: %s", field, line)
		}
	}

	// t must equal the clock time.
	if !strings.Contains(line, `"t":"2026-09-17T12:00:00Z"`) {
		t.Errorf("unexpected t in: %s", line)
	}

	// ev must be "new".
	if !strings.Contains(line, `"ev":"new"`) {
		t.Errorf("unexpected ev in: %s", line)
	}
}

// appendOne writes one "add" event row with the given role env value and returns
// the trimmed log line. Extracted to eliminate repeated scaffold across role/field tests.
func appendOne(t *testing.T, role string, fields map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "g.mmd.jsonl")
	clk := fixedClock{t: time.Now()}
	l := New(logPath, clk)
	t.Setenv("TM_ROLE", role)
	l.Append(NewRow("add", fields))
	data, _ := os.ReadFile(logPath)
	return strings.TrimSpace(string(data))
}

func TestRoleNullWhenUnset(t *testing.T) {
	line := appendOne(t, "", map[string]any{"id": "c1"})
	if !strings.Contains(line, `"role":null`) {
		t.Errorf("expected role:null, got: %s", line)
	}
}

func TestRoleSetWhenEnvPresent(t *testing.T) {
	line := appendOne(t, "teacher", map[string]any{"id": "c1"})
	if !strings.Contains(line, `"role":"teacher"`) {
		t.Errorf("expected role:teacher, got: %s", line)
	}
}

func TestFieldsAreSorted(t *testing.T) {
	// Fields: z first, a second — result must be sorted a before z.
	line := appendOne(t, "", map[string]any{
		"z_field": "last",
		"a_field": "first",
		"m_field": "middle",
	})
	posA := strings.Index(line, `"a_field"`)
	posM := strings.Index(line, `"m_field"`)
	posZ := strings.Index(line, `"z_field"`)
	if posA < 0 || posM < 0 || posZ < 0 {
		t.Fatalf("fields missing in output: %s", line)
	}
	if posA >= posM || posM >= posZ {
		t.Errorf("fields not sorted: a=%d m=%d z=%d in: %s", posA, posM, posZ, line)
	}
}

func TestFieldOrderCommonFirst(t *testing.T) {
	// t must come before ev, ev before role, role before id.
	line := appendOne(t, "", map[string]any{"id": "c1"})
	posT := strings.Index(line, `"t":`)
	posEv := strings.Index(line, `"ev":`)
	posRole := strings.Index(line, `"role":`)
	posID := strings.Index(line, `"id":`)
	if posT >= posEv || posEv >= posRole || posRole >= posID {
		t.Errorf("wrong field order: t=%d ev=%d role=%d id=%d in: %s",
			posT, posEv, posRole, posID, line)
	}
}
