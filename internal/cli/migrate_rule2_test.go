package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	icite "github.com/reithan/teach-me/internal/cite"
)

// writeEventLogURL writes an "add" event with a url field but no commit field,
// simulating a concept whose source was fetched from the web and saved as a
// plain-path copy.
func writeEventLogURL(t *testing.T, logPath, id, citeStr, url string) {
	t.Helper()
	ev := map[string]any{
		"ev":   "add",
		"id":   id,
		"cite": citeStr,
		"url":  url,
		"t":    "2026-01-01T00:00:00Z",
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if err := os.WriteFile(logPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write event log: %v", err)
	}
}

// TestMigrateRule2_Match verifies the happy path: a plain-path citation whose
// add event logged a url that serves identical text is rewritten to the hashed
// URL citation.
func TestMigrateRule2_Match(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	t.Setenv("TM_CACHE_DIR", t.TempDir())

	const content = "line1\nline2\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, content)
	}))
	t.Cleanup(srv.Close)

	// Hash the two lines that the citation covers.
	contentHash := icite.Hash("line1\nline2")

	// Create the graph in a temp dir with a saved copy of the URL content.
	graphDir := t.TempDir()
	t.Chdir(graphDir)
	graphFile := filepath.Join(graphDir, "g.mmd")
	savedCopy := filepath.Join(graphDir, "file.txt")
	if err := os.WriteFile(savedCopy, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	citeStr := contentHash + "@file.txt:1-2"
	if err := os.WriteFile(graphFile, []byte(buildRule1Graph(contentHash)), 0o644); err != nil {
		t.Fatal(err)
	}

	writeEventLogURL(t, graphFile+".jsonl", "c1", citeStr, srv.URL+"/doc.txt")

	t.Setenv("TM_FILE", graphFile)
	t.Setenv("TM_SRC_ROOT", graphDir)

	// dry-run: output shows ok rewrite, file unchanged.
	dryOut, dryErr, dryCode := run(t, "migrate", graphFile, "--dry-run")
	if dryCode != 0 {
		t.Fatalf("tm migrate --dry-run: exit %d; stderr: %s", dryCode, dryErr)
	}
	if !strings.Contains(dryOut, "ok c1") {
		t.Errorf("dry-run output should contain 'ok c1'; got:\n%s", dryOut)
	}
	if !strings.Contains(dryOut, srv.URL) {
		t.Errorf("dry-run output should contain server URL; got:\n%s", dryOut)
	}
	// File must be unchanged after dry-run.
	before, _ := os.ReadFile(graphFile)
	if strings.Contains(string(before), "format 2") {
		t.Error("graph was written during dry-run")
	}

	// Real run: graph is rewritten to format 2 with the URL citation.
	outReal, errReal, codeReal := run(t, "migrate", graphFile)
	if codeReal != 0 {
		t.Fatalf("tm migrate: exit %d; stderr: %s", codeReal, errReal)
	}
	_ = outReal

	after, _ := os.ReadFile(graphFile)
	if !strings.Contains(string(after), srv.URL) {
		t.Errorf("migrated graph should contain server URL; graph:\n%s", after)
	}
	if !strings.Contains(string(after), "tm:format 2") {
		t.Errorf("migrated graph should contain format marker; graph:\n%s", after)
	}
}

// TestMigrateRule2_HashDiffers verifies that when the URL serves different
// content than what was stored, the citation is left with a "hash differs" reason.
func TestMigrateRule2_HashDiffers(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	t.Setenv("TM_CACHE_DIR", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// Different content from what was saved locally.
		_, _ = fmt.Fprint(w, "different\ncontent\n")
	}))
	t.Cleanup(srv.Close)

	// The saved file has original content; the hash matches the saved copy.
	originalContent := "line1\nline2"
	contentHash := icite.Hash(originalContent)

	graphDir := t.TempDir()
	t.Chdir(graphDir)
	graphFile := filepath.Join(graphDir, "g.mmd")
	savedCopy := filepath.Join(graphDir, "file.txt")
	if err := os.WriteFile(savedCopy, []byte(originalContent+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	citeStr := contentHash + "@file.txt:1-2"
	if err := os.WriteFile(graphFile, []byte(buildRule1Graph(contentHash)), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEventLogURL(t, graphFile+".jsonl", "c1", citeStr, srv.URL+"/doc.txt")

	t.Setenv("TM_FILE", graphFile)
	t.Setenv("TM_SRC_ROOT", graphDir)

	out, _, code := run(t, "migrate", graphFile, "--dry-run")
	if code != 0 {
		t.Fatalf("tm migrate --dry-run: exit %d", code)
	}
	if !strings.Contains(out, "hash differs") {
		t.Errorf("want 'hash differs' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "left c1") {
		t.Errorf("want 'left c1' in output; got:\n%s", out)
	}
}

// TestMigrateRule2_FetchFails verifies that when the URL is unreachable the
// citation is left with the fetch error text as the reason.
func TestMigrateRule2_FetchFails(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	t.Setenv("TM_CACHE_DIR", t.TempDir())
	// Disable cache TTL so no stale entry masks the failure.
	t.Setenv("TM_CACHE_TTL", "0")

	// Use a server that we immediately close so all requests fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	deadURL := srv.URL
	srv.Close()

	contentHash := icite.Hash("line1\nline2")

	graphDir := t.TempDir()
	t.Chdir(graphDir)
	graphFile := filepath.Join(graphDir, "g.mmd")
	savedCopy := filepath.Join(graphDir, "file.txt")
	if err := os.WriteFile(savedCopy, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	citeStr := contentHash + "@file.txt:1-2"
	if err := os.WriteFile(graphFile, []byte(buildRule1Graph(contentHash)), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEventLogURL(t, graphFile+".jsonl", "c1", citeStr, deadURL+"/doc.txt")

	t.Setenv("TM_FILE", graphFile)
	t.Setenv("TM_SRC_ROOT", graphDir)

	out, _, code := run(t, "migrate", graphFile, "--dry-run")
	if code != 0 {
		t.Fatalf("tm migrate --dry-run: exit %d", code)
	}
	// Citation is left with a fetch-related error reason (not hash differs).
	if !strings.Contains(out, "left c1") {
		t.Errorf("want 'left c1' in output; got:\n%s", out)
	}
	// Should not claim hash differs since the fetch itself failed.
	if strings.Contains(out, "hash differs") {
		t.Errorf("unexpected 'hash differs' for a dead server; got:\n%s", out)
	}
}

// TestMigrateRule2_NoURL verifies that a plain-path citation whose event has
// no url field is not claimed by rule 2 (falls through to "plain path" reason).
func TestMigrateRule2_NoURL(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	t.Setenv("TM_CACHE_DIR", t.TempDir())

	contentHash := icite.Hash("line1\nline2")

	graphDir := t.TempDir()
	t.Chdir(graphDir)
	graphFile := filepath.Join(graphDir, "g.mmd")
	savedCopy := filepath.Join(graphDir, "file.txt")
	if err := os.WriteFile(savedCopy, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	citeStr := contentHash + "@file.txt:1-2"
	if err := os.WriteFile(graphFile, []byte(buildRule1Graph(contentHash)), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write event with NO url field and NO commit field.
	ev := map[string]any{
		"ev":   "add",
		"id":   "c1",
		"cite": citeStr,
		"t":    "2026-01-01T00:00:00Z",
	}
	data, _ := json.Marshal(ev)
	if err := os.WriteFile(graphFile+".jsonl", append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TM_FILE", graphFile)
	t.Setenv("TM_SRC_ROOT", graphDir)

	out, _, code := run(t, "migrate", graphFile, "--dry-run")
	if code != 0 {
		t.Fatalf("tm migrate --dry-run: exit %d", code)
	}
	// Rule 2 returns empty reason; falls through to "plain path" default.
	if !strings.Contains(out, "plain path") {
		t.Errorf("want 'plain path' reason for no-url event; got:\n%s", out)
	}
}
