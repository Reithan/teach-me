package source_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/source"
)

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// newCounter returns an HTTP handler that counts requests and serves the
// current value of *body (dereferenced on each request).
func newCounter(body *string) (http.Handler, *int64) {
	var n int64
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&n, 1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, *body)
	}), &n
}

// resolverWithCache creates a Resolver with cfg, srcRoot, and cacheDir all
// set. Tests drive the CacheDir field rather than relying on UserCacheDir.
func resolverWithCache(cfg *source.Config, srcRoot, cacheDir string) *source.Resolver {
	r := source.NewResolverWithConfig(cfg, srcRoot)
	r.CacheDir = cacheDir
	return r
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

// TestCache_Hit verifies that two Reads of the same URI within TTL produce
// only one HTTP request and that the second call returns the cached FetchedAt.
func TestCache_Hit(t *testing.T) {
	body := "alpha\nbeta\ngamma\n"
	handler, reqs := newCounter(&body)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	cfg := loadCfg(t, "", "")
	cfg.CacheTTL = 24 * time.Hour
	r := resolverWithCache(cfg, t.TempDir(), cacheDir)

	c := cite.Citation{File: srv.URL, Start: 1, End: 2}

	_, meta1, err := r.Read(c)
	if err != nil {
		t.Fatalf("first Read: %v", err)
	}
	if atomic.LoadInt64(reqs) != 1 {
		t.Fatalf("want 1 request after first read, got %d", atomic.LoadInt64(reqs))
	}

	_, meta2, err := r.Read(c)
	if err != nil {
		t.Fatalf("second Read: %v", err)
	}
	if atomic.LoadInt64(reqs) != 1 {
		t.Errorf("want still 1 request (cache hit), got %d", atomic.LoadInt64(reqs))
	}
	// FetchedAt should be identical (the cached timestamp).
	if !meta2.FetchedAt.Equal(meta1.FetchedAt) {
		t.Errorf("meta2.FetchedAt = %v, want %v (cached)", meta2.FetchedAt, meta1.FetchedAt)
	}
	// URL is preserved from cache.
	if meta2.URL != meta1.URL {
		t.Errorf("meta2.URL = %q, want %q", meta2.URL, meta1.URL)
	}
}

// TestCache_Expiry verifies that an expired entry causes a re-fetch.
func TestCache_Expiry(t *testing.T) {
	body := "line1\nline2\n"
	handler, reqs := newCounter(&body)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	cfg := loadCfg(t, "", "")
	cfg.CacheTTL = 24 * time.Hour
	r := resolverWithCache(cfg, t.TempDir(), cacheDir)

	c := cite.Citation{File: srv.URL, Start: 1, End: 1}

	// First read populates the cache.
	if _, _, err := r.Read(c); err != nil {
		t.Fatalf("first Read: %v", err)
	}
	if atomic.LoadInt64(reqs) != 1 {
		t.Fatalf("want 1 request, got %d", atomic.LoadInt64(reqs))
	}

	// Backdate the cache entry's fetched_at by 25 hours to simulate expiry.
	entryFiles, _ := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	if len(entryFiles) != 1 {
		t.Fatalf("expected 1 cache file, got %d", len(entryFiles))
	}
	data, err := os.ReadFile(entryFiles[0])
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("unmarshal cache: %v", err)
	}
	entry["fetched_at"] = time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339)
	modified, _ := json.Marshal(entry)
	if err := os.WriteFile(entryFiles[0], modified, 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	// Second read must re-fetch because entry is expired.
	if _, _, err := r.Read(c); err != nil {
		t.Fatalf("second Read: %v", err)
	}
	if atomic.LoadInt64(reqs) != 2 {
		t.Errorf("want 2 requests (expiry refetch), got %d", atomic.LoadInt64(reqs))
	}
}

// TestCache_TTLZeroDisables verifies that CacheTTL=0 disables the cache
// entirely: no hit, no write.
func TestCache_TTLZeroDisables(t *testing.T) {
	body := "hello\nworld\n"
	handler, reqs := newCounter(&body)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	cfg := loadCfg(t, "cache-ttl=0\n", "")
	r := resolverWithCache(cfg, t.TempDir(), cacheDir)

	c := cite.Citation{File: srv.URL, Start: 1, End: 1}

	for range 2 {
		if _, _, err := r.Read(c); err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if atomic.LoadInt64(reqs) != 2 {
		t.Errorf("want 2 requests (disabled cache), got %d", atomic.LoadInt64(reqs))
	}
	// No cache file should be written.
	entries, _ := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	if len(entries) != 0 {
		t.Errorf("want no cache files, got %d", len(entries))
	}
}

// TestCache_TTLZeroEnvOverride verifies that TM_CACHE_TTL=0 disables the
// cache even when a config-file TTL is set.
func TestCache_TTLZeroEnvOverride(t *testing.T) {
	body := "one\ntwo\n"
	handler, reqs := newCounter(&body)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	cfg := loadCfg(t, "cache-ttl=24h\n", "") // config says 24h
	r := resolverWithCache(cfg, t.TempDir(), cacheDir)

	t.Setenv("TM_CACHE_TTL", "0")

	c := cite.Citation{File: srv.URL, Start: 1, End: 1}
	for range 2 {
		if _, _, err := r.Read(c); err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if atomic.LoadInt64(reqs) != 2 {
		t.Errorf("want 2 requests (TM_CACHE_TTL=0 disables), got %d", atomic.LoadInt64(reqs))
	}
}

// TestCache_ConverterMiss verifies that a cache entry is treated as a miss
// when either the converter command or the pinned converter version changes.
func TestCache_ConverterMiss(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, cvDir string) (cfg1Text, cfg2Text string)
	}{
		{
			name: "different converter command",
			setup: func(t *testing.T, cvDir string) (string, string) {
				t.Helper()
				conv1 := writeScript(t, cvDir, "conv1", "sed 's/x/A/g'")
				conv1ver := writeScript(t, cvDir, "conv1_ver", `printf "1.0"`)
				conv2 := writeScript(t, cvDir, "conv2", "sed 's/x/B/g'")
				conv2ver := writeScript(t, cvDir, "conv2_ver", `printf "2.0"`)
				c1 := fmt.Sprintf("convert text/plain=%s\nversion %s=1.0\nversion-cmd %s=%s\n",
					conv1, conv1, conv1, conv1ver)
				c2 := fmt.Sprintf("convert text/plain=%s\nversion %s=2.0\nversion-cmd %s=%s\n",
					conv2, conv2, conv2, conv2ver)
				return c1, c2
			},
		},
		{
			name: "same converter different version",
			setup: func(t *testing.T, cvDir string) (string, string) {
				t.Helper()
				conv := writeScript(t, cvDir, "conv", "cat")
				ver1 := writeScript(t, cvDir, "ver1", `printf "conv version 1.0"`)
				ver2 := writeScript(t, cvDir, "ver2", `printf "conv version 2.0"`)
				c1 := fmt.Sprintf("convert text/plain=%s\nversion %s=1.0\nversion-cmd %s=%s\n",
					conv, conv, conv, ver1)
				c2 := fmt.Sprintf("convert text/plain=%s\nversion %s=2.0\nversion-cmd %s=%s\n",
					conv, conv, conv, ver2)
				return c1, c2
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cvDir := t.TempDir()
			cfg1Text, cfg2Text := tc.setup(t, cvDir)

			body := "hello x world\n"
			handler, reqs := newCounter(&body)
			srv := httptest.NewServer(handler)
			t.Cleanup(srv.Close)

			cacheDir := t.TempDir()

			cfg1 := loadCfg(t, cfg1Text, "")
			cfg1.CacheTTLSet = true
			cfg1.CacheTTL = 24 * time.Hour
			r1 := resolverWithCache(cfg1, t.TempDir(), cacheDir)

			c := cite.Citation{File: srv.URL, Start: 1, End: 1}
			if _, _, err := r1.Read(c); err != nil {
				t.Fatalf("Read with cfg1: %v", err)
			}
			if atomic.LoadInt64(reqs) != 1 {
				t.Fatalf("want 1 request, got %d", atomic.LoadInt64(reqs))
			}

			cfg2 := loadCfg(t, cfg2Text, "")
			cfg2.CacheTTLSet = true
			cfg2.CacheTTL = 24 * time.Hour
			r2 := resolverWithCache(cfg2, t.TempDir(), cacheDir)

			if _, _, err := r2.Read(c); err != nil {
				t.Fatalf("Read with cfg2: %v", err)
			}
			if atomic.LoadInt64(reqs) != 2 {
				t.Errorf("want 2 requests (miss), got %d", atomic.LoadInt64(reqs))
			}
		})
	}
}

// TestCache_FailedFetchNotCached verifies that a failed fetch is not cached
// and the refusal carries the new fix text.
func TestCache_FailedFetchNotCached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	cfg := loadCfg(t, "", "")
	cfg.CacheTTL = 24 * time.Hour
	r := resolverWithCache(cfg, t.TempDir(), cacheDir)

	c := cite.Citation{File: srv.URL, Start: 1, End: 1}
	_, _, err := r.Read(c)
	if err == nil {
		t.Fatal("expected error for 410 response, got nil")
	}

	// The fix line should carry the new text.
	var refusal *source.RefusalError
	if re, ok := err.(*source.RefusalError); ok {
		refusal = re
	}
	if refusal == nil {
		t.Fatalf("expected *source.RefusalError, got %T: %v", err, err)
	}
	wantFix := "retry when egress is available, or ask the learner for a copy and cite the copy as a plain path"
	if refusal.Fix != wantFix {
		t.Errorf("fix = %q, want %q", refusal.Fix, wantFix)
	}

	// No cache file should be written.
	entries, _ := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	if len(entries) != 0 {
		t.Errorf("want no cache files after failed fetch, got %d", len(entries))
	}
}

// TestCache_DriftWithinTTL verifies the documented accepted trade: a URL
// whose content changes within the TTL still passes CheckDrift (cache hit),
// but shows as drifted after the cache entry is removed.
func TestCache_DriftWithinTTL(t *testing.T) {
	var changed int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if atomic.LoadInt32(&changed) == 0 {
			_, _ = fmt.Fprint(w, "original line one\noriginal line two\n")
		} else {
			_, _ = fmt.Fprint(w, "different line one\ndifferent line two\n")
		}
	}))
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	cfg := loadCfg(t, "", "")
	cfg.CacheTTL = 24 * time.Hour

	// Hash the content as originally served.
	r := resolverWithCache(cfg, t.TempDir(), cacheDir)
	c := cite.Citation{File: srv.URL, Start: 1, End: 2}
	text, _, err := r.Read(c)
	if err != nil {
		t.Fatalf("initial Read: %v", err)
	}
	hash := cite.Hash(text)
	citeStr := fmt.Sprintf("%s@%s:1-2", hash, srv.URL)

	// Flip server to modified content (cache still holds original).
	atomic.StoreInt32(&changed, 1)

	// Within TTL: CheckDrift returns false (cache hit, original content).
	drifted, _, err := r.CheckDrift(citeStr)
	if err != nil {
		t.Fatalf("CheckDrift (within TTL): %v", err)
	}
	if drifted {
		t.Error("expected no drift within TTL (cache hit), but got drifted=true")
	}

	// Remove the cache entry to force a fresh fetch.
	entries, _ := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	for _, e := range entries {
		os.Remove(e) //nolint:errcheck
	}

	// After cache clear: CheckDrift detects the change.
	drifted, _, err = r.CheckDrift(citeStr)
	if err != nil {
		t.Fatalf("CheckDrift (after clear): %v", err)
	}
	if !drifted {
		t.Error("expected drift after cache clear, but got drifted=false")
	}
}

// TestCache_MalformedCacheEntryIsMiss verifies that a malformed JSON cache
// file is treated as a miss (not an error), triggering a re-fetch, and that
// the entry is rewritten with valid JSON after the fresh fetch.
func TestCache_MalformedCacheEntryIsMiss(t *testing.T) {
	body := "test content\n"
	handler, reqs := newCounter(&body)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	cfg := loadCfg(t, "", "")
	// Default TTL (24h) via effectiveCacheTTL.
	r := resolverWithCache(cfg, t.TempDir(), cacheDir)

	// Write a malformed JSON file at the expected cache path.
	h := sha256.Sum256([]byte(srv.URL))
	entryPath := filepath.Join(cacheDir, fmt.Sprintf("%x.json", h))
	if err := os.WriteFile(entryPath, []byte("not-json{{{"), 0o644); err != nil {
		t.Fatalf("write malformed entry: %v", err)
	}

	// Read should treat the malformed entry as a miss and fetch.
	c := cite.Citation{File: srv.URL, Start: 1, End: 1}
	_, _, err := r.Read(c)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if atomic.LoadInt64(reqs) != 1 {
		t.Errorf("want 1 fetch (malformed entry = miss), got %d", atomic.LoadInt64(reqs))
	}

	// The cache entry must now be valid JSON (was rewritten after the fresh fetch).
	data, readErr := os.ReadFile(entryPath)
	if readErr != nil {
		t.Fatalf("read rewritten entry: %v", readErr)
	}

	var rewritten map[string]any
	if jsonErr := json.Unmarshal(data, &rewritten); jsonErr != nil {
		t.Fatalf("rewritten entry is not valid JSON: %v; data: %s", jsonErr, data)
	}
	if locator, _ := rewritten["locator"].(string); locator != srv.URL {
		t.Errorf("rewritten entry locator = %q, want %q", locator, srv.URL)
	}
}
