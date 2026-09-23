package cli_test

// TestSourceCitationWiring exercises the full source-resolution pipeline
// through tm add. It proves §13.1 RefusalError exits exit 1 and success
// events carry source metadata fields (url, mime, converter,
// converter_version, fetched_at for URI; commit for git paths).
//
// Reuses from cli_test.go: tempErrlog, run.
// Config is injected via t.Setenv("XDG_CONFIG_HOME", …).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/eventlog"
)

// srcWriteScript writes a shell script with the given body and returns its path.
func srcWriteScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name+".sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("srcWriteScript: %v", err)
	}
	return p
}

// srcSetupXDG writes a source config to $dir/tm/config and sets XDG_CONFIG_HOME.
func srcSetupXDG(t *testing.T, cfgContent string) {
	t.Helper()
	xdgDir := t.TempDir()
	tmDir := filepath.Join(xdgDir, "tm")
	if err := os.MkdirAll(tmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmDir, "config"), []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
}

// srcEventFields reads the .mmd.jsonl for mmdFile and returns the fields map
// of the first event whose "ev" key matches evName.
func srcEventFields(t *testing.T, mmdFile, evName string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(eventlog.Path(mmdFile))
	if err != nil {
		t.Fatalf("read eventlog: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row["ev"] == evName {
			return row
		}
	}
	t.Fatalf("no %q event in eventlog", evName)
	return nil
}

// srcInitGitRepo initialises a real git repo in dir, writes file.txt with
// content, commits it, and returns the full commit SHA.
func srcInitGitRepo(t *testing.T, dir, content string) string {
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

// absPath joins dir to the locator portion of a "locator:start-end" cite arg.
// For example absPath("/tmp/d", "file.txt:1-1") → "/tmp/d/file.txt:1-1".
func absPath(dir, citeArg string) string {
	idx := strings.Index(citeArg, ":")
	if idx < 0 {
		return filepath.Join(dir, citeArg)
	}
	return filepath.Join(dir, citeArg[:idx]) + citeArg[idx:]
}

func TestSourceCitationWiring(t *testing.T) {
	// Shared httptest server: text/html on /doc.html, text/plain on /doc.txt.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/doc.html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<p>source text</p>\n")
		case "/doc.txt":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "source text\n")
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	// Pre-build converter scripts shared across rows.
	convDir := t.TempDir()
	cv := srcWriteScript(t, convDir, "conv", "sed 's/<[^>]*>//g'")
	vr := srcWriteScript(t, convDir, "conv_ver", `printf "1.0"`)
	wrongVr := srcWriteScript(t, convDir, "wrong_ver", `printf "2.0"`)

	tests := []struct {
		name       string
		xdgCfg     string // source config; "" = empty config
		citeArg    string // "locator:start-end"; non-http locators get dir prepended
		wantCode   int
		wantInErr  string
		checkEvent func(t *testing.T, mmdFile string)
		needGit    bool // skip if git not on PATH; inits repo in mmdDir
	}{
		{
			name:      "local file not in working tree exits 1",
			citeArg:   "missing.txt:1-1",
			wantCode:  1,
			wantInErr: "not in the working tree",
		},
		{
			name:      "connection refused fetch exits 1",
			citeArg:   "http://127.0.0.1:1:1-1",
			wantCode:  1,
			wantInErr: "fetch",
		},
		{
			name:      "text/html without converter exits 1",
			citeArg:   srv.URL + "/doc.html:1-1",
			wantCode:  1,
			wantInErr: "no converter for text/html",
		},
		{
			name:      "version mismatch exits 1",
			xdgCfg:   fmt.Sprintf("convert text/html=%s\nversion %s=1.0\nversion-cmd %s=%s\n", cv, cv, cv, wrongVr),
			citeArg:   srv.URL + "/doc.html:1-1",
			wantCode:  1,
			wantInErr: "2.0",
		},
		{
			name:   "successful URI add records url/mime/converter/converter_version/fetched_at",
			xdgCfg: fmt.Sprintf("convert text/html=%s\nversion %s=1.0\nversion-cmd %s=%s\n", cv, cv, cv, vr),
			citeArg: srv.URL + "/doc.html:1-1",
			wantCode: 0,
			checkEvent: func(t *testing.T, mmdFile string) {
				fields := srcEventFields(t, mmdFile, "add")
				for _, key := range []string{"url", "mime", "converter", "converter_version", "fetched_at"} {
					if v, ok := fields[key]; !ok || v == "" {
						t.Errorf("add event missing field %q; fields: %v", key, fields)
					}
				}
			},
		},
		{
			name:    "successful path add inside git records commit",
			citeArg: "file.txt:1-1",
			wantCode: 0,
			needGit: true,
			checkEvent: func(t *testing.T, mmdFile string) {
				fields := srcEventFields(t, mmdFile, "add")
				if v, ok := fields["commit"]; !ok || v == "" {
					t.Errorf("add event missing commit field; fields: %v", fields)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.needGit {
				if _, err := exec.LookPath("git"); err != nil {
					t.Skip("git not on PATH")
				}
			}

			// Fresh graph file per sub-test.
			dir := t.TempDir()
			t.Setenv("TM_FILE", "")
			tempErrlog(t) // activates TM_ERRORS for this sub-test

			mmdFile := filepath.Join(dir, "g.mmd")
			if _, errOut, code := run(t, "new", mmdFile); code != 0 {
				t.Fatalf("tm new: exit %d; %s", code, errOut)
			}
			t.Setenv("TM_FILE", mmdFile)

			// Source config.
			xdgCfg := tc.xdgCfg
			if tc.needGit {
				gitBin, _ := exec.LookPath("git")
				if xdgCfg != "" {
					xdgCfg += "\n"
				}
				xdgCfg += fmt.Sprintf("git=%s\n", gitBin)
				srcInitGitRepo(t, dir, "source text\n")
			}
			srcSetupXDG(t, xdgCfg)

			// Resolve citation to absolute path when needed.
			citeArg := tc.citeArg
			if !strings.HasPrefix(citeArg, "http") {
				citeArg = absPath(dir, citeArg)
			}

			_, errOut, code := run(t, "add", "c1", citeArg, "scope text")

			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d; stderr:\n%s", code, tc.wantCode, errOut)
			}
			if tc.wantInErr != "" && !strings.Contains(errOut, tc.wantInErr) {
				t.Errorf("stderr = %q; want to contain %q", errOut, tc.wantInErr)
			}
			if tc.checkEvent != nil {
				tc.checkEvent(t, mmdFile)
			}
		})
	}
}
