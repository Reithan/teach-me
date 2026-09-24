package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/config"
)

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
}

func TestUserPath_HomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".config", "tm", "config")
	if got := config.UserPath(); got != want {
		t.Errorf("UserPath = %q, want %q", got, want)
	}
}

func TestLookup_MalformedLines(t *testing.T) {
	userPath := isolate(t)
	// A line without '=' is skipped; the valid line after it still counts.
	write(t, userPath, "this line has no equals sign\nfile = /g.mmd\n")
	got, ok, err := config.Lookup("file")
	if err != nil || !ok || got != "/g.mmd" {
		t.Errorf("Lookup = %q, %v, %v; want /g.mmd, true, nil", got, ok, err)
	}
}

func TestLookup_OverlongLineIsError(t *testing.T) {
	userPath := isolate(t)
	// bufio.Scanner refuses tokens over 64 KiB; the error must surface.
	write(t, userPath, "file = "+strings.Repeat("x", 70_000)+"\n")
	if _, _, err := config.Lookup("file"); err == nil {
		t.Fatal("want error for an overlong config line")
	}
}

func TestSet_Errors(t *testing.T) {
	skipIfRoot(t)
	tests := []struct {
		name  string
		setup func(t *testing.T, base string) (path string)
	}{
		{
			name: "existing file unreadable",
			setup: func(t *testing.T, base string) string {
				p := filepath.Join(base, "config")
				write(t, p, "git = git\n")
				if err := os.Chmod(p, 0o000); err != nil {
					t.Fatal(err)
				}
				return p
			},
		},
		{
			name: "parent not writable so mkdir fails",
			setup: func(t *testing.T, base string) string {
				ro := filepath.Join(base, "ro")
				if err := os.Mkdir(ro, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
				return filepath.Join(ro, "tm", "config")
			},
		},
		{
			name: "directory not writable so temp file fails",
			setup: func(t *testing.T, base string) string {
				dir := filepath.Join(base, "ro")
				if err := os.Mkdir(dir, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
				return filepath.Join(dir, "config")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			path := tc.setup(t, base)
			if err := config.Set(path, map[string]string{"file": "/g.mmd"}); err == nil {
				t.Fatal("want error from Set")
			}
			// No temp file may be left behind next to the target.
			entries, _ := os.ReadDir(filepath.Dir(path))
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".config-") {
					t.Errorf("temp file left behind: %s", e.Name())
				}
			}
		})
	}
}
