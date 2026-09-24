package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reithan/teach-me/internal/config"
)

// isolate points XDG_CONFIG_HOME and the working directory at fresh temp
// dirs and returns the user config path.
func isolate(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	return config.UserPath()
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLookup(t *testing.T) {
	tests := []struct {
		name    string
		user    string // user config content; "" = absent
		local   string // .tmconfig content; "" = absent
		key     string
		wantVal string
		wantOK  bool
	}{
		{name: "both absent", key: "file", wantOK: false},
		{name: "user only", user: "file = /u/g.mmd\n", key: "file", wantVal: "/u/g.mmd", wantOK: true},
		{name: "local overrides user", user: "file = /u/g.mmd\n", local: "file=/l/g.mmd\n", key: "file", wantVal: "/l/g.mmd", wantOK: true},
		{name: "local lacks key falls through", user: "src-root = /src\n", local: "file=/l/g.mmd\n", key: "src-root", wantVal: "/src", wantOK: true},
		{name: "comments and blanks ignored", user: "# file = /no\n\n  file = /yes  \n", key: "file", wantVal: "/yes", wantOK: true},
		{name: "last value wins", user: "file = /a\nfile = /b\n", key: "file", wantVal: "/b", wantOK: true},
		{name: "empty value is absent", user: "file =\n", key: "file", wantOK: false},
		{name: "converter keys untouched", user: "convert text/html = pandoc\n", key: "file", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userPath := isolate(t)
			if tc.user != "" {
				write(t, userPath, tc.user)
			}
			if tc.local != "" {
				write(t, config.LocalName, tc.local)
			}
			got, ok, err := config.Lookup(tc.key)
			if err != nil {
				t.Fatalf("Lookup: %v", err)
			}
			if ok != tc.wantOK || got != tc.wantVal {
				t.Errorf("Lookup(%q) = %q, %v; want %q, %v", tc.key, got, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}

func TestLookup_UnreadableIsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	userPath := isolate(t)
	write(t, userPath, "file = /x\n")
	if err := os.Chmod(userPath, 0o000); err != nil {
		t.Fatal(err)
	}
	if _, _, err := config.Lookup("file"); err == nil {
		t.Fatal("want error for unreadable config")
	}
}

func TestSet(t *testing.T) {
	tests := []struct {
		name    string
		before  string // "" = file absent
		kv      map[string]string
		wantOut string
	}{
		{
			name:    "creates file and parent dir",
			kv:      map[string]string{"file": "/g.mmd"},
			wantOut: "file = /g.mmd\n",
		},
		{
			name:    "replaces in place and keeps other lines",
			before:  "# converters\nconvert text/html = pandoc\nfile=/old.mmd\ngit = git\n",
			kv:      map[string]string{"file": "/new.mmd"},
			wantOut: "# converters\nconvert text/html = pandoc\nfile = /new.mmd\ngit = git\n",
		},
		{
			name:    "appends missing keys in sorted order",
			before:  "git = git\n",
			kv:      map[string]string{"src-root": "/src", "file": "/g.mmd"},
			wantOut: "git = git\nfile = /g.mmd\nsrc-root = /src\n",
		},
		{
			name:    "handles missing trailing newline",
			before:  "git = git",
			kv:      map[string]string{"file": "/g.mmd"},
			wantOut: "git = git\nfile = /g.mmd\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nested", "config")
			if tc.before != "" {
				write(t, path, tc.before)
			}
			if err := config.Set(path, tc.kv); err != nil {
				t.Fatalf("Set: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.wantOut {
				t.Errorf("Set output:\n%s\nwant:\n%s", got, tc.wantOut)
			}
			// No temp files left behind.
			entries, _ := os.ReadDir(filepath.Dir(path))
			if len(entries) != 1 {
				t.Errorf("want only the config in %s, got %d entries", filepath.Dir(path), len(entries))
			}
		})
	}
}

func TestSrcRootAndDoc_Precedence(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		user     string
		wantSrc  string
		wantDoc  string
		graphDir string
	}{
		{name: "env wins", env: "/env", user: "src-root = /cfg\ndoc = /cfg/SKILL.md\n", graphDir: "/g", wantSrc: "/env", wantDoc: "/env/SKILL.md"},
		{name: "config when env unset", user: "src-root = /cfg\ndoc = /cfg/SKILL.md\n", graphDir: "/g", wantSrc: "/cfg", wantDoc: "/cfg/SKILL.md"},
		{name: "graph dir when nothing set", graphDir: "/g", wantSrc: "/g", wantDoc: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userPath := isolate(t)
			t.Setenv("TM_SRC_ROOT", tc.env)
			t.Setenv("TM_DOC", "")
			if tc.env != "" {
				t.Setenv("TM_DOC", tc.env+"/SKILL.md")
			}
			if tc.user != "" {
				write(t, userPath, tc.user)
			}
			if got := config.SrcRoot(tc.graphDir); got != tc.wantSrc {
				t.Errorf("SrcRoot = %q, want %q", got, tc.wantSrc)
			}
			if got := config.Doc(); got != tc.wantDoc {
				t.Errorf("Doc = %q, want %q", got, tc.wantDoc)
			}
		})
	}
}
