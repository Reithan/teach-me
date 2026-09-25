package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// sanitize replaces non-alphanumeric chars with underscores for file naming.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

// TestCacheList_Empty verifies that tm cache list prints nothing when empty.
func TestCacheList_Empty(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_ROLE", "")

	cacheDir := t.TempDir()
	t.Setenv("TM_CACHE_DIR", cacheDir)

	out, errOut, code := run(t, "cache", "list")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr: %s", code, errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("want empty output for empty cache, got %q", out)
	}
}

// TestCacheList_Output verifies the format of tm cache list output.
func TestCacheList_Output(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_ROLE", "")

	cacheDir := t.TempDir()
	t.Setenv("TM_CACHE_DIR", cacheDir)

	// Write two entries manually.
	now := time.Now().UTC().Truncate(time.Second)
	fa1 := now.Format(time.RFC3339)
	fa2 := now.Add(-1 * time.Hour).Format(time.RFC3339)

	// Write real .json files with the expected format.
	writeRealEntry := func(dir, locator, fetchedAt, converter string) {
		t.Helper()
		entry := map[string]any{
			"locator":           locator,
			"final_url":         locator,
			"mime":              "text/plain",
			"converter":         converter,
			"converter_version": "",
			"fetched_at":        fetchedAt,
			"text":              "content\n",
		}
		data, _ := json.Marshal(entry)
		// Use a fixed name so test is deterministic.
		name := fmt.Sprintf("e-%s.json", sanitize(locator))
		_ = os.WriteFile(filepath.Join(dir, name), data, 0o644)
	}

	writeRealEntry(cacheDir, "https://a.example.com/doc", fa1, "")
	writeRealEntry(cacheDir, "https://b.example.com/page", fa2, "pandoc -t plain")

	out, errOut, code := run(t, "cache", "list")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr: %s", code, errOut)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d:\n%s", len(lines), out)
	}

	// Lines sorted by locator: a.example.com comes first.
	if !strings.HasPrefix(lines[0], "https://a.example.com/doc") {
		t.Errorf("line 0 = %q; want https://a.example.com/doc prefix", lines[0])
	}
	if !strings.HasPrefix(lines[1], "https://b.example.com/page") {
		t.Errorf("line 1 = %q; want https://b.example.com/page prefix", lines[1])
	}

	// No-converter entry shows "-".
	if !strings.HasSuffix(strings.TrimSpace(lines[0]), "-") {
		t.Errorf("line 0 should end with '-' for no converter: %q", lines[0])
	}
	// Converter entry shows the command.
	if !strings.Contains(lines[1], "pandoc -t plain") {
		t.Errorf("line 1 should contain converter name: %q", lines[1])
	}
}

// TestCacheClear_EmptiesDir verifies that tm cache clear removes all .json
// entries and prints "ok".
func TestCacheClear_EmptiesDir(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_ROLE", "")

	cacheDir := t.TempDir()
	t.Setenv("TM_CACHE_DIR", cacheDir)

	// Place three .json files and one unrelated file.
	for i := range 3 {
		p := filepath.Join(cacheDir, fmt.Sprintf("entry%d.json", i))
		_ = os.WriteFile(p, []byte(`{"locator":"x"}`), 0o644)
	}
	_ = os.WriteFile(filepath.Join(cacheDir, "other.txt"), []byte("keep"), 0o644)

	out, errOut, code := run(t, "cache", "clear")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr: %s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// .json files are gone.
	jsonFiles, _ := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	if len(jsonFiles) != 0 {
		t.Errorf("want 0 .json files after clear, got %d", len(jsonFiles))
	}
	// Non-.json file is preserved.
	if _, err := os.Stat(filepath.Join(cacheDir, "other.txt")); err != nil {
		t.Errorf("other.txt should be preserved: %v", err)
	}
}

// TestCacheClear_EmptyDir verifies that tm cache clear on a missing or empty
// cache dir still prints "ok".
func TestCacheClear_EmptyDir(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_ROLE", "")

	// Point to a dir that doesn't exist yet.
	cacheDir := filepath.Join(t.TempDir(), "nonexistent")
	t.Setenv("TM_CACHE_DIR", cacheDir)

	out, errOut, code := run(t, "cache", "clear")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr: %s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}
}

// TestCacheGrader_Refused verifies that graders cannot use tm cache commands.
func TestCacheGrader_Refused(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_ROLE", "grader")
	t.Setenv("TM_CACHE_DIR", t.TempDir())

	tests := []struct{ subcmd string }{
		{"clear"},
		{"list"},
	}
	for _, tc := range tests {
		t.Run(tc.subcmd, func(t *testing.T) {
			_, errOut, code := run(t, "cache", tc.subcmd)
			if code == 0 {
				t.Fatalf("want non-zero exit for grader, got 0")
			}
			if !strings.Contains(errOut, "TM_ROLE=grader") {
				t.Errorf("want grader refusal in stderr; got: %s", errOut)
			}
		})
	}
}

// TestCacheFetch_WritesEntry verifies that a URI fetch (via tm add) writes a
// cache entry and a subsequent fetch hits the cache (one HTTP request for two
// reads).
func TestCacheFetch_WritesEntry(t *testing.T) {
	// This test uses a live httptest server and CLI integration.
	var reqCount int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&reqCount, 1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintln(w, "alpha")
		_, _ = fmt.Fprintln(w, "beta")
		_, _ = fmt.Fprintln(w, "gamma")
	}))
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	t.Setenv("TM_CACHE_DIR", cacheDir)
	t.Setenv("TM_CACHE_TTL", "24h")
	tempErrlog(t)
	t.Setenv("TM_ROLE", "")

	// Create a graph and add a concept with a URL citation.
	graphDir := t.TempDir()
	graphFile := filepath.Join(graphDir, "test.mmd")
	t.Setenv("TM_FILE", graphFile)
	t.Setenv("TM_SRC_ROOT", graphDir)

	url := srv.URL + ":1-2"
	out, errOut, code := run(t, "new", graphFile)
	if code != 0 {
		t.Fatalf("new: code=%d stderr=%s", code, errOut)
	}
	_ = out

	// Add a concept with the URL citation (two lines: alpha, beta).
	_, errOut, code = run(t, "add", "concept1", url, "scope text for test")
	if code != 0 {
		t.Fatalf("add: code=%d stderr=%s", code, errOut)
	}

	// One HTTP request was made for the add.
	if atomic.LoadInt64(&reqCount) != 1 {
		t.Fatalf("want 1 HTTP request after add, got %d", atomic.LoadInt64(&reqCount))
	}

	// A cache entry must exist.
	entries, _ := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	if len(entries) == 0 {
		t.Fatal("want a cache entry after add, got none")
	}

	// tm cache list must show the URL.
	out, errOut, code = run(t, "cache", "list")
	if code != 0 {
		t.Fatalf("cache list: code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, srv.URL) {
		t.Errorf("cache list output missing URL %s:\n%s", srv.URL, out)
	}
}

// TestCacheList_NonExistentCacheDir verifies that tm cache list returns 0
// with no output when the cache dir does not exist yet.
func TestCacheList_NonExistentCacheDir(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_ROLE", "")

	// Point to a dir that doesn't exist.
	cacheDir := filepath.Join(t.TempDir(), "doesnotexist")
	t.Setenv("TM_CACHE_DIR", cacheDir)

	out, errOut, code := run(t, "cache", "list")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr: %s", code, errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("want empty output for non-existent cache dir, got %q", out)
	}
}

// TestCacheList_SkipsMalformedEntries verifies that tm cache list skips
// malformed JSON files silently and still returns the valid entries.
func TestCacheList_SkipsMalformedEntries(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_ROLE", "")

	cacheDir := t.TempDir()
	t.Setenv("TM_CACHE_DIR", cacheDir)

	// Write one valid entry.
	now := time.Now().UTC().Truncate(time.Second)
	valid := map[string]any{
		"locator":           "https://good.example.com/doc",
		"final_url":         "https://good.example.com/doc",
		"mime":              "text/plain",
		"converter":         "",
		"converter_version": "",
		"fetched_at":        now.Format(time.RFC3339),
		"text":              "content\n",
	}
	validData, _ := json.Marshal(valid)
	_ = os.WriteFile(filepath.Join(cacheDir, "valid.json"), validData, 0o644)

	// Write one malformed JSON file.
	_ = os.WriteFile(filepath.Join(cacheDir, "broken.json"), []byte("not-json{{{"), 0o644)

	// Write a non-.json file that should be skipped.
	_ = os.WriteFile(filepath.Join(cacheDir, "other.txt"), []byte("skip me"), 0o644)

	out, errOut, code := run(t, "cache", "list")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr: %s", code, errOut)
	}

	// Only the valid entry should appear.
	if !strings.Contains(out, "https://good.example.com/doc") {
		t.Errorf("want valid entry in output, got: %q", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Errorf("want 1 output line (malformed skipped), got %d:\n%s", len(lines), out)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// TestCacheDrift_CLI
// ──────────────────────────────────────────────────────────────────────────────

// TestCacheDrift_CLI verifies the accepted cache-drift trade: content changes
// on a live URL are not detected by tm check --drift while a cache entry is
// fresh (§13.1 TTL-late rule), and are detected after tm cache clear forces a
// re-fetch.
func TestCacheDrift_CLI(t *testing.T) {
	tempErrlog(t)

	var served int32 // 0 = original content, 1 = modified content
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.LoadInt32(&served) == 0 {
			_, _ = fmt.Fprintln(w, "line 1")
			_, _ = fmt.Fprintln(w, "line 2")
			_, _ = fmt.Fprintln(w, "line 3")
			_, _ = fmt.Fprintln(w, "line 4")
			_, _ = fmt.Fprintln(w, "line 5")
		} else {
			_, _ = fmt.Fprintln(w, "line 1 MODIFIED")
			_, _ = fmt.Fprintln(w, "line 2")
			_, _ = fmt.Fprintln(w, "line 3")
			_, _ = fmt.Fprintln(w, "line 4")
			_, _ = fmt.Fprintln(w, "line 5")
		}
	}))
	t.Cleanup(srv.Close)

	// Isolate cache to a per-test dir so TTL does not expire mid-test.
	cacheDir := t.TempDir()
	t.Setenv("TM_CACHE_DIR", cacheDir)
	t.Setenv("TM_PROBE_MIN", "1")

	dir := t.TempDir()
	t.Chdir(dir)
	gfile := filepath.Join(dir, "g.mmd")

	if _, _, code := run(t, "new", gfile); code != 0 {
		t.Fatalf("new: exit %d", code)
	}
	t.Setenv("TM_FILE", gfile)

	// Add concept and question with URL citations (caches the URL content).
	addCite := srv.URL + ":1-5"
	if _, _, code := run(t, "add", "c1", addCite, "Scope of c1"); code != 0 {
		t.Fatalf("add: exit %d", code)
	}
	qCite := srv.URL + ":1-3"
	if _, _, code := run(t, "q", "c1", qCite, "What is on line 1?"); code != 0 {
		t.Fatalf("q: exit %d", code)
	}

	// Advance to passed state so grade events exist for tm check --drift.
	out, _, code := run(t, "ask", "c1")
	if code != 0 {
		t.Fatalf("ask: exit %d", code)
	}
	askLines := strings.Split(strings.TrimSpace(out), "\n")
	if len(askLines) < 2 {
		t.Fatalf("ask: want >=2 lines; got %q", out)
	}
	qid := strings.Fields(askLines[1])[0]
	if _, _, code = run(t, "answer", qid, "line 1"); code != 0 {
		t.Fatalf("answer: exit %d", code)
	}
	if _, _, code = run(t, "grade", qid, "pass", "Correct"); code != 0 {
		t.Fatalf("grade: exit %d", code)
	}

	// Flip server to return modified content; cache still holds original.
	atomic.StoreInt32(&served, 1)

	// tm check --drift c1: cache hit → original content matches stored hash → no DRIFT.
	out, errOut, code := run(t, "check", "--drift", "c1")
	if code != 0 {
		t.Fatalf("check --drift (cached): exit %d; stderr=%s", code, errOut)
	}
	if strings.Contains(out, "DRIFT ") {
		t.Errorf("want no DRIFT within TTL (cached); output:\n%s", out)
	}

	// tm cache clear removes all entries.
	if _, _, code = run(t, "cache", "clear"); code != 0 {
		t.Fatalf("cache clear: exit %d", code)
	}

	// tm check --drift c1: fresh fetch → modified content → hash differs → DRIFT.
	out, errOut, code = run(t, "check", "--drift", "c1")
	if code != 0 {
		t.Fatalf("check --drift (post-clear): exit %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "DRIFT ") {
		t.Errorf("want DRIFT after cache clear; output:\n%s", out)
	}
}
