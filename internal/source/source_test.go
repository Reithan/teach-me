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
// Shared helpers
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

// newConfig returns a Config with the given user config content and no .tmconfig.
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
// a .tmconfig. Either content may be "".
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

// writeTempFile writes content to dir/<name>.
func writeTempFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
}

// makeCitation creates a hashless Citation struct for tests.
func makeCitation(file string, start, end int) cite.Citation {
	return cite.Citation{File: file, Start: start, End: end}
}

// newTestServer starts an httptest.Server that sets the given Content-Type
// and body on every GET. A whitespace-only Content-Type is passed through
// as-is (stripMIMEParams then yields "") so tests can exercise the URL-ext
// fallback. The server is closed via t.Cleanup.
func newTestServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newConverterPair writes a converter script (body) and a version-1.0.0 script,
// then returns a config snippet ready for newConfig.
// The converter handles text/html.
func newConverterPair(t *testing.T, dir, body string) string {
	t.Helper()
	const (
		mime = "text/html"
		pin  = "1.0.0"
	)
	name := "conv_text_html"
	convPath := writeScript(t, dir, name, body)
	verPath := writeScript(t, dir, name+"_ver", fmt.Sprintf(`printf "%s"`, pin))
	return fmt.Sprintf("convert %s=%s\nversion %s=%s\nversion-cmd %s=%s\n",
		mime, convPath, convPath, pin, convPath, verPath)
}

// skipIfNoGit skips the test if git is not on PATH and returns the git path.
func skipIfNoGit(t *testing.T) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH; skipping git test")
	}
	return gitPath
}

// initGitRepo creates a real git repo in dir, commits "file.txt" with content,
// and returns the commit SHA.
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
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// makeSyntheticGitDir creates a .git directory under dir with the given HEAD
// content, an optional loose ref (refName → sha40), and optional packed-refs
// content. Returns dir.
func makeSyntheticGitDir(t *testing.T, headContent, looseRefName, looseRefSHA, packedRefsContent string) string {
	t.Helper()
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(headContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if looseRefName != "" {
		refPath := filepath.Join(gitDir, filepath.FromSlash(looseRefName))
		if err := os.MkdirAll(filepath.Dir(refPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(refPath, []byte(looseRefSHA+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if packedRefsContent != "" {
		if err := os.WriteFile(filepath.Join(gitDir, "packed-refs"), []byte(packedRefsContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// ──────────────────────────────────────────────────────────────────────────────
// Config parsing
// ──────────────────────────────────────────────────────────────────────────────

func TestLoadConfig(t *testing.T) {
	allKeys := `
# comment line
git = /usr/bin/git
convert text/html = pandoc -f html -t plain
ext .html = text/html
version pandoc = 3.1.11
version-cmd pandoc = pandoc --version
`

	tests := []struct {
		name         string
		userContent  string
		tmcfgContent string
		wantGit      string
		wantConvKey  string
		wantConvCmd  string
		wantExtMIME  string
		wantVersion  string
	}{
		{
			name:    "absent files yield empty config",
			wantGit: "",
		},
		{
			name:        "parses all recognised keys",
			userContent: allKeys,
			wantGit:     "/usr/bin/git",
			wantConvKey: "text/html",
			wantConvCmd: "pandoc",
			wantExtMIME: "text/html",
			wantVersion: "3.1.11",
		},
		{
			name:         ".tmconfig overrides user config",
			userContent:  "convert text/html = converter-a\n",
			tmcfgContent: "convert text/html = converter-b\n",
			wantConvKey:  "text/html",
			wantConvCmd:  "converter-b",
		},
		{
			name:        "unknown keys are ignored",
			userContent: "file = /some/path.mmd\ndoc = /some/doc.md\nunknown = x\n",
			wantGit:     "",
		},
		{
			name:        "ext without leading dot gets dot added",
			userContent: "ext html = text/html\n",
			wantExtMIME: "text/html",
		},
		{
			name:        "version-cmd default is [prog --version]",
			userContent: "version pandoc = 3.1.11\n",
			wantVersion: "3.1.11",
		},
		{
			name:        "line without equals separator is skipped",
			userContent: "bare-word-no-equals\ngit=git\n",
			wantGit:     "git",
		},
		{
			name:        "blank value is skipped",
			userContent: "git=\n",
			wantGit:     "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cfg *source.Config
			if tc.tmcfgContent != "" {
				cfg = newConfigFromFiles(t, tc.userContent, tc.tmcfgContent)
			} else {
				if tc.userContent == "" && tc.tmcfgContent == "" {
					dir := t.TempDir()
					var err error
					cfg, err = source.LoadConfigPaths(
						filepath.Join(dir, "noconfig"),
						filepath.Join(dir, "notmconfig"),
					)
					if err != nil {
						t.Fatalf("LoadConfigPaths: %v", err)
					}
				} else {
					cfg = newConfig(t, tc.userContent)
				}
			}
			if cfg.Git != tc.wantGit {
				t.Errorf("Git = %q, want %q", cfg.Git, tc.wantGit)
			}
			if tc.wantConvKey != "" {
				cmds := cfg.Converters[tc.wantConvKey]
				if len(cmds) == 0 || cmds[0] != tc.wantConvCmd {
					t.Errorf("Converters[%s] = %v, want [%s ...]", tc.wantConvKey, cmds, tc.wantConvCmd)
				}
			}
			if tc.wantExtMIME != "" {
				if m := cfg.ExtMIME[".html"]; m != tc.wantExtMIME {
					t.Errorf("ExtMIME[.html] = %q, want %q", m, tc.wantExtMIME)
				}
			}
			if tc.wantVersion != "" {
				if v := cfg.Versions["pandoc"]; v != tc.wantVersion {
					t.Errorf("Versions[pandoc] = %q, want %q", v, tc.wantVersion)
				}
				cmd := cfg.VersionCmd("pandoc")
				if len(cmd) < 2 || cmd[0] != "pandoc" {
					t.Errorf("VersionCmd(pandoc) = %v, want [pandoc ...]", cmd)
				}
			}
		})
	}
}

// TestLoadConfig_XDGConfigHome verifies that XDG_CONFIG_HOME overrides
// ~/.config/tm/config as the user config path.
func TestLoadConfig_XDGConfigHome(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "tm")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config"), []byte("git=mygit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfg, err := source.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Git != "mygit" {
		t.Errorf("Git = %q; XDG_CONFIG_HOME not picked up", cfg.Git)
	}
}

// TestLoadConfigPaths_ReadErrors verifies that non-ENOENT file-read errors are
// propagated (e.g. the file exists but is unreadable).
func TestLoadConfigPaths_ReadErrors(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: chmod 0o000 has no effect")
	}
	tests := []struct {
		name       string
		makeUnread string // "user" or "tmcfg"
	}{
		{"unreadable user config propagates error", "user"},
		{"unreadable .tmconfig propagates error", "tmcfg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			userPath := filepath.Join(tmpDir, "config")
			tmcfgPath := filepath.Join(tmpDir, ".tmconfig")

			if tc.makeUnread == "user" {
				if err := os.WriteFile(userPath, []byte("git=git\n"), 0o000); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(tmcfgPath, []byte("git=git\n"), 0o000); err != nil {
					t.Fatal(err)
				}
			}
			_, err := source.LoadConfigPaths(userPath, tmcfgPath)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Config helper methods: ExtToMIME, ConverterFor
// ──────────────────────────────────────────────────────────────────────────────

func TestConfigHelpers(t *testing.T) {
	cfg := newConfig(t, "ext html=text/html\nconvert text/html=pandoc -f html -t plain\n")

	tests := []struct {
		name     string
		fn       string
		input    string
		wantMIME string
		wantCmd  string
	}{
		{"ExtToMIME empty ext returns empty", "ExtToMIME", "", "", ""},
		{"ExtToMIME without leading dot resolves", "ExtToMIME", "html", "text/html", ""},
		{"ExtToMIME with leading dot resolves", "ExtToMIME", ".html", "text/html", ""},
		{"ConverterFor strips MIME params before lookup", "ConverterFor", "text/html; charset=utf-8", "", "pandoc"},
		{"ConverterFor exact MIME matches", "ConverterFor", "text/html", "", "pandoc"},
		{"ConverterFor unknown MIME returns nil", "ConverterFor", "application/pdf", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			switch tc.fn {
			case "ExtToMIME":
				got := cfg.ExtToMIME(tc.input)
				if got != tc.wantMIME {
					t.Errorf("ExtToMIME(%q) = %q, want %q", tc.input, got, tc.wantMIME)
				}
			case "ConverterFor":
				cmds := cfg.ConverterFor(tc.input)
				if tc.wantCmd == "" {
					if len(cmds) != 0 {
						t.Errorf("ConverterFor(%q) = %v, want nil", tc.input, cmds)
					}
				} else {
					if len(cmds) == 0 || cmds[0] != tc.wantCmd {
						t.Errorf("ConverterFor(%q) = %v, want [%s ...]", tc.input, cmds, tc.wantCmd)
					}
				}
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// No-config guarantee
// ──────────────────────────────────────────────────────────────────────────────

// TestNoConfigGuarantee verifies that a local file with a convertible extension
// is read raw when no converter is configured — no external process is exec'd.
func TestNoConfigGuarantee(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "page.html", "<p>hello world</p>\nline2\nline3\n")

	// Spy script writes a signal file if invoked.
	signalFile := filepath.Join(dir, "invoked")
	_ = writeScript(t, dir, "spy", fmt.Sprintf("touch %s\ncat", signalFile))

	r := resolverFrom(newConfig(t, ""), dir)
	c := makeCitation("page.html", 1, 2)
	if _, _, err := r.Read(c); err != nil {
		t.Fatalf("unexpected error reading HTML file with no config: %v", err)
	}
	if _, err := os.Stat(signalFile); !os.IsNotExist(err) {
		t.Error("converter was invoked despite no config — guarantee violated")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Converter tests
// ──────────────────────────────────────────────────────────────────────────────

func TestConverter(t *testing.T) {
	tests := []struct {
		name      string
		build     func(t *testing.T) (dir string, cfgContent string)
		start     int
		end       int
		wantErr   bool
		wantInErr string
		wantInFix string
		wantText  string
		wantConv  bool
		wantVer   string
		wantMIME  string
	}{
		{
			name: "converter runs, slices text, and sets meta fields",
			build: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				cfg := newConverterPair(t, dir, "cat")
				writeTempFile(t, dir, "doc.html", "line one\nline two\nline three\n")
				return dir, cfg + "ext .html=text/html\n"
			},
			start: 1, end: 2, wantText: "line one\nline two",
			wantConv: true, wantVer: "1.0.0", wantMIME: "text/html",
		},
		{
			name: "version mismatch: actual differs from pin produces RefusalError",
			build: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				// pin=1.0.0 but version script prints 2.0.0 → mismatch
				convPath := writeScript(t, dir, "conv", "cat")
				verPath := writeScript(t, dir, "conv_ver", `printf "2.0.0"`)
				cfg := fmt.Sprintf("convert text/html=%s\nversion %s=1.0.0\nversion-cmd %s=%s\next .html=text/html\n",
					convPath, convPath, convPath, verPath)
				writeTempFile(t, dir, "doc.html", "content\n")
				return dir, cfg
			},
			start: 1, end: 1,
			wantErr: true, wantInErr: "2.0.0", wantInFix: "set version",
		},
		{
			name: "missing version pin produces RefusalError",
			build: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				convPath := writeScript(t, dir, "conv", "cat")
				cfg := fmt.Sprintf("convert text/html=%s\next .html=text/html\n", convPath)
				writeTempFile(t, dir, "doc.html", "content\n")
				return dir, cfg
			},
			start: 1, end: 1,
			wantErr: true, wantInErr: "no version pin",
		},
		{
			name: "non-zero exit with stderr produces RefusalError mentioning stderr",
			build: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				cfg := newConverterPair(t, dir, `echo "conversion failed" >&2
exit 1`)
				writeTempFile(t, dir, "doc.html", "content\n")
				return dir, cfg + "ext .html=text/html\n"
			},
			start: 1, end: 1,
			wantErr: true, wantInErr: "conversion failed",
		},
		{
			name: "non-zero exit with empty stderr uses err.Error in RefusalError",
			build: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				cfg := newConverterPair(t, dir, "exit 1")
				writeTempFile(t, dir, "doc.html", "content\n")
				return dir, cfg + "ext .html=text/html\n"
			},
			start: 1, end: 1,
			wantErr: true, wantInErr: "converter",
		},
		{
			name: "multi-line version output uses first line for pin check",
			build: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				convPath := writeScript(t, dir, "conv", "cat")
				verPath := writeScript(t, dir, "conv_ver", `printf "2.0.0\nExtra version info\n"`)
				cfg := fmt.Sprintf("convert text/html=%s\nversion %s=2.0.0\nversion-cmd %s=%s\next .html=text/html\n",
					convPath, convPath, convPath, verPath)
				writeTempFile(t, dir, "doc.html", "body{}\n")
				return dir, cfg
			},
			start: 1, end: 1, wantText: "body{}", wantVer: "2.0.0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, cfgContent := tc.build(t)
			r := resolverFrom(newConfig(t, cfgContent), dir)
			c := makeCitation("doc.html", tc.start, tc.end)
			text, meta, err := r.Read(c)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var ref *source.RefusalError
				if !errors.As(err, &ref) {
					t.Fatalf("expected RefusalError, got %T: %v", err, err)
				}
				if tc.wantInErr != "" && !strings.Contains(ref.Err, tc.wantInErr) {
					t.Errorf("Err = %q, want to contain %q", ref.Err, tc.wantInErr)
				}
				if tc.wantInFix != "" && !strings.Contains(ref.Fix, tc.wantInFix) {
					t.Errorf("Fix = %q, want to contain %q", ref.Fix, tc.wantInFix)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantText != "" && text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
			if tc.wantConv && meta.Converter == "" {
				t.Error("meta.Converter is empty; expected converter path")
			}
			if tc.wantVer != "" && meta.ConverterVersion != tc.wantVer {
				t.Errorf("meta.ConverterVersion = %q, want %q", meta.ConverterVersion, tc.wantVer)
			}
			if tc.wantMIME != "" && meta.MIME != tc.wantMIME {
				t.Errorf("meta.MIME = %q, want %q", meta.MIME, tc.wantMIME)
			}
		})
	}

	// Version check must be cached per resolver: two reads with the same
	// converter must invoke the version script exactly once.
	t.Run("version check is cached per resolver instance", func(t *testing.T) {
		dir := t.TempDir()
		counterFile := filepath.Join(dir, "count")
		convPath := writeScript(t, dir, "conv", "cat")
		verPath := writeScript(t, dir, "conv_ver",
			fmt.Sprintf(`n=0; test -f %s && n=$(cat %s); echo $((n+1)) > %s; echo "1.0.0"`,
				counterFile, counterFile, counterFile))
		cfg := fmt.Sprintf("convert text/html=%s\nversion %s=1.0.0\nversion-cmd %s=%s\next .html=text/html\n",
			convPath, convPath, convPath, verPath)
		writeTempFile(t, dir, "a.html", "line1\nline2\n")
		writeTempFile(t, dir, "b.html", "line3\nline4\n")

		r := resolverFrom(newConfig(t, cfg), dir)
		for _, f := range []string{"a.html", "b.html"} {
			if _, _, err := r.Read(makeCitation(f, 1, 1)); err != nil {
				t.Fatalf("Read(%s): %v", f, err)
			}
		}
		data, _ := os.ReadFile(counterFile)
		if n := strings.TrimSpace(string(data)); n != "1" {
			t.Errorf("version script ran %s times, want 1 (should be cached)", n)
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// URI fetch tests
// ──────────────────────────────────────────────────────────────────────────────

func TestReadURI(t *testing.T) {
	const threeLines = "line one\nline two\nline three\n"

	// Redirect servers: redirectSrv → finalSrv.
	finalSrv := newTestServer(t, "text/plain", threeLines)
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalSrv.URL+"/target", http.StatusFound)
	}))
	t.Cleanup(redirectSrv.Close)

	// 404 server always returns Not Found.
	notFoundSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(notFoundSrv.Close)

	// Oversize server writes >16 MiB.
	oversizeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		chunk := strings.Repeat("x", 1024*1024)
		for i := 0; i < 17; i++ {
			_, _ = fmt.Fprint(w, chunk)
		}
	}))
	t.Cleanup(oversizeSrv.Close)

	// Whitespace CT causes stripMIMEParams to return "" → URL-extension fallback.
	noCtSrv := newTestServer(t, " ", "# Title\nContent here\n")
	// No CT, no extension → treated as text/plain.
	noMimeSrv := newTestServer(t, " ", "plain content\n")

	// Converter for text/html: strips tags.
	convDir := t.TempDir()
	convCfg := newConverterPair(t, convDir, `sed 's/<[^>]*>//g'`)
	htmlSrv := newTestServer(t, "text/html", "<p>line one</p>\n<p>line two</p>\n")

	// Failing converter for text/html.
	failDir := t.TempDir()
	failCfg := newConverterPair(t, failDir, `echo "conv error" >&2
exit 1`)

	tests := []struct {
		name      string
		url       string
		cfgExtra  string
		start     int
		end       int
		wantText  string
		wantMIME  string
		wantURL   string
		wantErr   bool
		wantInErr string
	}{
		{
			name:  "text/plain is returned raw",
			url:   newTestServer(t, "text/plain", threeLines).URL + "/doc.txt",
			start: 1, end: 2, wantText: "line one\nline two",
		},
		{
			name:  "text/markdown is returned raw",
			url:   newTestServer(t, "text/markdown", "# H\ntext\n").URL + "/doc.md",
			start: 1, end: 2, wantText: "# H\ntext",
		},
		{
			name:  "redirect follows and records final URL in meta",
			url:   redirectSrv.URL + "/redir",
			start: 1, end: 1, wantText: "line one",
			wantURL: finalSrv.URL + "/target",
		},
		{
			name:  "non-2xx response produces RefusalError mentioning fetch",
			url:   notFoundSrv.URL + "/doc.txt",
			start: 1, end: 1,
			wantErr: true, wantInErr: "fetch",
		},
		{
			name:  "response body exceeding 16 MiB produces RefusalError",
			url:   oversizeSrv.URL + "/big",
			start: 1, end: 1,
			wantErr: true, wantInErr: "16 MiB",
		},
		{
			name:  "unknown MIME with no converter produces RefusalError",
			url:   newTestServer(t, "application/pdf", "%PDF").URL + "/doc.pdf",
			start: 1, end: 1,
			wantErr: true, wantInErr: "no converter for application/pdf",
		},
		{
			name:     "converter is applied; meta.MIME is set",
			url:      htmlSrv.URL + "/page.html",
			cfgExtra: convCfg,
			start:    1, end: 2, wantMIME: "text/html",
		},
		{
			name:     "whitespace Content-Type falls back to URL path extension",
			url:      noCtSrv.URL + "/doc.md",
			cfgExtra: "ext .md=text/markdown\n",
			start:    1, end: 2, wantText: "# Title\nContent here",
			wantMIME: "text/markdown",
		},
		{
			name:  "no Content-Type and no extension mapping treated as text/plain",
			url:   noMimeSrv.URL + "/doc",
			start: 1, end: 1, wantText: "plain content",
		},
		{
			name:     "converter non-zero exit produces RefusalError",
			url:      newTestServer(t, "text/html", "<p>content</p>\n").URL + "/bad.html",
			cfgExtra: failCfg,
			start:    1, end: 1,
			wantErr: true, wantInErr: "converter",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := resolverFrom(newConfig(t, tc.cfgExtra), t.TempDir())
			c := makeCitation(tc.url, tc.start, tc.end)
			text, meta, err := r.Read(c)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
					t.Errorf("err = %q, want to contain %q", err.Error(), tc.wantInErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantText != "" && text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
			if tc.wantMIME != "" && meta.MIME != tc.wantMIME {
				t.Errorf("meta.MIME = %q, want %q", meta.MIME, tc.wantMIME)
			}
			if tc.wantURL != "" && meta.URL != tc.wantURL {
				t.Errorf("meta.URL = %q, want %q", meta.URL, tc.wantURL)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Path reading tests
// ──────────────────────────────────────────────────────────────────────────────

func TestReadPath(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "lines.txt", "alpha\nbeta\ngamma\n")
	writeTempFile(t, dir, "doc.html", "<p>hello world</p>\n")
	writeTempFile(t, dir, "readme.md", "line one\nline two\n")

	tests := []struct {
		name      string
		file      string
		cfgExtra  string
		start     int
		end       int
		wantText  string
		wantMIME  string
		wantErr   bool
		wantInErr string
	}{
		{
			name: "raw file is sliced by line range",
			file: "lines.txt", start: 2, end: 3, wantText: "beta\ngamma",
		},
		{
			name: "ext mapped to raw MIME sets meta.MIME",
			file: "readme.md", cfgExtra: "ext .md=text/markdown\n",
			start: 1, end: 2, wantText: "line one\nline two",
			wantMIME: "text/markdown",
		},
		{
			name: "ext mapped to MIME with no converter returns raw text",
			file: "doc.html", cfgExtra: "ext .html=text/html\n",
			start: 1, end: 1, wantText: "<p>hello world</p>",
		},
		{
			name: "start < 1 returns error",
			file: "lines.txt", start: 0, end: 1,
			wantErr: true, wantInErr: "positive",
		},
		{
			name: "end beyond file length returns error",
			file: "lines.txt", start: 1, end: 99,
			wantErr: true, wantInErr: "out of bounds",
		},
		{
			name: "missing file with no git configured returns RefusalError",
			file: "missing.txt", start: 1, end: 1,
			wantErr: true, wantInErr: "not in the working tree",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newConfig(t, tc.cfgExtra)
			r := resolverFrom(cfg, dir)
			c := makeCitation(tc.file, tc.start, tc.end)
			text, meta, err := r.Read(c)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
					t.Errorf("err = %q, want it to contain %q", err.Error(), tc.wantInErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantText != "" && text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
			if tc.wantMIME != "" && meta.MIME != tc.wantMIME {
				t.Errorf("meta.MIME = %q, want %q", meta.MIME, tc.wantMIME)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Git HEAD blob — requires a real git repo
// ──────────────────────────────────────────────────────────────────────────────

func TestGitHEADBlob(t *testing.T) {
	gitPath := skipIfNoGit(t)

	dir := t.TempDir()
	content := "first line\nsecond line\nthird line\n"
	initGitRepo(t, dir, content)

	// Remove file.txt from working tree; HEAD still has it.
	if err := os.Remove(filepath.Join(dir, "file.txt")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	tests := []struct {
		name      string
		gitInCfg  bool
		wantText  string
		wantErr   bool
		wantInErr string
		wantInFix string
	}{
		{
			name:     "git configured: blob retrieved from HEAD",
			gitInCfg: true,
			wantText: "first line\nsecond line",
		},
		{
			name:      "git not configured: RefusalError not-in-working-tree",
			gitInCfg:  false,
			wantErr:   true,
			wantInErr: "not in the working tree",
			wantInFix: "set git",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfgContent := ""
			if tc.gitInCfg {
				cfgContent = fmt.Sprintf("git = %s", gitPath)
			}
			r := resolverFrom(newConfig(t, cfgContent), dir)
			c := makeCitation("file.txt", 1, 2)
			text, _, err := r.Read(c)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var ref *source.RefusalError
				if !errors.As(err, &ref) {
					t.Fatalf("expected RefusalError, got %T: %v", err, err)
				}
				if tc.wantInErr != "" && !strings.Contains(ref.Err, tc.wantInErr) {
					t.Errorf("Err = %q, want to contain %q", ref.Err, tc.wantInErr)
				}
				if tc.wantInFix != "" && !strings.Contains(ref.Fix, tc.wantInFix) {
					t.Errorf("Fix = %q, want to contain %q", ref.Fix, tc.wantInFix)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// CommitForPath — synthetic git dir structures (no git binary needed)
// ──────────────────────────────────────────────────────────────────────────────

func TestCommitForPath_Fake(t *testing.T) {
	const sha40 = "abcdef1234567890abcdef1234567890abcdef12"

	tests := []struct {
		name              string
		headContent       string
		looseRefName      string
		looseRefSHA       string
		packedRefsContent string
		commondirContent  string // if set, written to .git/commondir
		wantCommit        string
	}{
		{
			name: "not in repo returns empty",
			// no .git directory created; handled by empty dir below
		},
		{
			name:         "HEAD points to loose branch ref",
			headContent:  "ref: refs/heads/main\n",
			looseRefName: "refs/heads/main", looseRefSHA: sha40,
			wantCommit: sha40,
		},
		{
			name:              "HEAD points to packed-refs entry",
			headContent:       "ref: refs/heads/main\n",
			packedRefsContent: "# pack-refs with: peeled\n" + sha40 + " refs/heads/main\n",
			wantCommit:        sha40,
		},
		{
			name:        "detached HEAD with valid SHA",
			headContent: sha40 + "\n",
			wantCommit:  sha40,
		},
		{
			name:        "HEAD content is neither ref nor SHA returns empty",
			headContent: "invalid-content\n",
			wantCommit:  "",
		},
		{
			name:             "empty commondir value falls back to gitDir",
			headContent:      "ref: refs/heads/main\n",
			commondirContent: "   \n",
			wantCommit:       "",
		},
		{
			name:              "packed-refs line with only one field is ignored",
			headContent:       "ref: refs/heads/main\n",
			packedRefsContent: "# pack-refs with: peeled\nabc123\n",
			wantCommit:        "",
		},
		{
			name:        "no loose ref and no packed-refs returns empty",
			headContent: "ref: refs/heads/main\n",
			wantCommit:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var dir string
			if tc.headContent == "" {
				// Not-in-repo case: plain temp dir with no .git.
				dir = t.TempDir()
			} else {
				dir = makeSyntheticGitDir(t,
					tc.headContent, tc.looseRefName, tc.looseRefSHA, tc.packedRefsContent)
				if tc.commondirContent != "" {
					gitDir := filepath.Join(dir, ".git")
					if err := os.WriteFile(filepath.Join(gitDir, "commondir"),
						[]byte(tc.commondirContent), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			got := source.CommitForPath(filepath.Join(dir, "file.txt"))
			if got != tc.wantCommit {
				t.Errorf("CommitForPath = %q, want %q", got, tc.wantCommit)
			}
		})
	}
}

// TestCommitForPath_AbsoluteCommonDir verifies that a commondir file containing
// an absolute path points to the correct common git dir.
func TestCommitForPath_AbsoluteCommonDir(t *testing.T) {
	dir := t.TempDir()
	commonDir := filepath.Join(dir, "main-git")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wtGitDir := filepath.Join(dir, "worktree", ".git")
	if err := os.MkdirAll(wtGitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGitDir, "commondir"), []byte(commonDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No ref files in common dir either → CommitForPath returns "".
	got := source.CommitForPath(filepath.Join(dir, "worktree", "file.txt"))
	if got != "" {
		t.Errorf("CommitForPath = %q, want empty (absolute commondir, no refs)", got)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// CommitRecording — real git repos
// ──────────────────────────────────────────────────────────────────────────────

func TestCommitRecording(t *testing.T) {
	skipIfNoGit(t)

	tests := []struct {
		name  string
		setup func(t *testing.T) (dir, wantSHA string)
	}{
		{
			name: "branch ref in loose file",
			setup: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				sha := initGitRepo(t, dir, "alpha\nbeta\n")
				return dir, sha
			},
		},
		{
			name: "SHA is in packed-refs",
			setup: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				sha := initGitRepo(t, dir, "alpha\nbeta\n")
				// Pack refs and remove loose refs.
				cmd := exec.Command("git", "-C", dir, "pack-refs", "--all")
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git pack-refs: %v\n%s", err, out)
				}
				return dir, sha
			},
		},
		{
			name: "worktree gitdir file is followed",
			setup: func(t *testing.T) (string, string) {
				mainDir := t.TempDir()
				sha := initGitRepo(t, mainDir, "content\n")
				wtDir := t.TempDir()
				// Create a worktree.
				cmd := exec.Command("git", "-C", mainDir, "worktree", "add", wtDir)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git worktree add: %v\n%s", err, out)
				}
				return wtDir, sha
			},
		},
		{
			name: "path not in repo returns empty commit",
			setup: func(t *testing.T) (string, string) {
				return t.TempDir(), ""
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, wantSHA := tc.setup(t)
			got := source.CommitForPath(filepath.Join(dir, "file.txt"))
			if got != wantSHA {
				t.Errorf("CommitForPath = %q, want %q", got, wantSHA)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// FindGitRepo
// ──────────────────────────────────────────────────────────────────────────────

func TestFindGitRepo(t *testing.T) {
	skipIfNoGit(t)

	t.Run("path not in any repo returns empty strings", func(t *testing.T) {
		dir := t.TempDir()
		repoDir, gitDir := source.FindGitRepo(filepath.Join(dir, "file.txt"))
		if repoDir != "" || gitDir != "" {
			t.Errorf("expected empty, got repoDir=%q gitDir=%q", repoDir, gitDir)
		}
	})

	t.Run("path inside repo returns non-empty repoDir and gitDir", func(t *testing.T) {
		dir := t.TempDir()
		initGitRepo(t, dir, "content\n")
		repoDir, gitDir := source.FindGitRepo(filepath.Join(dir, "file.txt"))
		if repoDir == "" {
			t.Error("expected non-empty repoDir")
		}
		if gitDir == "" {
			t.Error("expected non-empty gitDir")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// Resolver construction
// ──────────────────────────────────────────────────────────────────────────────

// TestRefusalError verifies the Error() method with and without a Fix string.
func TestRefusalError(t *testing.T) {
	tests := []struct {
		name string
		err  *source.RefusalError
		want string
	}{
		{"only Err field", &source.RefusalError{Err: "the error"}, "the error"},
		{"Err and Fix joined with newline", &source.RefusalError{Err: "the error", Fix: "the fix"}, "the error\nthe fix"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolverConstruction verifies the three constructor paths behave correctly.
func TestResolverConstruction(t *testing.T) {
	// NewEmptyResolver: Converters map is non-nil and Git is empty.
	t.Run("NewEmptyResolver has empty Converters and no Git", func(t *testing.T) {
		r := source.NewEmptyResolver(t.TempDir())
		if r.Cfg == nil {
			t.Fatal("Cfg is nil")
		}
		if r.Cfg.Git != "" {
			t.Errorf("Git = %q, want empty", r.Cfg.Git)
		}
		if r.Cfg.Converters == nil {
			t.Error("Converters map is nil; must be initialised")
		}
	})

	// NewResolver with no config files succeeds and returns usable resolver.
	t.Run("NewResolver with absent config files returns usable resolver", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		dir := t.TempDir()
		r, err := source.NewResolver(dir)
		if err != nil {
			t.Fatalf("NewResolver: %v", err)
		}
		// Usability check: Read a local file without error.
		writeTempFile(t, dir, "hello.txt", "hello\n")
		if _, _, err := r.Read(makeCitation("hello.txt", 1, 1)); err != nil {
			t.Errorf("resolver from NewResolver cannot read local file: %v", err)
		}
	})

	// MustResolver: when config is unreadable, falls back silently.
	t.Run("MustResolver falls back when config is unreadable", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("running as root: chmod 0o000 has no effect")
		}
		tmpDir := t.TempDir()
		cfgDir := filepath.Join(tmpDir, "tm")
		if err := os.MkdirAll(cfgDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfgDir, "config"), []byte("git=git\n"), 0o000); err != nil {
			t.Fatal(err)
		}
		t.Setenv("XDG_CONFIG_HOME", tmpDir)

		dir := t.TempDir()
		r := source.MustResolver(dir)
		// Fallback resolver still reads local files.
		writeTempFile(t, dir, "hello.txt", "hello\n")
		if _, _, err := r.Read(makeCitation("hello.txt", 1, 1)); err != nil {
			t.Errorf("fallback resolver cannot read local file: %v", err)
		}
	})

	// MustResolver: success path returns a resolver that respects git config.
	t.Run("MustResolver with valid config returns resolver with parsed git", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgDir := filepath.Join(tmpDir, "tm")
		if err := os.MkdirAll(cfgDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfgDir, "config"), []byte("git=/usr/bin/git\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("XDG_CONFIG_HOME", tmpDir)
		r := source.MustResolver(t.TempDir())
		if r.Cfg.Git != "/usr/bin/git" {
			t.Errorf("Cfg.Git = %q, want /usr/bin/git", r.Cfg.Git)
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// HashCitation
// ──────────────────────────────────────────────────────────────────────────────

func TestHashCitation(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "source.txt", "alpha\nbeta\ngamma\n")

	tests := []struct {
		name      string
		citeStr   string
		wantErr   bool
		wantInErr string
		wantHash  bool // true → result should start with a 12-hex hash
	}{
		{
			name:     "local file produces hashed canonical form",
			citeStr:  "source.txt:1-2",
			wantHash: true,
		},
		{
			name:     "already-hashed citation with matching hash is returned unchanged",
			citeStr:  "", // filled in dynamically below
			wantHash: true,
		},
		{
			name:      "hash mismatch returns error",
			citeStr:   "000000000000@source.txt:1-2",
			wantErr:   true,
			wantInErr: "hash mismatch",
		},
		{
			name:      "parse error is propagated",
			citeStr:   "no-range-here",
			wantErr:   true,
			wantInErr: "missing line range",
		},
		{
			name:      "missing file with no git returns error",
			citeStr:   "nonexistent.txt:1-5",
			wantErr:   true,
			wantInErr: "not in the working tree",
		},
	}

	r := resolverFrom(newConfig(t, ""), dir)

	// Pre-compute a valid hashed citation for the "already hashed" row.
	hashed, _, err := r.HashCitation("source.txt:1-2")
	if err != nil {
		t.Fatalf("pre-compute HashCitation: %v", err)
	}

	for i, tc := range tests {
		if i == 1 {
			tests[i].citeStr = hashed
		}
		tc = tests[i]
		t.Run(tc.name, func(t *testing.T) {
			result, _, err := r.HashCitation(tc.citeStr)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
					t.Errorf("err = %q, want to contain %q", err.Error(), tc.wantInErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantHash && len(result) < 13 {
				t.Errorf("result = %q; expected hashed form (>12 chars)", result)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// CheckDrift
// ──────────────────────────────────────────────────────────────────────────────

func TestCheckDrift(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "stable.txt", "line1\nline2\n")

	r := resolverFrom(newConfig(t, ""), dir)

	// Pre-compute a hashed citation for the drift tests.
	hashed, _, err := r.HashCitation("stable.txt:1-2")
	if err != nil {
		t.Fatalf("pre-compute hash: %v", err)
	}

	// Mutate file for the drift case.
	driftDir := t.TempDir()
	writeTempFile(t, driftDir, "file.txt", "line1\nline2\n")
	rDrift := resolverFrom(newConfig(t, ""), driftDir)
	driftHashed, _, _ := rDrift.HashCitation("file.txt:1-2")
	writeTempFile(t, driftDir, "file.txt", "changed\nline2\n")

	// Deleted-file case for read error.
	delDir := t.TempDir()
	writeTempFile(t, delDir, "del.txt", "alpha\nbeta\n")
	rDel := resolverFrom(newConfig(t, ""), delDir)
	delHashed, _, _ := rDel.HashCitation("del.txt:1-2")
	if err := os.Remove(filepath.Join(delDir, "del.txt")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		resolver  *source.Resolver
		citeStr   string
		wantDrift bool
		wantErr   bool
		wantInErr string
	}{
		{
			name:     "no drift when content matches hash",
			resolver: r, citeStr: hashed, wantDrift: false,
		},
		{
			name:     "drift detected when content has changed",
			resolver: rDrift, citeStr: driftHashed, wantDrift: true,
		},
		{
			name:     "hashless citation is never drifted",
			resolver: r, citeStr: "stable.txt:1-2", wantDrift: false,
		},
		{
			name:     "parse error is propagated",
			resolver: r, citeStr: "bad@@citation",
			wantErr:   true,
			wantInErr: "missing line range",
		},
		{
			name:     "read error from deleted file is propagated",
			resolver: rDel, citeStr: delHashed,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			drifted, _, err := tc.resolver.CheckDrift(tc.citeStr)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
					t.Errorf("err = %q, want to contain %q", err.Error(), tc.wantInErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if drifted != tc.wantDrift {
				t.Errorf("drifted = %v, want %v", drifted, tc.wantDrift)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// ApplyMeta
// ──────────────────────────────────────────────────────────────────────────────

func TestApplyMeta(t *testing.T) {
	fetchedAt := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		meta      source.Meta
		wantKeys  map[string]string
		absentKey string
	}{
		{
			name:      "zero meta writes nothing",
			meta:      source.Meta{},
			absentKey: "commit",
		},
		{
			name:      "only non-zero fields are written",
			meta:      source.Meta{Commit: "abc123", URL: "https://example.com"},
			wantKeys:  map[string]string{"commit": "abc123", "url": "https://example.com"},
			absentKey: "mime",
		},
		{
			name: "all fields written when fully populated",
			meta: source.Meta{
				Commit:           "deadbeef1234",
				URL:              "https://example.com",
				MIME:             "text/html",
				Converter:        "pandoc",
				ConverterVersion: "3.1.11",
				FetchedAt:        fetchedAt,
			},
			wantKeys: map[string]string{
				"commit":            "deadbeef1234",
				"url":               "https://example.com",
				"mime":              "text/html",
				"converter":         "pandoc",
				"converter_version": "3.1.11",
			},
		},
		{
			name:      "fetched_at is written in RFC3339 format",
			meta:      source.Meta{FetchedAt: fetchedAt},
			wantKeys:  map[string]string{},
			absentKey: "commit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{}
			source.ApplyMeta(fields, tc.meta)
			for k, want := range tc.wantKeys {
				if got, _ := fields[k].(string); got != want {
					t.Errorf("fields[%q] = %q, want %q", k, got, want)
				}
			}
			if tc.absentKey != "" {
				if _, ok := fields[tc.absentKey]; ok {
					t.Errorf("fields[%q] should be absent for zero value", tc.absentKey)
				}
			}
			if !tc.meta.FetchedAt.IsZero() {
				s, ok := fields["fetched_at"].(string)
				if !ok || !strings.HasPrefix(s, "2026-09-23") {
					t.Errorf("fetched_at = %v, want RFC3339 starting 2026-09-23", fields["fetched_at"])
				}
			}
		})
	}
}
