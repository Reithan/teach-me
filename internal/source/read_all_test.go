package source_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/source"
)

// ──────────────────────────────────────────────────────────────────────────────
// ReadAll
// ──────────────────────────────────────────────────────────────────────────────

// TestReadAll verifies that ReadAll returns the full converted text for plain
// paths, converted files, git locators, and URIs; and refuses aid paths.
// One case also proves the ReadAll/Read consistency property that tm src relies on.
func TestReadAll(t *testing.T) {
	// Shared httptest server with a redirect for the URI cases.
	var srvURL string
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		switch r.URL.Path {
		case "/redir":
			http.Redirect(w, r, srvURL+"/plain", http.StatusFound)
		case "/plain":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprint(w, "line one\nline two\nline three\n")
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL

	// Fake converter: strips HTML tags (echo the first line for easy assertions).
	convDir := t.TempDir()
	cv := writeScript(t, convDir, "conv", "sed 's/<[^>]*>//g'")
	vr := writeScript(t, convDir, "conv_ver", `printf "1.0"`)
	htmlCfg := fmt.Sprintf("convert text/html=%s\nversion %s=1.0\nversion-cmd %s=%s\next .html=text/html\n",
		cv, cv, cv, vr)

	tests := []struct {
		name    string
		setup   func(t *testing.T) (dir string, cfg string, locator string)
		wantAll string // full text ReadAll must return
		wantURL string // meta.URL from ReadAll (URI redirect check)
		wantErr bool
		wantRef bool // expect RefusalError
	}{
		{
			name: "plain file full text",
			setup: func(t *testing.T) (string, string, string) {
				dir := t.TempDir()
				f := filepath.Join(dir, "src.txt")
				if err := os.WriteFile(f, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return dir, "", f
			},
			wantAll: "alpha\nbeta\ngamma",
		},
		{
			name: "converted file full text",
			setup: func(t *testing.T) (string, string, string) {
				dir := t.TempDir()
				f := filepath.Join(dir, "doc.html")
				if err := os.WriteFile(f, []byte("<p>hello</p>\n<p>world</p>\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return dir, htmlCfg, f
			},
			wantAll: "hello\nworld",
		},
		{
			name: "URI plain text full fetch",
			setup: func(t *testing.T) (string, string, string) {
				return t.TempDir(), "", srvURL + "/plain"
			},
			wantAll: "line one\nline two\nline three",
		},
		{
			name: "URI redirect sets final URL in meta",
			setup: func(t *testing.T) (string, string, string) {
				return t.TempDir(), "", srvURL + "/redir"
			},
			wantAll: "line one\nline two\nline three",
			wantURL: srvURL + "/plain",
		},
		{
			name: "missing file refuses",
			setup: func(t *testing.T) (string, string, string) {
				dir := t.TempDir()
				return dir, "", filepath.Join(dir, "missing.txt")
			},
			wantErr: true,
			wantRef: true,
		},
		{
			name: "aids-dir path refuses",
			setup: func(t *testing.T) (string, string, string) {
				dir := t.TempDir()
				locator := writeAidFile(t, dir, "note.txt", "aid content\n")
				return dir, "", filepath.Join(dir, locator)
			},
			wantErr: true,
			wantRef: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dir, cfgStr, locator := tc.setup(t)
			r := resolverFrom(loadCfg(t, cfgStr, ""), dir)
			r.GraphDir = dir

			text, meta, err := r.ReadAll(locator)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantRef {
					var ref *source.RefusalError
					if !errors.As(err, &ref) {
						t.Errorf("want RefusalError, got %T: %v", err, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantAll != "" && text != tc.wantAll {
				t.Errorf("ReadAll = %q, want %q", text, tc.wantAll)
			}
			if tc.wantURL != "" && meta.URL != tc.wantURL {
				t.Errorf("meta.URL = %q, want %q", meta.URL, tc.wantURL)
			}
		})
	}
}

// TestReadAll_GitFileAtRef verifies ReadAll returns the full file content for a
// git file-at-ref locator.
func TestReadAll_GitFileAtRef(t *testing.T) {
	gitBin := skipIfNoGit(t)
	dir := t.TempDir()
	const content = "line1\nline2\nline3\n"
	_, sha2 := initGitRepo2Commits(t, dir, "initial\n", content)
	s2 := short12(sha2)

	r := resolverForGit(t, dir, gitBin)
	locator := fmt.Sprintf("git:r@%s:file.txt", s2)

	text, meta, err := r.ReadAll(locator)
	if err != nil {
		t.Fatalf("ReadAll git file-at-ref: %v", err)
	}
	wantAll := strings.TrimSuffix(content, "\n")
	if text != wantAll {
		t.Errorf("ReadAll = %q, want %q", text, wantAll)
	}
	if meta.ResolvedLocator == "" {
		t.Error("meta.ResolvedLocator must not be empty for git locator")
	}
}

// TestReadAll_GitDiffForm verifies ReadAll returns the full diff text for a
// git diff-range locator.
func TestReadAll_GitDiffForm(t *testing.T) {
	gitBin := skipIfNoGit(t)
	dir := t.TempDir()
	sha1, sha2 := initGitRepo2Commits(t, dir, "old\n", "new\n")
	s1, s2 := short12(sha1), short12(sha2)

	r := resolverForGit(t, dir, gitBin)
	locator := fmt.Sprintf("git:r@%s..%s", s1, s2)

	text, _, err := r.ReadAll(locator)
	if err != nil {
		t.Fatalf("ReadAll git diff: %v", err)
	}
	// diff output includes +/- lines; just verify it is non-empty and plausible.
	if !strings.Contains(text, "-old") && !strings.Contains(text, "+new") {
		t.Errorf("ReadAll git diff = %q; want diff content", text)
	}
}

// TestReadAll_PropertyConsistency proves that ReadAll lines [a,b] equal the
// text returned by Read for the same locator with range a-b.
// This is the core property tm src is built on.
func TestReadAll_PropertyConsistency(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(f, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := resolverFrom(loadCfg(t, "", ""), dir)

	allText, _, err := r.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	allLines := strings.Split(allText, "\n")

	// For each pair (a, b) within the file, verify that ReadAll lines [a,b]
	// match what Read returns for the same locator with range a-b.
	total := len(allLines)
	for start := 1; start <= total; start++ {
		for end := start; end <= total; end++ {
			cit := makeCitation(f, start, end)
			readText, _, readErr := r.Read(cit)
			if readErr != nil {
				t.Fatalf("Read(%d-%d): %v", start, end, readErr)
			}
			allSlice := strings.Join(allLines[start-1:end], "\n")
			if allSlice != readText {
				t.Errorf("range %d-%d: ReadAll slice = %q, Read = %q",
					start, end, allSlice, readText)
			}
		}
	}
}

// TestReadAll_URICacheSharing proves that a URI served once is not re-fetched
// when ReadAll and then Read are called within the same TTL.
func TestReadAll_URICacheSharing(t *testing.T) {
	var fetches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "line one\nline two\n")
	}))
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	r := resolverFrom(loadCfg(t, "cache-ttl = 5m\n", ""), t.TempDir())
	r.CacheDir = cacheDir

	// First call: ReadAll — fetches and caches.
	_, _, err := r.ReadAll(srv.URL + "/doc")
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if fetches != 1 {
		t.Errorf("after ReadAll: fetches = %d, want 1", fetches)
	}

	// Second call: Read — must hit cache, no additional fetch.
	_, _, err = r.Read(makeCitation(srv.URL+"/doc", 1, 1))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if fetches != 1 {
		t.Errorf("after Read: fetches = %d, want 1 (cache must be hit)", fetches)
	}
}
