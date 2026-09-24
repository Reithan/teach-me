package source

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// defaultCacheTTL is the TTL used when neither TM_CACHE_TTL nor cache-ttl is set.
const defaultCacheTTL = 24 * time.Hour

// cacheEntry is the JSON structure stored in one cache file.
type cacheEntry struct {
	Locator          string    `json:"locator"`
	FinalURL         string    `json:"final_url"`
	MIME             string    `json:"mime"`
	Converter        string    `json:"converter"`         // command words joined by space; empty for raw
	ConverterVersion string    `json:"converter_version"` // empty for raw
	FetchedAt        time.Time `json:"fetched_at"`        // RFC3339 UTC
	Text             string    `json:"text"`
}

// cacheKey returns the hex-encoded SHA-256 of the locator string.
// The key is the locator as written in the citation (before any redirect or alias
// resolution), so that a different URL always produces a different key.
func cacheKey(locator string) string {
	h := sha256.Sum256([]byte(locator))
	return fmt.Sprintf("%x", h)
}

// cachePath returns the full path of the cache entry file for locator.
func cachePath(cacheDir, locator string) string {
	return filepath.Join(cacheDir, cacheKey(locator)+".json")
}

// readCacheEntry reads and decodes the cache entry at path.
// Returns (nil, nil) when the file does not exist.
func readCacheEntry(path string) (*cacheEntry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var e cacheEntry
	if jsonErr := json.Unmarshal(data, &e); jsonErr != nil {
		return nil, jsonErr
	}
	return &e, nil
}

// writeCacheEntry writes entry to path atomically (temp file + rename).
// A write failure is silently ignored by callers; this function returns the
// error so callers can choose to log it if desired.
func writeCacheEntry(path string, entry *cacheEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "cache-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()        //nolint:errcheck
		os.Remove(tmpName) //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName) //nolint:errcheck
		return err
	}
	return os.Rename(tmpName, path)
}

// isCacheHit reports whether entry is a valid hit for the given TTL, current
// converter command (the program name), and current converter version.
// A hit requires: age within TTL, and converter/version equal what the current
// config would use for the entry's MIME type.
func isCacheHit(entry *cacheEntry, now time.Time, ttl time.Duration, converterCmds []string, converterVersion string) bool {
	if ttl <= 0 {
		return false
	}
	age := now.Sub(entry.FetchedAt)
	if age < 0 || age > ttl {
		return false
	}
	// Converter and version must match the current config's choice.
	wantConverter := ""
	if len(converterCmds) > 0 {
		wantConverter = strings.Join(converterCmds, " ")
	}
	if entry.Converter != wantConverter {
		return false
	}
	if entry.ConverterVersion != converterVersion {
		return false
	}
	return true
}

// effectiveCacheTTL resolves the TTL for r: TM_CACHE_TTL env overrides per call,
// then cfg.CacheTTL (when explicitly set), then the built-in default of 24h.
// Returns 0 when the cache is explicitly disabled (TM_CACHE_TTL=0 or cache-ttl=0).
func (r *Resolver) effectiveCacheTTL() time.Duration {
	// env override: even "0" is valid (disable cache for this call)
	if v := os.Getenv("TM_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		// bad env value: treat as "not set"
	}
	// config-file value: use it only when explicitly set (CacheTTLSet=true),
	// because the zero value of time.Duration is the same as "disabled".
	if r.Cfg.CacheTTLSet {
		return r.Cfg.CacheTTL
	}
	return defaultCacheTTL
}
