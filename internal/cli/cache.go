package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// cacheRun is the Run handler for:
//
//	tm cache clear
//	tm cache list
func cacheRun(ctx *Context) int {
	sub := ctx.Positionals[0]
	switch sub {
	case "clear":
		return cacheClear(ctx)
	case "list":
		return cacheList(ctx)
	default:
		ctx.ErrMsg = fmt.Sprintf("unknown cache subcommand %q", sub)
		ctx.FixMsg = FindCommand("cache").Usage()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}
}

// cacheDir returns the cache directory.
// TM_CACHE_DIR overrides the location (matches the resolver's resolverCacheDir).
func cacheDir() (string, error) {
	if v := os.Getenv("TM_CACHE_DIR"); v != "" {
		return v, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine cache dir: %v", err)
	}
	return filepath.Join(base, "tm"), nil
}

// cacheClear handles `tm cache clear`: removes every *.json entry under the
// cache dir and prints "ok".
func cacheClear(ctx *Context) int {
	dir, err := cacheDir()
	if err != nil {
		ctx.ErrMsg = err.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	entries, readErr := os.ReadDir(dir)
	if os.IsNotExist(readErr) {
		// Cache dir does not exist yet: nothing to clear.
		_, _ = fmt.Fprintln(ctx.Out, "ok")
		return 0
	}
	if readErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot read cache dir: %v", readErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if rmErr := os.Remove(p); rmErr != nil && !os.IsNotExist(rmErr) {
			ctx.ErrMsg = fmt.Sprintf("cannot remove %s: %v", p, rmErr)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
			return 3
		}
	}

	_, _ = fmt.Fprintln(ctx.Out, "ok")
	return 0
}

// cacheListEntry is the decoded subset of a cache entry for listing.
type cacheListEntry struct {
	Locator   string    `json:"locator"`
	Converter string    `json:"converter"`
	FetchedAt time.Time `json:"fetched_at"`
}

// cacheList handles `tm cache list`: prints one line per entry sorted by
// locator: "<locator> <fetched_at> <converter or ->".
func cacheList(ctx *Context) int {
	dir, err := cacheDir()
	if err != nil {
		ctx.ErrMsg = err.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	entries, readErr := os.ReadDir(dir)
	if os.IsNotExist(readErr) {
		// Empty cache: print nothing.
		return 0
	}
	if readErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot read cache dir: %v", readErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	type row struct {
		locator   string
		fetchedAt time.Time
		converter string
	}
	var rows []row

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, readFileErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readFileErr != nil {
			continue // skip unreadable entries
		}
		var le cacheListEntry
		if jsonErr := json.Unmarshal(data, &le); jsonErr != nil {
			continue // skip malformed entries
		}
		conv := le.Converter
		if conv == "" {
			conv = "-"
		}
		rows = append(rows, row{locator: le.Locator, fetchedAt: le.FetchedAt, converter: conv})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].locator < rows[j].locator
	})

	for _, r := range rows {
		_, _ = fmt.Fprintf(ctx.Out, "%s %s %s\n",
			r.locator,
			r.fetchedAt.UTC().Format(time.RFC3339),
			r.converter,
		)
	}
	return 0
}
