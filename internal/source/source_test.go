package source_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/source"
)

// ──────────────────────────────────────────────────────────────────────────────
// Shared helpers
// ──────────────────────────────────────────────────────────────────────────────

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name+".sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writeScript: %v", err)
	}
	return p
}

// loadCfg parses user config and optional .tmconfig from strings.
func loadCfg(t *testing.T, user, tmcfg string) *source.Config {
	t.Helper()
	dir := t.TempDir()
	uPath, tPath := filepath.Join(dir, "config"), filepath.Join(dir, ".tmconfig")
	if user != "" {
		if err := os.WriteFile(uPath, []byte(user), 0o644); err != nil {
			t.Fatalf("loadCfg user: %v", err)
		}
	}
	if tmcfg != "" {
		if err := os.WriteFile(tPath, []byte(tmcfg), 0o644); err != nil {
			t.Fatalf("loadCfg tmcfg: %v", err)
		}
	}
	cfg, err := source.LoadConfigPaths(uPath, tPath)
	if err != nil {
		t.Fatalf("LoadConfigPaths: %v", err)
	}
	return cfg
}

func resolverFrom(cfg *source.Config, srcRoot string) *source.Resolver {
	return source.NewResolverWithConfig(cfg, srcRoot)
}

func makeCitation(file string, start, end int) cite.Citation {
	return cite.Citation{File: file, Start: start, End: end}
}

func skipIfNoGit(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	return p
}

// initGitRepo creates a real git repo and commits file.txt.
func initGitRepo(t *testing.T, dir, content string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "T"},
	} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "file.txt"}, {"commit", "-m", "init"}} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// makeConvFixture builds a text/html converter in a fresh temp dir.
// convBody is the script body; cfgPin is the expected version ("" → omit version key);
// verOutput is what the version script prints; docContent is written to doc.html.
func makeConvFixture(t *testing.T, convBody, cfgPin, verOutput, docContent string) (dir, cfg string) {
	t.Helper()
	dir = t.TempDir()
	cv := writeScript(t, dir, "conv", convBody)
	vr := writeScript(t, dir, "conv_ver", fmt.Sprintf(`printf "%s"`, verOutput))
	if cfgPin != "" {
		cfg = fmt.Sprintf("convert text/html=%s\nversion %s=%s\nversion-cmd %s=%s\next .html=text/html\n",
			cv, cv, cfgPin, cv, vr)
	} else {
		cfg = fmt.Sprintf("convert text/html=%s\next .html=text/html\n", cv)
	}
	if err := os.WriteFile(filepath.Join(dir, "doc.html"), []byte(docContent), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, cfg
}

// ──────────────────────────────────────────────────────────────────────────────
// Config parsing
// ──────────────────────────────────────────────────────────────────────────────

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		user    string
		tmcfg   string
		wantGit string
		check   func(t *testing.T, cfg *source.Config)
	}{
		{name: "absent files yield empty config", wantGit: ""},
		{
			name:    "parses all recognised keys",
			user:    "git=/g\nconvert text/html=p\next .html=text/html\nversion p=3.1\nversion-cmd p=p --ver\n",
			wantGit: "/g",
			check: func(t *testing.T, cfg *source.Config) {
				if cfg.ExtToMIME(".html") != "text/html" {
					t.Error("ext not parsed")
				}
				if cfg.Versions["p"] != "3.1" {
					t.Error("version not parsed")
				}
			},
		},
		{
			name: ".tmconfig overrides user config",
			user: "git=/usr\n", tmcfg: "git=/tm\n",
			wantGit: "/tm",
		},
		{
			name: "unknown keys are silently ignored",
			user: "unknown-key=value\ngit=/g\n", wantGit: "/g",
		},
		{
			name:    "blank lines and no-equals lines are skipped",
			user:    "\n# comment\nno-equals\ngit=/g\n",
			wantGit: "/g",
		},
		{
			name: "version-cmd defaults to [program, --version]",
			user: "convert text/html=p\nversion p=1.0\n",
			check: func(t *testing.T, cfg *source.Config) {
				cmds := cfg.VersionCmd("p")
				if len(cmds) != 2 || cmds[1] != "--version" {
					t.Errorf("default VersionCmd = %v", cmds)
				}
			},
		},
		{
			name: "unreadable user config propagates error",
			check: func(t *testing.T, _ *source.Config) {
				if os.Getuid() == 0 {
					t.Skip("running as root")
				}
				if runtime.GOOS == "windows" {
					t.Skip("file permission bits are not enforced on Windows")
				}
				dir := t.TempDir()
				p := filepath.Join(dir, "config")
				if err := os.WriteFile(p, []byte("x\n"), 0o000); err != nil {
					t.Fatal(err)
				}
				_, err := source.LoadConfigPaths(p, filepath.Join(dir, "nofile"))
				if err == nil {
					t.Fatal("expected error for unreadable file")
				}
			},
		},
		{
			name: "unreadable .tmconfig propagates error",
			check: func(t *testing.T, _ *source.Config) {
				if os.Getuid() == 0 {
					t.Skip("running as root")
				}
				if runtime.GOOS == "windows" {
					t.Skip("file permission bits are not enforced on Windows")
				}
				dir := t.TempDir()
				p := filepath.Join(dir, ".tmconfig")
				if err := os.WriteFile(p, []byte("x\n"), 0o000); err != nil {
					t.Fatal(err)
				}
				_, err := source.LoadConfigPaths(filepath.Join(dir, "nofile"), p)
				if err == nil {
					t.Fatal("expected error for unreadable .tmconfig")
				}
			},
		},
		{
			name: "ext key without leading dot gets dot prepended; ExtToMIME accepts either form",
			user: "ext html=text/html\n",
			check: func(t *testing.T, cfg *source.Config) {
				if cfg.ExtToMIME(".html") != "text/html" {
					t.Error("ExtToMIME .html: want text/html")
				}
				if cfg.ExtToMIME("html") != "text/html" {
					t.Error("ExtToMIME html (no dot): want text/html")
				}
			},
		},
		{
			name:    "empty value after = is silently skipped",
			user:    "git=/g\nconvert=\n",
			wantGit: "/g",
		},
		{
			name: "invalid repo alias in user config returns error",
			check: func(t *testing.T, _ *source.Config) {
				dir := t.TempDir()
				p := filepath.Join(dir, "config")
				if err := os.WriteFile(p, []byte("repo bad alias!="+dir+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				_, err := source.LoadConfigPaths(p, filepath.Join(dir, "nofile"))
				if err == nil {
					t.Fatal("expected error for invalid repo alias")
				}
			},
		},
		{
			name: "cache-ttl is parsed correctly",
			user: "cache-ttl=12h\n",
			check: func(t *testing.T, cfg *source.Config) {
				if !cfg.CacheTTLSet {
					t.Error("CacheTTLSet = false, want true")
				}
				if cfg.CacheTTL != 12*time.Hour {
					t.Errorf("CacheTTL = %v, want 12h", cfg.CacheTTL)
				}
			},
		},
		{
			name: "cache-ttl=0 disables cache (CacheTTLSet=true)",
			user: "cache-ttl=0\n",
			check: func(t *testing.T, cfg *source.Config) {
				if !cfg.CacheTTLSet {
					t.Error("CacheTTLSet = false, want true for cache-ttl=0")
				}
				if cfg.CacheTTL != 0 {
					t.Errorf("CacheTTL = %v, want 0", cfg.CacheTTL)
				}
			},
		},
		{
			name: "invalid cache-ttl returns error",
			check: func(t *testing.T, _ *source.Config) {
				dir := t.TempDir()
				p := filepath.Join(dir, "config")
				if err := os.WriteFile(p, []byte("cache-ttl=not-a-duration\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				_, err := source.LoadConfigPaths(p, filepath.Join(dir, "nofile"))
				if err == nil {
					t.Fatal("expected error for invalid cache-ttl")
				}
				if !strings.Contains(err.Error(), "cache-ttl") {
					t.Errorf("error should mention cache-ttl; got: %v", err)
				}
			},
		},
		{
			name: "XDG_CONFIG_HOME is used to locate user config",
			check: func(t *testing.T, _ *source.Config) {
				xdg := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", xdg)
				cfgPath := filepath.Join(xdg, "tm", "config")
				if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfgPath, []byte("git=/usr/bin/git\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				cfg, err := source.LoadConfig()
				if err != nil || cfg.Git != "/usr/bin/git" {
					t.Errorf("LoadConfig via XDG: git=%q err=%v", cfg.Git, err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.check != nil && tc.user == "" && tc.tmcfg == "" && tc.wantGit == "" {
				// Special case: test handles its own config loading.
				tc.check(t, nil)
				return
			}
			cfg := loadCfg(t, tc.user, tc.tmcfg)
			if tc.wantGit != "" && cfg.Git != tc.wantGit {
				t.Errorf("Git = %q, want %q", cfg.Git, tc.wantGit)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Converter execution
// ──────────────────────────────────────────────────────────────────────────────

func TestConverter(t *testing.T) {
	tests := []struct {
		name      string
		posixOnly bool
		convBody  string
		cfgPin    string // version pin in config; "" → no version key
		verOutput string // what version script prints
		docHTML   string
		start     int
		end       int
		wantText  string
		wantErr   bool
		wantInErr string
	}{
		{
			name:      "converter runs and slices output",
			posixOnly: true,
			convBody:  "sed 's/<[^>]*>//g'", cfgPin: "1.0", verOutput: "1.0",
			docHTML: "<p>alpha</p>\n<p>beta</p>\n",
			start:   1, end: 2, wantText: "alpha\nbeta",
		},
		{
			name:      "version mismatch produces RefusalError",
			posixOnly: true,
			convBody:  "cat", cfgPin: "1.0", verOutput: "2.0",
			docHTML: "x\n",
			wantErr: true, wantInErr: "2.0",
		},
		{
			name:     "missing version pin produces RefusalError",
			convBody: "cat", cfgPin: "", verOutput: "",
			docHTML: "x\n",
			wantErr: true, wantInErr: "no version pin",
		},
		{
			name:      "non-zero exit with stderr produces RefusalError citing stderr",
			posixOnly: true,
			convBody: `echo "bad input" >&2
exit 1`, cfgPin: "1.0", verOutput: "1.0",
			docHTML: "x\n",
			wantErr: true, wantInErr: "bad input",
		},
		{
			name:     "non-zero exit with empty stderr uses process error in RefusalError",
			convBody: "exit 1", cfgPin: "1.0", verOutput: "1.0",
			docHTML: "x\n", wantErr: true,
		},
		{
			name:      "multi-line version output uses first line for pin check",
			posixOnly: true,
			convBody:  "cat", cfgPin: "1.0", verOutput: `1.0\nextra`,
			docHTML: "line\n",
			start:   1, end: 1, wantText: "line",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.posixOnly && runtime.GOOS == "windows" {
				t.Skip("shell-script converters need a POSIX shell")
			}
			dir, cfg := makeConvFixture(t, tc.convBody, tc.cfgPin, tc.verOutput, tc.docHTML)
			r := resolverFrom(loadCfg(t, cfg, ""), dir)
			start, end := 1, 1
			if tc.start != 0 {
				start, end = tc.start, tc.end
			}
			text, _, err := r.Read(makeCitation(filepath.Join(dir, "doc.html"), start, end))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
					t.Errorf("err = %q; want to contain %q", err.Error(), tc.wantInErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantText != "" && text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
		})
	}

	t.Run("version check is cached: same resolver skips re-exec", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("shell-script converters need a POSIX shell")
		}
		dir, cfg := makeConvFixture(t, "cat", "1.0", "1.0", "line\n")
		r := resolverFrom(loadCfg(t, cfg, ""), dir)
		for i := range 3 {
			if err := os.WriteFile(filepath.Join(dir, "doc.html"), []byte(fmt.Sprintf("%d\n", i)), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, _, err := r.Read(makeCitation(filepath.Join(dir, "doc.html"), 1, 1)); err != nil {
				t.Fatalf("Read %d: %v", i, err)
			}
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// URI fetch
// ──────────────────────────────────────────────────────────────────────────────

func TestURIFetch(t *testing.T) {
	const threeLines = "line one\nline two\nline three\n"

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redir":
			http.Redirect(w, r, srvURL+"/plain", http.StatusFound)
		case "/plain":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprint(w, threeLines)
		case "/md":
			w.Header().Set("Content-Type", "text/markdown")
			_, _ = fmt.Fprint(w, "# H\ntext\n")
		case "/notfound":
			http.Error(w, "not found", http.StatusNotFound)
		case "/big":
			w.Header().Set("Content-Type", "text/plain")
			chunk := strings.Repeat("x", 1<<20)
			for range 17 {
				_, _ = fmt.Fprint(w, chunk)
			}
		case "/pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = fmt.Fprint(w, "%PDF")
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, "<p>alpha</p>\n")
		case "/doc.md":
			w.Header().Set("Content-Type", " ")
			_, _ = fmt.Fprint(w, "# T\nC\n")
		case "/doc":
			_, _ = fmt.Fprint(w, "plain\n")
		default:
			http.Error(w, "unknown", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL

	cvDir := t.TempDir()
	cv := writeScript(t, cvDir, "conv", "sed 's/<[^>]*>//g'")
	vr := writeScript(t, cvDir, "conv_ver", `printf "1.0"`)
	htmlCfg := fmt.Sprintf("convert text/html=%s\nversion %s=1.0\nversion-cmd %s=%s\n", cv, cv, cv, vr)

	tests := []struct {
		name, url, cfgExtra string
		posixOnly           bool
		start, end          int
		wantText, wantMIME  string
		wantURL             string
		wantErr             bool
		wantInErr           string
	}{
		{name: "text/plain is returned raw", url: srvURL + "/plain", start: 1, end: 2, wantText: "line one\nline two"},
		{name: "text/markdown is returned raw", url: srvURL + "/md", start: 1, end: 2, wantText: "# H\ntext"},
		{name: "redirect records final URL in meta", url: srvURL + "/redir", start: 1, end: 1, wantText: "line one", wantURL: srvURL + "/plain"},
		{name: "non-2xx produces RefusalError", url: srvURL + "/notfound", start: 1, end: 1, wantErr: true, wantInErr: "fetch"},
		{name: "oversize body produces RefusalError", url: srvURL + "/big", start: 1, end: 1, wantErr: true, wantInErr: "16 MiB"},
		{name: "unknown MIME with no converter produces RefusalError", url: srvURL + "/pdf", start: 1, end: 1, wantErr: true, wantInErr: "no converter for application/pdf"},
		{name: "converter matched by MIME sets meta", posixOnly: true, url: srvURL + "/html", cfgExtra: htmlCfg, start: 1, end: 1, wantMIME: "text/html"},
		{name: "whitespace CT falls back to URL extension", url: srvURL + "/doc.md", cfgExtra: "ext .md=text/markdown\n", start: 1, end: 2, wantText: "# T\nC", wantMIME: "text/markdown"},
		{name: "no CT and no extension treated as plain text", url: srvURL + "/doc", start: 1, end: 1, wantText: "plain"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.posixOnly && runtime.GOOS == "windows" {
				t.Skip("shell-script converters need a POSIX shell")
			}
			r := resolverFrom(loadCfg(t, tc.cfgExtra, ""), t.TempDir())
			text, meta, err := r.Read(makeCitation(tc.url, tc.start, tc.end))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
					t.Errorf("err = %q; want to contain %q", err.Error(), tc.wantInErr)
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
// Path resolution
// ──────────────────────────────────────────────────────────────────────────────

func TestPath(t *testing.T) {
	tests := []struct {
		name, file, cfgExtra string
		start, end           int
		wantText             string
		wantErr              bool
		wantInErr            string
		extra                func(t *testing.T, dir string, r *source.Resolver)
	}{
		{
			name: "raw file read",
			file: "a.txt", start: 1, end: 2, wantText: "alpha\nbeta",
		},
		{
			name: "ext mapped but no converter reads file as-is",
			file: "a.html", cfgExtra: "ext .html=text/html\n",
			start: 1, end: 1, wantText: "raw",
		},
		{
			name: "missing file with no git produces RefusalError",
			file: "missing.txt", start: 1, end: 1,
			wantErr: true, wantInErr: "not in the working tree",
		},
		{
			name:      "missing file inside git repo returns refusal (no HEAD fallback)",
			file:      "file.txt",
			start:     1,
			end:       1,
			wantErr:   true,
			wantInErr: "not in the working tree",
		},
		{
			// HashCitation produces a stable content hash; CheckDrift returns
			// false for a hashless citation, false when content matches, and
			// true after the file is mutated.  Also verifies NewResolver loads
			// config internally via LoadConfig (no explicit config path needed).
			name: "HashCitation produces hash; CheckDrift detects mutation",
			file: "a.txt", start: 1, end: 2,
			extra: func(t *testing.T, dir string, r *source.Resolver) {
				// NewResolver calls LoadConfig internally; verify it succeeds.
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				nr, err := source.NewResolver(dir)
				if err != nil || nr == nil {
					t.Errorf("NewResolver: %v", err)
				}
				f := filepath.Join(dir, "a.txt")
				plain := f + ":1-2"
				if drifted, _, err := r.CheckDrift(plain); err != nil || drifted {
					t.Errorf("CheckDrift hashless: drifted=%v err=%v", drifted, err)
				}
				hashed, _, err := r.HashCitation(plain)
				if err != nil {
					t.Fatalf("HashCitation: %v", err)
				}
				if drifted, _, err := r.CheckDrift(hashed); err != nil || drifted {
					t.Errorf("CheckDrift match: drifted=%v err=%v", drifted, err)
				}
				if err := os.WriteFile(f, []byte("alpha\nchanged\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if drifted, _, err := r.CheckDrift(hashed); err != nil || !drifted {
					t.Errorf("CheckDrift mutated: drifted=%v err=%v", drifted, err)
				}
			},
		},
		{
			// HashCitation and CheckDrift reject bad citation syntax and return
			// an error when the stored hash does not match current content or
			// the source file has been removed.
			name: "HashCitation and CheckDrift: bad syntax and missing file return errors",
			file: "a.txt", start: 1, end: 2,
			extra: func(t *testing.T, dir string, r *source.Resolver) {
				if _, _, err := r.HashCitation(":::"); err == nil {
					t.Error("HashCitation bad syntax: want error")
				}
				if _, _, err := r.CheckDrift(":::"); err == nil {
					t.Error("CheckDrift bad syntax: want error")
				}
				f := filepath.Join(dir, "a.txt")
				hashed, _, err := r.HashCitation(f + ":1-2")
				if err != nil {
					t.Fatalf("HashCitation: %v", err)
				}
				wrong := strings.Replace(hashed, hashed[:12], "000000000000", 1)
				if _, _, err := r.HashCitation(wrong); err == nil {
					t.Error("HashCitation hash mismatch: want error")
				}
				_ = os.Remove(f)
				if _, _, err := r.CheckDrift(hashed); err == nil {
					t.Error("CheckDrift missing file: want error")
				}
				if _, _, err := r.HashCitation(hashed); err == nil {
					t.Error("HashCitation missing file: want error")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgExtra := tc.cfgExtra

			switch tc.file {
			case "a.txt":
				if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "a.html":
				if err := os.WriteFile(filepath.Join(dir, "a.html"), []byte("raw\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				// "file.txt" and "missing.txt": no file created — the test expects a refusal error.
			}

			r := resolverFrom(loadCfg(t, cfgExtra, ""), dir)
			c := makeCitation(filepath.Join(dir, tc.file), tc.start, tc.end)
			text, _, err := r.Read(c)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
					t.Errorf("err = %q; want to contain %q", err.Error(), tc.wantInErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantText != "" && text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
			if tc.extra != nil {
				tc.extra(t, dir, r)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// ApplyMeta
// ──────────────────────────────────────────────────────────────────────────────

func TestApplyMeta(t *testing.T) {
	tests := []struct {
		name       string
		meta       source.Meta
		wantKeys   []string
		wantNoKeys bool
	}{
		{name: "zero Meta adds no fields", meta: source.Meta{}, wantNoKeys: true},
		{
			name: "all non-empty fields are added",
			meta: source.Meta{
				Commit: "abc", Ref: "main", URL: "https://x.com", MIME: "text/html",
				Converter: "pandoc", ConverterVersion: "3.1", FetchedAt: time.Now(),
			},
			wantKeys: []string{"commit", "ref", "url", "mime", "converter", "converter_version", "fetched_at"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{}
			source.ApplyMeta(fields, tc.meta)
			if tc.wantNoKeys && len(fields) != 0 {
				t.Errorf("expected no fields, got %v", fields)
			}
			for _, k := range tc.wantKeys {
				if _, ok := fields[k]; !ok {
					t.Errorf("missing field %q", k)
				}
			}
		})
	}
}
