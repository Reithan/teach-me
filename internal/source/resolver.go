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
	Commit           string    // git commit SHA (for path locators inside a repo)
	URL              string    // final URL after redirects (for URI locators)
	MIME             string    // MIME type (when a converter was used or for URIs)
	Converter        string    // converter program name
	ConverterVersion string    // first line of the converter's version output
	FetchedAt        time.Time // when the URI was fetched (zero if not fetched)
}

// Resolver resolves citations through converters, URIs, and git HEAD.
// Create one per command invocation via NewResolver.
type Resolver struct {
	Cfg     *Config
	SrcRoot string

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
	return &Resolver{
		Cfg:      cfg,
		SrcRoot:  cite.SrcRoot(graphDir),
		verified: make(map[string]versionEntry),
	}, nil
}

// NewResolverWithConfig creates a Resolver from an already-loaded Config and srcRoot.
// Used in tests and when the caller needs explicit control over config paths.
func NewResolverWithConfig(cfg *Config, srcRoot string) *Resolver {
	return &Resolver{
		Cfg:      cfg,
		SrcRoot:  srcRoot,
		verified: make(map[string]versionEntry),
	}
}

// Read resolves citation c, applies any configured converter, and returns the
// text of lines [c.Start, c.End] along with metadata. The text is suitable for
// hashing (Normalize is not applied here; cite.Hash calls it internally).
func (r *Resolver) Read(c cite.Citation) (string, Meta, error) {
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
// Path resolution
// ──────────────────────────────────────────────────────────────────────────────

func (r *Resolver) readPath(c cite.Citation) (string, Meta, error) {
	path := cite.Resolve(c, r.SrcRoot)

	// Determine MIME from extension (for converter lookup).
	ext := strings.ToLower(filepath.Ext(path))
	mime := r.Cfg.ExtToMIME(ext)

	// Commit recording: read HEAD by file reads (no exec).
	commit := CommitForPath(path)

	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		// File missing — try git HEAD blob; on any failure return a structured refusal.
		if os.IsNotExist(readErr) {
			text, meta, gitErr := r.tryGitBlob(c, path, commit)
			if gitErr == nil {
				return text, meta, nil
			}
			// Return the structured refusal (RefusalError) rather than the raw
			// "no such file" error so the caller gets actionable guidance.
			return "", Meta{Commit: commit}, gitErr
		}
		return "", Meta{Commit: commit}, fmt.Errorf("citation %q: %w", c.File, readErr)
	}

	return r.convertAndSlice(c, raw, mime, commit)
}

// tryGitBlob attempts to retrieve a missing file from git HEAD.
// If git is not configured or the blob doesn't exist, it returns an error
// with the §13.1 refusal text.
func (r *Resolver) tryGitBlob(c cite.Citation, absPath, commit string) (string, Meta, error) {
	// FindGitRepo walks up from absPath (handling nonexistent paths via Stat).
	repoDir, _ := FindGitRepo(absPath)

	configPath := r.Cfg.ConfigPath

	notInTree := &RefusalError{
		Err: fmt.Sprintf("%s is not in the working tree", c.File),
		Fix: fmt.Sprintf("check it out, or set git in %s to read it from HEAD", configPath),
	}

	if repoDir == "" || r.Cfg.Git == "" {
		// Not in a repo, or git not configured.
		return "", Meta{}, notInTree
	}

	// Compute repo-relative path.
	relpath, relErr := filepath.Rel(repoDir, absPath)
	if relErr != nil {
		relpath = absPath
	}
	// Use forward slashes for git.
	relpath = filepath.ToSlash(relpath)

	raw, blobErr := HeadBlob(r.Cfg.Git, repoDir, relpath)
	if blobErr != nil {
		return "", Meta{}, notInTree
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	mime := r.Cfg.ExtToMIME(ext)

	text, meta, err := r.convertAndSlice(c, raw, mime, commit)
	return text, meta, err
}

// ──────────────────────────────────────────────────────────────────────────────
// URI resolution
// ──────────────────────────────────────────────────────────────────────────────

func (r *Resolver) readURI(c cite.Citation) (string, Meta, error) {
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
		if cmds := r.Cfg.ConverterFor(ctMIME); len(cmds) > 0 {
			mime, convCmds = ctMIME, cmds
		} else if isRawMIME(extMIME) {
			// CT has no converter; URL ext maps to a raw type.
			mime, isRaw = extMIME, true
		} else if extMIME != "" {
			if cmds := r.Cfg.ConverterFor(extMIME); len(cmds) > 0 {
				mime, convCmds = extMIME, cmds
			} else {
				return "", Meta{URL: finalURL, MIME: ctMIME, FetchedAt: fetchedAt},
					r.noConverterErr(c.File, ctMIME)
			}
		} else {
			return "", Meta{URL: finalURL, MIME: ctMIME, FetchedAt: fetchedAt},
				r.noConverterErr(c.File, ctMIME)
		}
	default:
		// No Content-Type: fall back to URL extension.
		if isRawMIME(extMIME) {
			mime, isRaw = extMIME, true
		} else if extMIME != "" {
			if cmds := r.Cfg.ConverterFor(extMIME); len(cmds) > 0 {
				mime, convCmds = extMIME, cmds
			} else {
				// Extension known but no converter: treat as plain text.
				isRaw = true
			}
		} else {
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
		Fix: "save a static copy under TM_SRC_ROOT and cite it",
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
