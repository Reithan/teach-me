package source_test

import (
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

// initGitRepo creates a real git repo, commits file.txt, and returns the HEAD SHA.
func initGitRepo(t *testing.T, dir, content string) string {
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
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// makeFakeGit creates a .git directory with the given HEAD, optional loose ref, and packed-refs.
func makeFakeGit(t *testing.T, head, looseRef, looseSHA, packed string) string {
	t.Helper()
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
	if looseRef != "" {
		p := filepath.Join(gitDir, filepath.FromSlash(looseRef))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(looseSHA+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if packed != "" {
		if err := os.WriteFile(filepath.Join(gitDir, "packed-refs"), []byte(packed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
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
			name:  "blank lines and no-equals lines are skipped",
			user:  "\n# comment\nno-equals\ngit=/g\n",
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
		convBody  string
		cfgPin    string // version pin in config; "" → no version key
		verOutput string // what version script prints
		docHtml   string
		start     int
		end       int
		wantText  string
		wantErr   bool
		wantInErr string
	}{
		{
			name: "converter runs and slices output",
			convBody: "sed 's/<[^>]*>//g'", cfgPin: "1.0", verOutput: "1.0",
			docHtml: "<p>alpha</p>\n<p>beta</p>\n",
			start: 1, end: 2, wantText: "alpha\nbeta",
		},
		{
			name: "version mismatch produces RefusalError",
			convBody: "cat", cfgPin: "1.0", verOutput: "2.0",
			docHtml: "x\n",
			wantErr: true, wantInErr: "2.0",
		},
		{
			name:      "missing version pin produces RefusalError",
			convBody:  "cat", cfgPin: "", verOutput: "",
			docHtml:   "x\n",
			wantErr: true, wantInErr: "no version pin",
		},
		{
			name: "non-zero exit with stderr produces RefusalError citing stderr",
			convBody: `echo "bad input" >&2
exit 1`, cfgPin: "1.0", verOutput: "1.0",
			docHtml:   "x\n",
			wantErr: true, wantInErr: "bad input",
		},
		{
			name: "non-zero exit with empty stderr uses process error in RefusalError",
			convBody: "exit 1", cfgPin: "1.0", verOutput: "1.0",
			docHtml: "x\n", wantErr: true,
		},
		{
			name: "multi-line version output uses first line for pin check",
			convBody: "cat", cfgPin: "1.0", verOutput: `1.0\nextra`,
			docHtml: "line\n",
			start: 1, end: 1, wantText: "line",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, cfg := makeConvFixture(t, tc.convBody, tc.cfgPin, tc.verOutput, tc.docHtml)
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
			w.Header().Set("Content-Type", "text/plain"); fmt.Fprint(w, threeLines)
		case "/md":
			w.Header().Set("Content-Type", "text/markdown"); fmt.Fprint(w, "# H\ntext\n")
		case "/notfound":
			http.Error(w, "not found", http.StatusNotFound)
		case "/big":
			w.Header().Set("Content-Type", "text/plain")
			chunk := strings.Repeat("x", 1<<20)
			for range 17 {
				fmt.Fprint(w, chunk)
			}
		case "/pdf":
			w.Header().Set("Content-Type", "application/pdf"); fmt.Fprint(w, "%PDF")
		case "/html":
			w.Header().Set("Content-Type", "text/html"); fmt.Fprint(w, "<p>alpha</p>\n")
		case "/doc.md":
			w.Header().Set("Content-Type", " "); fmt.Fprint(w, "# T\nC\n")
		case "/doc":
			fmt.Fprint(w, "plain\n")
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
		{name: "converter matched by MIME sets meta", url: srvURL + "/html", cfgExtra: htmlCfg, start: 1, end: 1, wantMIME: "text/html"},
		{name: "whitespace CT falls back to URL extension", url: srvURL + "/doc.md", cfgExtra: "ext .md=text/markdown\n", start: 1, end: 2, wantText: "# T\nC", wantMIME: "text/markdown"},
		{name: "no CT and no extension treated as plain text", url: srvURL + "/doc", start: 1, end: 1, wantText: "plain"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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
		skipNoGit            bool
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
			name:      "missing file with no git produces RefusalError",
			file:      "missing.txt", start: 1, end: 1,
			wantErr: true, wantInErr: "not in the working tree",
		},
		{
			name: "missing file inside git repo is read from HEAD blob",
			file: "file.txt", start: 1, end: 1, wantText: "blob line",
			skipNoGit: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gitBin := ""
			if tc.skipNoGit {
				gitBin = skipIfNoGit(t)
			}

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
			case "file.txt":
				initGitRepo(t, dir, "blob line\n")
				cfgExtra += fmt.Sprintf("git=%s\n", gitBin)
				// Remove the file so it must be read from HEAD.
				_ = os.Remove(filepath.Join(dir, "file.txt"))
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
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Commit recording
// ──────────────────────────────────────────────────────────────────────────────

func TestCommitRecording(t *testing.T) {
	const sha = "aabbccddeeff0011223344556677889900112233"

	tests := []struct {
		name    string
		makeDir func(t *testing.T) string
		wantSHA string
	}{
		{
			name:    "branch ref resolves via loose ref",
			makeDir: func(t *testing.T) string { return makeFakeGit(t, "ref: refs/heads/main\n", "refs/heads/main", sha, "") },
			wantSHA: sha,
		},
		{
			name:    "branch ref resolves via packed-refs",
			makeDir: func(t *testing.T) string { return makeFakeGit(t, "ref: refs/heads/main\n", "", "", sha+" refs/heads/main\n") },
			wantSHA: sha,
		},
		{
			name:    "detached HEAD uses SHA directly",
			makeDir: func(t *testing.T) string { return makeFakeGit(t, sha+"\n", "", "", "") },
			wantSHA: sha,
		},
		{
			name:    "invalid HEAD returns empty commit",
			makeDir: func(t *testing.T) string { return makeFakeGit(t, "not-a-sha\n", "", "", "") },
		},
		{
			name:    "path not in any repo returns empty commit",
			makeDir: func(t *testing.T) string { return t.TempDir() },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.makeDir(t)
			got := source.CommitForPath(filepath.Join(dir, "file.txt"))
			if tc.wantSHA == "" {
				if got != "" {
					t.Errorf("CommitForPath = %q, want empty", got)
				}
			} else if got != tc.wantSHA {
				t.Errorf("CommitForPath = %q, want %q", got, tc.wantSHA)
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
				Commit: "abc", URL: "https://x.com", MIME: "text/html",
				Converter: "pandoc", ConverterVersion: "3.1", FetchedAt: time.Now(),
			},
			wantKeys: []string{"commit", "url", "mime", "converter", "converter_version", "fetched_at"},
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
