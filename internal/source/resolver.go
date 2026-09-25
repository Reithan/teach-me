package source

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/reithan/teach-me/internal/cite"
)

// RefusalError is a structured err:/fix: refusal from the resolver.
// CLI handlers should detect this type and call writeErrFix(e.Err, e.Fix)
// directly, without wrapping the message in "citation <cite>: ...".
type RefusalError struct {
	Err string
	Fix string
}

func (e *RefusalError) Error() string {
	if e.Fix != "" {
		return e.Err + "\n" + e.Fix
	}
	return e.Err
}

const (
	fetchTimeout = 30 * time.Second
	fetchSizeMax = 16 << 20 // 16 MiB

	// rawTextMIME lists MIME types that are read as plain text without conversion.
	mimeTextPlain    = "text/plain"
	mimeTextMarkdown = "text/markdown"
)

// Meta holds metadata from a resolved source, optionally written to the event log.
type Meta struct {
	Commit           string    // git commit SHA: HEAD for plain paths (PR 1), resolved SHA for git: file-at-ref
	Ref              string    // ref as typed for git: locators (e.g. "main" or "a..b"); not set for other kinds
	URL              string    // final URL after redirects (for URI locators)
	MIME             string    // MIME type (when a converter was used or for URIs)
	Converter        string    // converter program name
	ConverterVersion string    // first line of the converter's version output
	FetchedAt        time.Time // when the URI was fetched (zero if not fetched)
	// ResolvedLocator is the SHA-pinned locator for git: citations.
	// Set by readGit; used by HashCitation to store the pinned form; not logged.
	ResolvedLocator string
}

// Resolver resolves citations through converters, URIs, and git HEAD.
// Create one per command invocation via NewResolver.
type Resolver struct {
	Cfg      *Config
	SrcRoot  string
	GraphDir string // graph file directory, used for aids-dir resolution
	CacheDir string // empty disables the cache (e.g. when UserCacheDir errors)

	mu       sync.Mutex
	verified map[string]versionEntry // program → verified entry
}

type versionEntry struct {
	firstLine string // first line of combined version output
	err       error  // non-nil on mismatch or exec failure
}

// NewResolver creates a Resolver for the given graph directory.
// It loads source config from the user config file and .tmconfig.
func NewResolver(graphDir string) (*Resolver, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	cacheDir := resolverCacheDir()
	return &Resolver{
		Cfg:      cfg,
		SrcRoot:  cite.SrcRoot(graphDir),
		GraphDir: graphDir,
		CacheDir: cacheDir,
		verified: make(map[string]versionEntry),
	}, nil
}

// NewResolverWithConfig creates a Resolver from an already-loaded Config and srcRoot.
// Used in tests and when the caller needs explicit control over config paths.
// CacheDir defaults to the user cache dir; callers may override it directly after construction.
func NewResolverWithConfig(cfg *Config, srcRoot string) *Resolver {
	return &Resolver{
		Cfg:      cfg,
		SrcRoot:  srcRoot,
		CacheDir: resolverCacheDir(),
		verified: make(map[string]versionEntry),
	}
}

// resolverCacheDir returns the default cache directory for a Resolver.
// TM_CACHE_DIR overrides the location (useful for tests and isolated runs).
// Returns "" when the directory cannot be determined (disables the cache).
func resolverCacheDir() string {
	if v := os.Getenv("TM_CACHE_DIR"); v != "" {
		return v
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "tm")
}

// Read resolves citation c, applies any configured converter, and returns the
// text of lines [c.Start, c.End] along with metadata. The text is suitable for
// hashing (Normalize is not applied here; cite.Hash calls it internally).
func (r *Resolver) Read(c cite.Citation) (string, Meta, error) {
	if cite.IsGit(c.File) {
		return r.readGit(c)
	}
	if cite.IsURI(c.File) {
		return r.readURI(c)
	}
	return r.readPath(c)
}

// HashCitation is the source-aware replacement for cite.HashCitation.
// It routes through Read so that converters, URIs, and git HEAD are applied.
// Returns the canonical hashed citation string and metadata.
func (r *Resolver) HashCitation(citeStr string) (string, Meta, error) {
	// Percent-encode raw '"' before parsing (see cite.HashCitation).
	encoded := strings.ReplaceAll(citeStr, `"`, "%22")

	c, err := cite.Parse(encoded)
	if err != nil {
		return "", Meta{}, err
	}

	text, meta, err := r.Read(c)
	if err != nil {
		return "", meta, err
	}

	computed := cite.Hash(text)
	if c.Hash != "" && c.Hash != computed {
		return "", meta, fmt.Errorf("hash mismatch (stored %s, file hashes to %s)", c.Hash, computed)
	}

	c.Hash = computed
	// For git: locators, replace the locator with the SHA-pinned form so that
	// every ref is resolved to a 12-hex SHA on write (§4.4, §13.1).
	if cite.IsGit(c.File) && meta.ResolvedLocator != "" {
		c.File = meta.ResolvedLocator
	}
	return cite.Format(c), meta, nil
}

// CheckDrift is the source-aware replacement for cite.CheckDrift.
// Returns (drifted, meta, error). A hashless citation is never drifted.
// A read error is returned as an error, not as drift.
func (r *Resolver) CheckDrift(citeStr string) (bool, Meta, error) {
	c, err := cite.Parse(citeStr)
	if err != nil {
		return false, Meta{}, err
	}
	if c.Hash == "" {
		return false, Meta{}, nil
	}

	text, meta, err := r.Read(c)
	if err != nil {
		return false, meta, err
	}

	computed := cite.Hash(text)
	return computed != c.Hash, meta, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// git: locator resolution
// ──────────────────────────────────────────────────────────────────────────────

// readGit resolves a git: locator. It runs the appropriate git command and
// slices the output to the line range in c.
func (r *Resolver) readGit(c cite.Citation) (string, Meta, error) {
	gl, parseErr := cite.ParseGit(c.File)
	if parseErr != nil {
		return "", Meta{}, &RefusalError{Err: parseErr.Error()}
	}

	// Alias lookup.
	repoPath, ok := r.Cfg.Repos[gl.Alias]
	if !ok {
		return "", Meta{}, &RefusalError{
			Err: fmt.Sprintf("unknown repo alias %s", gl.Alias),
			Fix: fmt.Sprintf("tm repo add %s <path>", gl.Alias),
		}
	}

	// git executable required.
	if r.Cfg.Git == "" {
		return "", Meta{}, &RefusalError{
			Err: "git: locator needs git",
			Fix: fmt.Sprintf("set git in %s", r.Cfg.ConfigPath),
		}
	}

	// Resolve a ref to a 12-hex SHA via rev-parse.
	resolveRef := func(ref string) (string, error) {
		// Refuse refs that begin with "-": git would interpret them as options.
		if strings.HasPrefix(ref, "-") {
			return "", &RefusalError{
				Err: "ref must not start with -",
				Fix: "use git:<alias>@<ref>[:<path>] where ref is a branch, tag, or SHA",
			}
		}
		cmd := exec.Command(r.Cfg.Git, "-C", repoPath, "rev-parse", "--verify", "--short=12", ref+"^{commit}") //nolint:gosec
		out, err := cmd.Output()
		if err != nil {
			return "", &RefusalError{
				Err: fmt.Sprintf("%s does not resolve in %s", ref, gl.Alias),
				Fix: fmt.Sprintf("check that the ref exists; register the repo with tm repo add %s <path>", gl.Alias),
			}
		}
		return strings.TrimSpace(string(out)), nil
	}

	sha, err := resolveRef(gl.Ref)
	if err != nil {
		return "", Meta{}, err
	}
	var shaB string
	if gl.RefB != "" {
		shaB, err = resolveRef(gl.RefB)
		if err != nil {
			return "", Meta{}, err
		}
	}

	// Build meta with Ref (as typed) and the SHA-pinned locator.
	refStr := gl.Ref
	if gl.RefB != "" {
		refStr = gl.Ref + ".." + gl.RefB
	}
	meta := Meta{Ref: refStr}

	pinnedGL := cite.GitLocator{Alias: gl.Alias, Ref: sha, RefB: shaB, Path: gl.Path}
	meta.ResolvedLocator = cite.FormatGit(pinnedGL)

	// Set Commit only for file-at-ref (the only form with a blob to pin).
	if gl.RefB == "" && gl.Path != "" {
		meta.Commit = sha
	}

	// Run the git command with a timeout and size cap like URIs.
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	gitArgs := []string{"-C", repoPath, "-c", "core.pager=cat", "-c", "color.ui=false"}
	var raw []byte
	var runErr error

	switch {
	case gl.RefB == "" && gl.Path != "":
		// File at ref: git cat-file -p <sha>:<path>
		args := append(gitArgs, "cat-file", "-p", sha+":"+filepath.ToSlash(gl.Path)) //nolint:gocritic
		raw, runErr = exec.CommandContext(ctx, r.Cfg.Git, args...).Output()          //nolint:gosec
	case gl.RefB == "" && gl.Path == "":
		// Commit: git show <sha>
		args := append(gitArgs, "show", "--no-ext-diff", "--no-textconv", sha) //nolint:gocritic
		raw, runErr = exec.CommandContext(ctx, r.Cfg.Git, args...).Output()    //nolint:gosec
	case gl.RefB != "" && gl.Path == "":
		// Diff: git diff <sha_a> <sha_b>
		args := append(gitArgs, "diff", "--no-ext-diff", "--no-textconv", sha, shaB) //nolint:gocritic
		raw, runErr = exec.CommandContext(ctx, r.Cfg.Git, args...).Output()          //nolint:gosec
	default:
		// Diff for path: git diff <sha_a> <sha_b> -- <path>
		args := append(gitArgs, "diff", "--no-ext-diff", "--no-textconv", sha, shaB, "--", filepath.ToSlash(gl.Path)) //nolint:gocritic
		raw, runErr = exec.CommandContext(ctx, r.Cfg.Git, args...).Output()                                           //nolint:gosec
	}

	if runErr != nil {
		return "", meta, &RefusalError{
			Err: fmt.Sprintf("git command failed for %s: %v", c.File, runErr),
		}
	}
	if int64(len(raw)) > fetchSizeMax {
		return "", meta, &RefusalError{
			Err: fmt.Sprintf("git output for %s exceeds 16 MiB size cap", c.File),
		}
	}

	// File-at-ref: apply converter for the path extension, then slice.
	if gl.RefB == "" && gl.Path != "" {
		ext := strings.ToLower(filepath.Ext(gl.Path))
		mime := r.Cfg.ExtToMIME(ext)
		text, convMeta, convErr := r.convertAndSlice(c, raw, mime, "")
		if convErr != nil {
			return "", meta, convErr
		}
		// Merge converter-specific fields from convMeta into meta.
		meta.MIME = convMeta.MIME
		meta.Converter = convMeta.Converter
		meta.ConverterVersion = convMeta.ConverterVersion
		return text, meta, nil
	}

	// Commit/diff/diff-for-path: slice raw.
	text, sliceErr := sliceLines(raw, c)
	if sliceErr != nil {
		return "", meta, fmt.Errorf("citation %q: %w", c.File, sliceErr)
	}
	return text, meta, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Path resolution
// ──────────────────────────────────────────────────────────────────────────────

func (r *Resolver) readPath(c cite.Citation) (string, Meta, error) {
	path := cite.Resolve(c, r.SrcRoot)

	// Refuse if the resolved path is inside the aids directory (§4.6 rule 5).
	if r.GraphDir != "" {
		aidsDir := AidsDir(r.Cfg, r.GraphDir)
		absPath := filepath.Clean(path)
		if isUnderDir(absPath, aidsDir) {
			return "", Meta{}, &RefusalError{
				Err: fmt.Sprintf("%s is an aid, not a source", c.File),
				Fix: fmt.Sprintf("cite the primary source; link the aid with tm aid <id> %s", c.File),
			}
		}
	}

	// Determine MIME from extension (for converter lookup).
	ext := strings.ToLower(filepath.Ext(path))
	mime := r.Cfg.ExtToMIME(ext)

	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", Meta{}, &RefusalError{
				Err: fmt.Sprintf("%s is not in the working tree", c.File),
				Fix: "cite it as git:<alias>@<ref>:<path> if it is committed",
			}
		}
		return "", Meta{}, fmt.Errorf("citation %q: %w", c.File, readErr)
	}

	return r.convertAndSlice(c, raw, mime, "")
}

// ──────────────────────────────────────────────────────────────────────────────
// URI resolution
// ──────────────────────────────────────────────────────────────────────────────

func (r *Resolver) readURI(c cite.Citation) (string, Meta, error) {
	ttl := r.effectiveCacheTTL()

	// ── Cache read ────────────────────────────────────────────────────────────
	// When cache is enabled and the entry is a hit, skip the fetch entirely.
	if ttl > 0 && r.CacheDir != "" {
		entryPath := cachePath(r.CacheDir, c.File)
		if entry, _ := readCacheEntry(entryPath); entry != nil {
			// Resolve what converter/version the current config would use for
			// the entry's MIME. A changed converter or version is a miss.
			converterCmds := r.Cfg.ConverterFor(entry.MIME)
			converterVer := ""
			if len(converterCmds) > 0 {
				if ver, verErr := r.checkVersion(converterCmds[0]); verErr == nil {
					converterVer = ver
				}
				// verErr != nil → version changed or converter missing → miss
			}
			if isCacheHit(entry, time.Now().UTC(), ttl, converterCmds, converterVer) {
				text, sliceErr := sliceLines([]byte(entry.Text), c)
				if sliceErr != nil {
					return "", Meta{}, fmt.Errorf("citation %q: %w", c.File, sliceErr)
				}
				meta := Meta{
					URL:       entry.FinalURL,
					MIME:      entry.MIME,
					FetchedAt: entry.FetchedAt,
				}
				if len(converterCmds) > 0 {
					meta.Converter = converterCmds[0]
					meta.ConverterVersion = converterVer
				}
				return text, meta, nil
			}
		}
	}

	// ── Full fetch ────────────────────────────────────────────────────────────
	fetchedAt := time.Now().UTC()

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.File, nil)
	if err != nil {
		return "", Meta{}, r.fetchFailure(c.File, err.Error())
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", Meta{}, r.fetchFailure(c.File, err.Error())
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", Meta{}, r.fetchFailure(c.File, resp.Status)
	}

	// Record the final URL after redirects.
	finalURL := resp.Request.URL.String()

	// Determine MIME and converter.
	// §13.1: converter matched by MIME type first, then by URL path extension.
	ctMIME := stripMIMEParams(resp.Header.Get("Content-Type"))
	urlExt := strings.ToLower(filepath.Ext(resp.Request.URL.Path))
	extMIME := r.Cfg.ExtToMIME(urlExt)

	isRawMIME := func(m string) bool {
		return m == mimeTextPlain || m == mimeTextMarkdown
	}

	var mime string
	var isRaw bool
	var convCmds []string

	switch {
	case isRawMIME(ctMIME):
		mime, isRaw = ctMIME, true
	case ctMIME != "":
		ctCmds := r.Cfg.ConverterFor(ctMIME)
		switch {
		case len(ctCmds) > 0:
			mime, convCmds = ctMIME, ctCmds
		case isRawMIME(extMIME):
			// CT has no converter; URL ext maps to a raw type.
			mime, isRaw = extMIME, true
		case extMIME != "":
			if cmds := r.Cfg.ConverterFor(extMIME); len(cmds) > 0 {
				mime, convCmds = extMIME, cmds
			} else {
				return "", Meta{URL: finalURL, MIME: ctMIME, FetchedAt: fetchedAt},
					r.noConverterErr(c.File, ctMIME)
			}
		default:
			return "", Meta{URL: finalURL, MIME: ctMIME, FetchedAt: fetchedAt},
				r.noConverterErr(c.File, ctMIME)
		}
	default:
		// No Content-Type: fall back to URL extension.
		switch {
		case isRawMIME(extMIME):
			mime, isRaw = extMIME, true
		case extMIME != "":
			if cmds := r.Cfg.ConverterFor(extMIME); len(cmds) > 0 {
				mime, convCmds = extMIME, cmds
			} else {
				// Extension known but no converter: treat as plain text.
				isRaw = true
			}
		default:
			// No CT, no extension: treat as plain text.
			isRaw = true
		}
	}

	// Read with size cap.
	lr := io.LimitReader(resp.Body, fetchSizeMax+1)
	raw, readErr := io.ReadAll(lr)
	if readErr != nil {
		return "", Meta{}, r.fetchFailure(c.File, readErr.Error())
	}
	if int64(len(raw)) > fetchSizeMax {
		return "", Meta{}, r.fetchFailure(c.File, "response exceeds 16 MiB size cap")
	}

	var converted []byte
	var converterName, converterVer string

	if !isRaw {
		cv, verLine, convErr := r.runConverter(convCmds, raw)
		if convErr != nil {
			return "", Meta{URL: finalURL, MIME: mime, FetchedAt: fetchedAt}, convErr
		}
		converted = cv
		converterName = convCmds[0]
		converterVer = verLine
	} else {
		converted = raw
	}

	meta := Meta{
		URL:       finalURL,
		MIME:      mime,
		FetchedAt: fetchedAt,
	}
	if converterName != "" {
		meta.Converter = converterName
		meta.ConverterVersion = converterVer
	}

	// ── Cache write ───────────────────────────────────────────────────────────
	// Store the full (unsliced) converted text. Write failures are ignored;
	// the cache is regenerable cost, not state.
	if ttl > 0 && r.CacheDir != "" {
		converterStr := ""
		if len(convCmds) > 0 {
			converterStr = strings.Join(convCmds, " ")
		}
		entry := &cacheEntry{
			Locator:          c.File,
			FinalURL:         finalURL,
			MIME:             mime,
			Converter:        converterStr,
			ConverterVersion: converterVer,
			FetchedAt:        fetchedAt,
			Text:             string(converted),
		}
		entryPath := cachePath(r.CacheDir, c.File)
		_ = writeCacheEntry(entryPath, entry) // ignore write error
	}

	text, sliceErr := sliceLines(converted, c)
	if sliceErr != nil {
		return "", meta, fmt.Errorf("citation %q: %w", c.File, sliceErr)
	}
	return text, meta, nil
}

// fetchFailure returns the §13.1 refusal for a failed fetch.
func (r *Resolver) fetchFailure(url, reason string) error {
	return &RefusalError{
		Err: fmt.Sprintf("fetch %s failed: %s", url, reason),
		Fix: "retry when egress is available, or ask the learner for a copy and cite the copy as a plain path",
	}
}

// noConverterErr returns the §13.1 refusal for an unknown MIME type.
func (r *Resolver) noConverterErr(url, mime string) error {
	return &RefusalError{
		Err: fmt.Sprintf("fetch %s failed: no converter for %s", url, mime),
		Fix: fmt.Sprintf("add a convert %s line to %s", mime, r.Cfg.ConfigPath),
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Converter execution
// ──────────────────────────────────────────────────────────────────────────────

// runConverter checks the version pin, then runs the converter with raw on
// stdin. Returns (output bytes, version first line, error).
func (r *Resolver) runConverter(cmds []string, raw []byte) ([]byte, string, error) {
	program := cmds[0]

	verLine, verErr := r.checkVersion(program)
	if verErr != nil {
		return nil, "", verErr
	}

	cmd := exec.Command(cmds[0], cmds[1:]...) //nolint:gosec
	cmd.Stdin = bytes.NewReader(raw)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Non-zero exit: refuse with the converter's stderr (first line).
		firstLine := firstLineOf(stderr.String())
		if firstLine == "" {
			firstLine = err.Error()
		}
		return nil, "", &RefusalError{
			Err: fmt.Sprintf("converter %s: %s", program, firstLine),
		}
	}

	return stdout.Bytes(), verLine, nil
}

// checkVersion checks the version pin for program, caching the result.
// Returns the first line of combined version output on success, or an error.
func (r *Resolver) checkVersion(program string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry, ok := r.verified[program]; ok {
		return entry.firstLine, entry.err
	}

	pin, hasPin := r.Cfg.Versions[program]
	if !hasPin {
		// A convert key whose program has no version line is a config error.
		err := &RefusalError{
			Err: fmt.Sprintf("converter %s: no version pin configured", program),
			Fix: fmt.Sprintf("add a version %s = <string> line to %s", program, r.Cfg.ConfigPath),
		}
		r.verified[program] = versionEntry{err: err}
		return "", err
	}

	verCmds := r.Cfg.VersionCmd(program)
	cmd := exec.Command(verCmds[0], verCmds[1:]...) //nolint:gosec
	combined, _ := cmd.CombinedOutput()             // ignore error; we only need the output

	firstLine := firstLineOf(string(combined))

	var entry versionEntry
	entry.firstLine = firstLine

	if !strings.Contains(firstLine, pin) {
		entry.err = &RefusalError{
			Err: fmt.Sprintf("%s is %s, config pins %s", program, firstLine, pin),
			Fix: fmt.Sprintf("set version %s = %s in %s; citations made under %s may drift",
				program, firstLine, r.Cfg.ConfigPath, pin),
		}
	}

	r.verified[program] = entry
	return entry.firstLine, entry.err
}

// convertAndSlice converts raw bytes with the appropriate converter for mime
// (or reads raw if mime is empty/"text/plain"/"text/markdown"), then slices
// to the line range in c. Returns the text and metadata.
func (r *Resolver) convertAndSlice(c cite.Citation, raw []byte, mime string, commit string) (string, Meta, error) {
	meta := Meta{Commit: commit}

	isRaw := mime == "" || mime == mimeTextPlain || mime == mimeTextMarkdown

	var content []byte
	if !isRaw {
		cmds := r.Cfg.ConverterFor(mime)
		if len(cmds) == 0 {
			// No converter for this extension — read as plain text.
			content = raw
		} else {
			cv, verLine, convErr := r.runConverter(cmds, raw)
			if convErr != nil {
				return "", meta, convErr
			}
			content = cv
			meta.MIME = mime
			meta.Converter = cmds[0]
			meta.ConverterVersion = verLine
		}
	} else {
		content = raw
		if mime != "" {
			meta.MIME = mime
		}
	}

	text, sliceErr := sliceLines(content, c)
	if sliceErr != nil {
		return "", meta, fmt.Errorf("citation %q: %w", c.File, sliceErr)
	}
	return text, meta, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// sliceLines slices lines [c.Start, c.End] inclusive from content.
// Content processing mirrors cite.ReadRange: single trailing newline trimmed.
func sliceLines(content []byte, c cite.Citation) (string, error) {
	s := strings.TrimSuffix(string(content), "\n")
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")

	if c.Start < 1 {
		return "", fmt.Errorf("line numbers must be positive")
	}
	if c.End > len(lines) {
		return "", fmt.Errorf("lines %d-%d out of bounds (file has %d lines)",
			c.Start, c.End, len(lines))
	}
	return strings.Join(lines[c.Start-1:c.End], "\n"), nil
}

// firstLineOf returns the first non-empty line of s, or s itself if no newline.
func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}

// isUnderDir reports whether path is inside (or equal to) dir.
// Both paths must be cleaned absolute paths. The comparison is case-sensitive.
func isUnderDir(path, dir string) bool {
	cleanDir := filepath.Clean(dir)
	if path == cleanDir {
		return true
	}
	return strings.HasPrefix(path, cleanDir+string(filepath.Separator))
}
