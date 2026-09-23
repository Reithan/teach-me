package source_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/source"
)

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// writeScript writes a shell script to dir/<name>.sh, makes it executable,
// and returns the full path.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name+".sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writeScript: %v", err)
	}
	return path
}

// newConfig returns a Config with the given user config file and no .tmconfig.
func newConfig(t *testing.T, userCfgContent string) *source.Config {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	if userCfgContent != "" {
		if err := os.WriteFile(cfgPath, []byte(userCfgContent), 0o644); err != nil {
			t.Fatalf("newConfig: %v", err)
		}
	}
	cfg, err := source.LoadConfigPaths(cfgPath, filepath.Join(dir, "nofile"))
	if err != nil {
		t.Fatalf("newConfig LoadConfigPaths: %v", err)
	}
	return cfg
}

// newConfigFromFiles returns a Config loading from both a user config file and
// a .tmconfig. Either path may be "".
func newConfigFromFiles(t *testing.T, userCfgContent, tmcfgContent string) *source.Config {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	tmcfgPath := filepath.Join(dir, ".tmconfig")

	if userCfgContent != "" {
		if err := os.WriteFile(cfgPath, []byte(userCfgContent), 0o644); err != nil {
			t.Fatalf("newConfigFromFiles user config: %v", err)
		}
	}
	if tmcfgContent != "" {
		if err := os.WriteFile(tmcfgPath, []byte(tmcfgContent), 0o644); err != nil {
			t.Fatalf("newConfigFromFiles tmconfig: %v", err)
		}
	}

	cfg, err := source.LoadConfigPaths(cfgPath, tmcfgPath)
	if err != nil {
		t.Fatalf("newConfigFromFiles LoadConfigPaths: %v", err)
	}
	return cfg
}

// resolverFrom creates a Resolver with the given config and srcRoot.
func resolverFrom(cfg *source.Config, srcRoot string) *source.Resolver {
	return source.NewResolverWithConfig(cfg, srcRoot)
}

// writeTempFile writes content to a file in dir and returns the path.
func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
	return path
}

// makeCitation creates a hashless Citation struct for tests.
func makeCitation(file string, start, end int) cite.Citation {
	return cite.Citation{File: file, Start: start, End: end}
}

// ──────────────────────────────────────────────────────────────────────────────
// Config parsing
// ──────────────────────────────────────────────────────────────────────────────

func TestLoadConfig_Empty(t *testing.T) {
	dir := t.TempDir()
	cfg, err := source.LoadConfigPaths(
		filepath.Join(dir, "noconfig"),
		filepath.Join(dir, "notmconfig"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Git != "" {
		t.Errorf("Git should be empty, got %q", cfg.Git)
	}
	if len(cfg.Converters) != 0 {
		t.Errorf("Converters should be empty, got %v", cfg.Converters)
	}
}

func TestLoadConfig_ParsesAllKeys(t *testing.T) {
	content := `
# comment line
git = /usr/bin/git
convert text/html = pandoc -f html -t plain
ext .html = text/html
version pandoc = 3.1.11
version-cmd pandoc = pandoc --version
`
	cfg := newConfig(t, content)

	if cfg.Git != "/usr/bin/git" {
		t.Errorf("Git = %q, want /usr/bin/git", cfg.Git)
	}
	if cmds := cfg.Converters["text/html"]; len(cmds) == 0 || cmds[0] != "pandoc" {
		t.Errorf("Converters[text/html] = %v, want [pandoc ...]", cmds)
	}
	if m := cfg.ExtMIME[".html"]; m != "text/html" {
		t.Errorf("ExtMIME[.html] = %q, want text/html", m)
	}
	if v := cfg.Versions["pandoc"]; v != "3.1.11" {
		t.Errorf("Versions[pandoc] = %q, want 3.1.11", v)
	}
	if cmd := cfg.VersionCmd("pandoc"); len(cmd) == 0 || cmd[0] != "pandoc" {
		t.Errorf("VersionCmd(pandoc) = %v, want [pandoc --version]", cmd)
	}
}

func TestLoadConfig_TmconfigOverridesUserConfig(t *testing.T) {
	userContent := `convert text/html = converter-a`
	tmcfgContent := `convert text/html = converter-b`

	cfg := newConfigFromFiles(t, userContent, tmcfgContent)

	cmds := cfg.Converters["text/html"]
	if len(cmds) == 0 || cmds[0] != "converter-b" {
		t.Errorf("expected .tmconfig to override user config; got %v", cmds)
	}
}

func TestLoadConfig_UnknownKeysIgnored(t *testing.T) {
	content := `file = /some/path.mmd
doc = /some/doc.md
unknown-key = something
`
	cfg := newConfig(t, content)
	if cfg.Git != "" || len(cfg.Converters) != 0 {
		t.Errorf("unexpected config values parsed: git=%q converters=%v", cfg.Git, cfg.Converters)
	}
}

func TestLoadConfig_ExtNormalizesLeadingDot(t *testing.T) {
	content := `ext html = text/html`
	cfg := newConfig(t, content)
	if m := cfg.ExtMIME[".html"]; m != "text/html" {
		t.Errorf("ExtMIME[.html] = %q, want text/html (leading dot added)", m)
	}
}

func TestLoadConfig_VersionCmdDefault(t *testing.T) {
	content := `version pandoc = 3.1.11`
	cfg := newConfig(t, content)
	cmd := cfg.VersionCmd("pandoc")
	if len(cmd) != 2 || cmd[0] != "pandoc" || cmd[1] != "--version" {
		t.Errorf("VersionCmd default = %v, want [pandoc --version]", cmd)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// No-config guarantee
// ──────────────────────────────────────────────────────────────────────────────

func TestNoConfigGuarantee_NoExec(t *testing.T) {
	// With no config, a local file with a convertible extension (.html) is read
	// raw — no converter is invoked.
	dir := t.TempDir()
	htmlContent := "<p>hello world</p>\nline2\nline3\n"
	path := writeTempFile(t, dir, "page.html", htmlContent)

	// Build a converter script that would signal if invoked.
	signalFile := filepath.Join(dir, "invoked")
	converterPath := writeScript(t, dir, "spy",
		fmt.Sprintf("touch %s\ncat", signalFile))
	_ = converterPath // not configured

	// Empty config (no user config, no .tmconfig).
	emptyCfg := newConfig(t, "")
	r := resolverFrom(emptyCfg, dir)

	c := makeCitation(filepath.Base(path), 1, 2)
	_, _, err := r.Read(c)
	if err != nil {
		t.Fatalf("unexpected error reading HTML file with no config: %v", err)
	}
	// The signal file must NOT exist (converter was not called).
	if _, err := os.Stat(signalFile); !os.IsNotExist(err) {
		t.Error("converter was invoked despite no config — guarantee violated")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Converter tests
// ──────────────────────────────────────────────────────────────────────────────

func TestConverter_Basic(t *testing.T) {
	dir := t.TempDir()
	// Converter: cat stdin (outputs the input unchanged).
	converterPath := writeScript(t, dir, "myconv", "cat")
	verPath := writeScript(t, dir, "myconv-ver", `echo "1.0.0"`)

	htmlContent := "line one\nline two\nline three\n"
	writeTempFile(t, dir, "doc.html", htmlContent)

	cfgContent := fmt.Sprintf(`convert text/html = %s
ext .html = text/html
version %s = 1.0.0
version-cmd %s = %s`, converterPath, converterPath, converterPath, verPath)
	cfg := newConfig(t, cfgContent)
	r := resolverFrom(cfg, dir)

	c := makeCitation("doc.html", 1, 2)
	text, meta, err := r.Read(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "line one\nline two" {
		t.Errorf("text = %q, want %q", text, "line one\nline two")
	}
	if meta.Converter != converterPath {
		t.Errorf("meta.Converter = %q, want %q", meta.Converter, converterPath)
	}
	if meta.ConverterVersion != "1.0.0" {
		t.Errorf("meta.ConverterVersion = %q, want %q", meta.ConverterVersion, "1.0.0")
	}
	if meta.MIME != "text/html" {
		t.Errorf("meta.MIME = %q, want text/html", meta.MIME)
	}
}

func TestConverter_VersionMismatch(t *testing.T) {
	dir := t.TempDir()
	converterPath := writeScript(t, dir, "myconv", "cat")
	verPath := writeScript(t, dir, "myconv-ver", `echo "2.0.0"`)

	writeTempFile(t, dir, "doc.html", "content\n")

	cfgContent := fmt.Sprintf(`convert text/html = %s
ext .html = text/html
version %s = 1.0.0
version-cmd %s = %s`, converterPath, converterPath, converterPath, verPath)
	cfg := newConfig(t, cfgContent)
	r := resolverFrom(cfg, dir)

	c := makeCitation("doc.html", 1, 1)
	_, _, err := r.Read(c)
	if err == nil {
		t.Fatal("expected version mismatch error, got nil")
	}

	var ref *source.RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("expected RefusalError, got %T: %v", err, err)
	}
	if !strings.Contains(ref.Err, "2.0.0") {
		t.Errorf("err should mention actual version 2.0.0: %q", ref.Err)
	}
	if !strings.Contains(ref.Err, "1.0.0") {
		t.Errorf("err should mention pinned version 1.0.0: %q", ref.Err)
	}
	if !strings.Contains(ref.Fix, "set version") {
		t.Errorf("fix should say 'set version ...': %q", ref.Fix)
	}
}

func TestConverter_NoVersionPin(t *testing.T) {
	dir := t.TempDir()
	converterPath := writeScript(t, dir, "myconv", "cat")

	writeTempFile(t, dir, "doc.html", "content\n")

	// configure convert but no version pin
	cfgContent := fmt.Sprintf(`convert text/html = %s
ext .html = text/html`, converterPath)
	cfg := newConfig(t, cfgContent)
	r := resolverFrom(cfg, dir)

	c := makeCitation("doc.html", 1, 1)
	_, _, err := r.Read(c)
	if err == nil {
		t.Fatal("expected no-version-pin error, got nil")
	}
	var ref *source.RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("expected RefusalError, got %T: %v", err, err)
	}
	if !strings.Contains(ref.Err, "no version pin") {
		t.Errorf("unexpected err: %q", ref.Err)
	}
}

func TestConverter_NonZeroExit(t *testing.T) {
	dir := t.TempDir()
	converterPath := writeScript(t, dir, "myconv", `echo "conversion failed" >&2
exit 1`)
	verPath := writeScript(t, dir, "myconv-ver", `echo "1.0.0"`)

	writeTempFile(t, dir, "doc.html", "content\n")

	cfgContent := fmt.Sprintf(`convert text/html = %s
ext .html = text/html
version %s = 1.0.0
version-cmd %s = %s`, converterPath, converterPath, converterPath, verPath)
	cfg := newConfig(t, cfgContent)
	r := resolverFrom(cfg, dir)

	c := makeCitation("doc.html", 1, 1)
	_, _, err := r.Read(c)
	if err == nil {
		t.Fatal("expected non-zero exit error, got nil")
	}
	var ref *source.RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("expected RefusalError, got %T: %v", err, err)
	}
	if !strings.Contains(ref.Err, "conversion failed") {
		t.Errorf("err should contain converter stderr: %q", ref.Err)
	}
}

func TestConverter_VersionCachedPerProcess(t *testing.T) {
	dir := t.TempDir()
	// Counter file incremented on each version check call.
	counterFile := filepath.Join(dir, "count")
	converterPath := writeScript(t, dir, "myconv", "cat")
	verPath := writeScript(t, dir, "myconv-ver",
		fmt.Sprintf(`n=0; test -f %s && n=$(cat %s); echo $((n+1)) > %s; echo "1.0.0"`,
			counterFile, counterFile, counterFile))

	writeTempFile(t, dir, "a.html", "line1\nline2\n")
	writeTempFile(t, dir, "b.html", "line3\nline4\n")

	cfgContent := fmt.Sprintf(`convert text/html = %s
ext .html = text/html
version %s = 1.0.0
version-cmd %s = %s`, converterPath, converterPath, converterPath, verPath)
	cfg := newConfig(t, cfgContent)
	r := resolverFrom(cfg, dir)

	// Read two files with the same converter — version check should run only once.
	for _, name := range []string{"a.html", "b.html"} {
		c := makeCitation(name, 1, 1)
		if _, _, err := r.Read(c); err != nil {
			t.Fatalf("unexpected error for %s: %v", name, err)
		}
	}

	data, _ := os.ReadFile(counterFile)
	count := strings.TrimSpace(string(data))
	if count != "1" {
		t.Errorf("version check ran %s times, want 1 (should be cached)", count)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// URI fetch tests
// ──────────────────────────────────────────────────────────────────────────────

func TestURIFetch_TextPlainRaw(t *testing.T) {
	content := "line one\nline two\nline three\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, content)
	}))
	defer srv.Close()

	cfg := newConfig(t, "")
	r := resolverFrom(cfg, t.TempDir())

	c := makeCitation(srv.URL+"/doc.txt", 1, 2)
	text, meta, err := r.Read(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "line one\nline two" {
		t.Errorf("text = %q, want %q", text, "line one\nline two")
	}
	if meta.URL == "" {
		t.Error("meta.URL should be set")
	}
	if meta.FetchedAt.IsZero() {
		t.Error("meta.FetchedAt should be set")
	}
	if meta.MIME != "text/plain" {
		t.Errorf("meta.MIME = %q, want text/plain", meta.MIME)
	}
}

func TestURIFetch_TextMarkdownRaw(t *testing.T) {
	content := "# Heading\nParagraph text.\nMore text.\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = fmt.Fprint(w, content)
	}))
	defer srv.Close()

	cfg := newConfig(t, "")
	r := resolverFrom(cfg, t.TempDir())

	c := makeCitation(srv.URL+"/readme.md", 1, 2)
	text, _, err := r.Read(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "# Heading\nParagraph text." {
		t.Errorf("text = %q", text)
	}
}

func TestURIFetch_Redirect(t *testing.T) {
	content := "redirected content\nline two\n"
	finalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, content)
	}))
	defer finalSrv.Close()

	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalSrv.URL+"/final.txt", http.StatusFound)
	}))
	defer redirectSrv.Close()

	cfg := newConfig(t, "")
	r := resolverFrom(cfg, t.TempDir())

	c := makeCitation(redirectSrv.URL+"/original", 1, 1)
	text, meta, err := r.Read(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "redirected content" {
		t.Errorf("text = %q, want %q", text, "redirected content")
	}
	// Final URL should be the destination after redirect.
	if !strings.Contains(meta.URL, finalSrv.URL) {
		t.Errorf("meta.URL = %q should contain final server URL %q", meta.URL, finalSrv.URL)
	}
}

func TestURIFetch_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, "not found")
	}))
	defer srv.Close()

	cfg := newConfig(t, "")
	r := resolverFrom(cfg, t.TempDir())

	c := makeCitation(srv.URL+"/missing.txt", 1, 1)
	_, _, err := r.Read(c)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	var ref *source.RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("expected RefusalError, got %T: %v", err, err)
	}
	if !strings.Contains(ref.Err, "failed") {
		t.Errorf("err = %q should contain 'failed'", ref.Err)
	}
	if !strings.Contains(ref.Fix, "TM_SRC_ROOT") {
		t.Errorf("fix = %q should mention TM_SRC_ROOT", ref.Fix)
	}
}

func TestURIFetch_OversizeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// Write more than 16 MiB.
		chunk := strings.Repeat("x", 1024)
		for i := 0; i < 17*1024+1; i++ {
			_, _ = fmt.Fprint(w, chunk)
		}
	}))
	defer srv.Close()

	cfg := newConfig(t, "")
	r := resolverFrom(cfg, t.TempDir())

	c := makeCitation(srv.URL+"/big.txt", 1, 1)
	_, _, err := r.Read(c)
	if err == nil {
		t.Fatal("expected error for oversize body, got nil")
	}
	var ref *source.RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("expected RefusalError, got %T: %v", err, err)
	}
	if !strings.Contains(ref.Err, "16 MiB") {
		t.Errorf("err = %q should mention 16 MiB", ref.Err)
	}
}

func TestURIFetch_NoConverter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = fmt.Fprint(w, "%PDF-1.4 content")
	}))
	defer srv.Close()

	cfg := newConfig(t, "") // no converter for application/pdf
	r := resolverFrom(cfg, t.TempDir())

	c := makeCitation(srv.URL+"/doc.pdf", 1, 1)
	_, _, err := r.Read(c)
	if err == nil {
		t.Fatal("expected error for no converter, got nil")
	}
	var ref *source.RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("expected RefusalError, got %T: %v", err, err)
	}
	if !strings.Contains(ref.Err, "no converter for application/pdf") {
		t.Errorf("err = %q should mention 'no converter for application/pdf'", ref.Err)
	}
	if !strings.Contains(ref.Fix, "convert application/pdf") {
		t.Errorf("fix = %q should say 'convert application/pdf'", ref.Fix)
	}
}

func TestURIFetch_WithConverter(t *testing.T) {
	dir := t.TempDir()
	converterPath := writeScript(t, dir, "myconv", `sed 's/<[^>]*>//g'`)
	verPath := writeScript(t, dir, "myconv-ver", `echo "1.0.0"`)

	htmlContent := "<p>line one</p>\n<p>line two</p>\n<p>line three</p>\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, htmlContent)
	}))
	defer srv.Close()

	cfgContent := fmt.Sprintf(`convert text/html = %s
version %s = 1.0.0
version-cmd %s = %s`, converterPath, converterPath, converterPath, verPath)
	cfg := newConfig(t, cfgContent)
	r := resolverFrom(cfg, dir)

	c := makeCitation(srv.URL+"/doc.html", 1, 2)
	text, meta, err := r.Read(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Converted text should have HTML tags removed.
	if strings.Contains(text, "<p>") {
		t.Errorf("HTML tags should be stripped by converter; got %q", text)
	}
	if meta.Converter != converterPath {
		t.Errorf("meta.Converter = %q, want %q", meta.Converter, converterPath)
	}
	if !meta.FetchedAt.After(time.Time{}) {
		t.Error("meta.FetchedAt should be set for URI fetch")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Path resolution tests
// ──────────────────────────────────────────────────────────────────────────────

func TestPath_RawFile(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "notes.txt", "alpha\nbeta\ngamma\n")

	cfg := newConfig(t, "")
	r := resolverFrom(cfg, dir)

	c := makeCitation("notes.txt", 2, 3)
	text, _, err := r.Read(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "beta\ngamma" {
		t.Errorf("text = %q, want %q", text, "beta\ngamma")
	}
}

func TestPath_MissingFile_NoGitConfigured(t *testing.T) {
	dir := t.TempDir()
	cfg := newConfig(t, "")
	r := resolverFrom(cfg, dir)

	c := makeCitation("missing.txt", 1, 1)
	_, _, err := r.Read(c)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	var ref *source.RefusalError
	if !errors.As(err, &ref) {
		// Could be a plain error from os.ReadFile wrapped by the resolver.
		// When git is not configured, it may be a RefusalError or a plain error.
		// Either is acceptable as long as it's non-nil.
		return
	}
	if !strings.Contains(ref.Err, "not in the working tree") {
		t.Errorf("err = %q should say 'not in the working tree'", ref.Err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Git HEAD blob tests
// ──────────────────────────────────────────────────────────────────────────────

func skipIfNoGit(t *testing.T) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH; skipping git test")
	}
	return gitPath
}

// initGitRepo creates a real git repo in dir, commits "file.txt" with content,
// then returns the commit SHA.
func initGitRepo(t *testing.T, dir, content string) string {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Write file.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for _, args := range [][]string{
		{"add", "file.txt"},
		{"commit", "-m", "initial"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Get the commit SHA.
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestGitHEADBlob_MissingFileRetrievedFromHEAD(t *testing.T) {
	gitPath := skipIfNoGit(t)

	dir := t.TempDir()
	content := "first line\nsecond line\nthird line\n"
	commitSHA := initGitRepo(t, dir, content)
	_ = commitSHA

	// Delete the file from the working tree.
	if err := os.Remove(filepath.Join(dir, "file.txt")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	cfgContent := fmt.Sprintf("git = %s", gitPath)
	cfg := newConfig(t, cfgContent)
	r := resolverFrom(cfg, dir)

	c := makeCitation("file.txt", 1, 2)
	text, meta, err := r.Read(c)
	if err != nil {
		t.Fatalf("expected HEAD blob retrieval to succeed; got: %v", err)
	}
	if text != "first line\nsecond line" {
		t.Errorf("text = %q, want %q", text, "first line\nsecond line")
	}
	_ = meta // meta.Commit is set by commit recording
}

func TestGitHEADBlob_MissingFileNoGit(t *testing.T) {
	skipIfNoGit(t) // Still need git to set up the repo.

	dir := t.TempDir()
	initGitRepo(t, dir, "content\n")
	if err := os.Remove(filepath.Join(dir, "file.txt")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// No git configured in source config.
	cfg := newConfig(t, "")
	r := resolverFrom(cfg, dir)

	c := makeCitation("file.txt", 1, 1)
	_, _, err := r.Read(c)
	if err == nil {
		t.Fatal("expected error for missing file with no git configured")
	}
	var ref *source.RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("expected RefusalError, got %T: %v", err, err)
	}
	if !strings.Contains(ref.Err, "not in the working tree") {
		t.Errorf("err = %q should say 'not in the working tree'", ref.Err)
	}
	if !strings.Contains(ref.Fix, "set git") {
		t.Errorf("fix = %q should say 'set git'", ref.Fix)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Commit recording tests
// ──────────────────────────────────────────────────────────────────────────────

func TestCommitRecording_BranchRef(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir()
	commitSHA := initGitRepo(t, dir, "alpha\nbeta\n")

	commit := source.CommitForPath(filepath.Join(dir, "file.txt"))
	if commit != commitSHA {
		t.Errorf("CommitForPath = %q, want %q", commit, commitSHA)
	}
}

func TestCommitRecording_PackedRefs(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir()
	commitSHA := initGitRepo(t, dir, "alpha\nbeta\n")

	// Pack the refs.
	cmd := exec.Command("git", "-C", dir, "pack-refs", "--all")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git pack-refs: %v\n%s", err, out)
	}

	commit := source.CommitForPath(filepath.Join(dir, "file.txt"))
	if commit != commitSHA {
		t.Errorf("CommitForPath with packed refs = %q, want %q", commit, commitSHA)
	}
}

func TestCommitRecording_WorktreeGitdirFile(t *testing.T) {
	skipIfNoGit(t)

	mainDir := t.TempDir()
	commitSHA := initGitRepo(t, mainDir, "content\n")

	// Create a worktree.
	wtDir := t.TempDir()
	cmd := exec.Command("git", "-C", mainDir, "worktree", "add", wtDir, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git worktree add failed (may not be supported): %v\n%s", err, out)
	}

	commit := source.CommitForPath(filepath.Join(wtDir, "file.txt"))
	if commit != commitSHA {
		t.Errorf("CommitForPath in worktree = %q, want %q", commit, commitSHA)
	}
}

func TestCommitRecording_NotInRepo(t *testing.T) {
	dir := t.TempDir()
	commit := source.CommitForPath(filepath.Join(dir, "file.txt"))
	if commit != "" {
		t.Errorf("CommitForPath outside repo = %q, want empty", commit)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// HashCitation and CheckDrift tests
// ──────────────────────────────────────────────────────────────────────────────

func TestHashCitation_LocalFile(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "src.txt", "line one\nline two\nline three\n")

	cfg := newConfig(t, "")
	r := resolverFrom(cfg, dir)

	hashed, _, err := r.HashCitation("src.txt:1-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Must have the hash prefix.
	if !strings.Contains(hashed, "@src.txt:1-2") {
		t.Errorf("hashed = %q, should contain @src.txt:1-2", hashed)
	}
	// Hash prefix should be 12 hex chars.
	atIdx := strings.Index(hashed, "@")
	if atIdx != 12 {
		t.Errorf("hash prefix in %q should be 12 chars before @", hashed)
	}
}

func TestHashCitation_Mismatch(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "src.txt", "changed content\n")

	cfg := newConfig(t, "")
	r := resolverFrom(cfg, dir)

	// First compute the correct hash.
	hashed, _, err := r.HashCitation("src.txt:1-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Modify file.
	writeTempFile(t, dir, "src.txt", "different content\n")

	// Now try with the old (stale) hash — should fail with mismatch.
	_, _, err = r.HashCitation(hashed)
	if err == nil {
		t.Fatal("expected hash mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("err = %q should say 'mismatch'", err.Error())
	}
}

func TestCheckDrift_NoDrift(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "src.txt", "stable content\n")

	cfg := newConfig(t, "")
	r := resolverFrom(cfg, dir)

	hashed, _, err := r.HashCitation("src.txt:1-1")
	if err != nil {
		t.Fatalf("unexpected error hashing: %v", err)
	}

	drifted, _, err := r.CheckDrift(hashed)
	if err != nil {
		t.Fatalf("unexpected error checking drift: %v", err)
	}
	if drifted {
		t.Error("expected no drift for freshly hashed citation")
	}
}

func TestCheckDrift_WithDrift(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "src.txt", "original content\n")

	cfg := newConfig(t, "")
	r := resolverFrom(cfg, dir)

	hashed, _, err := r.HashCitation("src.txt:1-1")
	if err != nil {
		t.Fatalf("unexpected error hashing: %v", err)
	}

	// Modify file.
	writeTempFile(t, dir, "src.txt", "changed content\n")

	drifted, _, err := r.CheckDrift(hashed)
	if err != nil {
		t.Fatalf("unexpected error checking drift: %v", err)
	}
	if !drifted {
		t.Error("expected drift after file modification")
	}
}

func TestCheckDrift_HashlessCitationNeverDrifted(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "src.txt", "content\n")

	cfg := newConfig(t, "")
	r := resolverFrom(cfg, dir)

	// Hashless citation (legacy form).
	drifted, _, err := r.CheckDrift("src.txt:1-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if drifted {
		t.Error("hashless citations should never be reported as drifted")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// ApplyMeta tests
// ──────────────────────────────────────────────────────────────────────────────

func TestApplyMeta_OnlyNonEmptyFields(t *testing.T) {
	fields := map[string]any{}
	meta := source.Meta{
		Commit: "abc123",
		URL:    "https://example.com",
	}
	source.ApplyMeta(fields, meta)

	if fields["commit"] != "abc123" {
		t.Errorf("commit = %v", fields["commit"])
	}
	if fields["url"] != "https://example.com" {
		t.Errorf("url = %v", fields["url"])
	}
	// Empty fields should not be set.
	if _, ok := fields["mime"]; ok {
		t.Error("mime should not be set when empty")
	}
	if _, ok := fields["converter"]; ok {
		t.Error("converter should not be set when empty")
	}
	if _, ok := fields["fetched_at"]; ok {
		t.Error("fetched_at should not be set when zero")
	}
}

func TestApplyMeta_FetchedAtRFC3339(t *testing.T) {
	fields := map[string]any{}
	fetchedAt := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	source.ApplyMeta(fields, source.Meta{FetchedAt: fetchedAt})
	if s, ok := fields["fetched_at"].(string); !ok || !strings.HasPrefix(s, "2026-09-23") {
		t.Errorf("fetched_at = %v, want RFC3339 with 2026-09-23", fields["fetched_at"])
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// FindGitRepo tests
// ──────────────────────────────────────────────────────────────────────────────

func TestFindGitRepo_NotInRepo(t *testing.T) {
	dir := t.TempDir()
	repoDir, gitDir := source.FindGitRepo(filepath.Join(dir, "file.txt"))
	if repoDir != "" || gitDir != "" {
		t.Errorf("expected empty, got repoDir=%q gitDir=%q", repoDir, gitDir)
	}
}

func TestFindGitRepo_InRepo(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir()
	initGitRepo(t, dir, "content\n")

	repoDir, gitDir := source.FindGitRepo(filepath.Join(dir, "file.txt"))
	if repoDir == "" {
		t.Error("expected non-empty repoDir")
	}
	if gitDir == "" {
		t.Error("expected non-empty gitDir")
	}
}
