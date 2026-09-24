package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/config"
	"github.com/reithan/teach-me/internal/docver"
	"github.com/reithan/teach-me/internal/version"
)

// freshConfig isolates the user config and clears the env vars that would
// otherwise short-circuit config lookups.
func freshConfig(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_SRC_ROOT", "")
	t.Setenv("TM_DOC", "")
}

// TestPointer_HoldsAcrossWorkingDirectories: tm new from one directory makes
// the graph resolvable from another, with no .tmconfig anywhere.
func TestPointer_HoldsAcrossWorkingDirectories(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	lesson := t.TempDir()
	file := filepath.Join(lesson, "g.mmd")

	t.Chdir(t.TempDir())
	if _, errOut, code := run(t, "new", file); code != 0 {
		t.Fatalf("tm new: exit %d; %s", code, errOut)
	}

	t.Chdir(t.TempDir())
	if _, errOut, code := run(t, "status"); code != 0 {
		t.Fatalf("tm status from another cwd: exit %d; %s", code, errOut)
	}
}

// TestPointer_Local: --local writes .tmconfig in the cwd as given and leaves
// the user config untouched, for both new and load.
func TestPointer_Local(t *testing.T) {
	for _, cmd := range []string{"new", "load"} {
		t.Run(cmd, func(t *testing.T) {
			freshConfig(t)
			tempErrlog(t)
			dir := t.TempDir()
			t.Chdir(dir)
			if cmd == "load" {
				if _, errOut, code := run(t, "new", "g.mmd", "--local"); code != 0 {
					t.Fatalf("seed tm new: exit %d; %s", code, errOut)
				}
				if err := os.Remove(config.LocalName); err != nil {
					t.Fatal(err)
				}
			}
			if _, errOut, code := run(t, cmd, "g.mmd", "--local"); code != 0 {
				t.Fatalf("tm %s --local: exit %d; %s", cmd, code, errOut)
			}
			data, err := os.ReadFile(config.LocalName)
			if err != nil {
				t.Fatalf("read .tmconfig: %v", err)
			}
			if !strings.Contains(string(data), "file = g.mmd\n") {
				t.Errorf(".tmconfig should hold the path as given; got:\n%s", data)
			}
			if _, err := os.Stat(config.UserPath()); !os.IsNotExist(err) {
				t.Errorf("user config must not be written with --local")
			}
			if _, errOut, code := run(t, "status"); code != 0 {
				t.Fatalf("tm status via .tmconfig: exit %d; %s", code, errOut)
			}
		})
	}
}

// TestPointer_SrcRoot: --src-root is stored absolute and a relative citation
// resolves against it from an unrelated working directory.
func TestPointer_SrcRoot(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "src.txt"), makeLines(20), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "g.mmd")

	// Pass the source root relative to the cwd to exercise Abs.
	t.Chdir(filepath.Dir(src))
	if _, errOut, code := run(t, "new", file, "--src-root", filepath.Base(src)); code != 0 {
		t.Fatalf("tm new: exit %d; %s", code, errOut)
	}
	data, err := os.ReadFile(config.UserPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "src-root = "+src+"\n") {
		t.Errorf("user config should hold the absolute src-root; got:\n%s", data)
	}

	t.Chdir(t.TempDir())
	if _, errOut, code := run(t, "add", "c1", "src.txt:1-1", "scope"); code != 0 {
		t.Fatalf("tm add with relative cite: exit %d; %s", code, errOut)
	}
}

// TestPointer_LoadFailureLeavesConfigUntouched: a failed load must not
// change the active pointer.
func TestPointer_LoadFailureLeavesConfigUntouched(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	t.Chdir(t.TempDir())
	if _, errOut, code := run(t, "new", "g.mmd"); code != 0 {
		t.Fatalf("tm new: exit %d; %s", code, errOut)
	}
	before, _ := os.ReadFile(config.UserPath())
	if _, _, code := run(t, "load", "missing.mmd"); code != 3 {
		t.Fatalf("tm load missing: want exit 3, got %d", code)
	}
	after, _ := os.ReadFile(config.UserPath())
	if string(before) != string(after) {
		t.Errorf("user config changed by failed load:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestHelp_DocFromConfig: the `doc` key defers baseline help like TM_DOC,
// and `--help --all` still prints the command list.
func TestHelp_DocFromConfig(t *testing.T) {
	freshConfig(t)
	tempErrlog(t)
	t.Chdir(t.TempDir())
	docPath := filepath.Join(t.TempDir(), "SKILL.md")
	content := fmt.Sprintf("---\nmetadata:\n  tm-version: %q\n---\n# skill\n", docver.Marker())
	if err := os.WriteFile(docPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.Set(config.UserPath(), map[string]string{"doc": docPath}); err != nil {
		t.Fatal(err)
	}

	out, _, code := run(t, "--help")
	if code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	want := fmt.Sprintf("see %s (tm %s)", docPath, version.Version())
	if strings.TrimSpace(out) != want {
		t.Errorf("--help with doc key: got %q, want %q", strings.TrimSpace(out), want)
	}

	out, _, code = run(t, "--help", "--all")
	if code != 0 {
		t.Fatalf("--help --all: exit %d", code)
	}
	if strings.Contains(out, "see ") || !strings.Contains(out, "tm new <file>") {
		t.Errorf("--help --all should list commands regardless of doc; got:\n%s", out)
	}
}

// TestErrlog_DispatcherErrorLandsBesideConfiguredGraph: an error raised
// before any handler resolves a graph still logs next to the active graph
// rather than in the working directory.
func TestErrlog_DispatcherErrorLandsBesideConfiguredGraph(t *testing.T) {
	freshConfig(t)
	t.Setenv("TM_ERRORS", "")
	lesson := t.TempDir()
	t.Chdir(lesson)
	if _, errOut, code := run(t, "new", "g.mmd"); code != 0 {
		t.Fatalf("tm new: exit %d; %s", code, errOut)
	}

	cwd := t.TempDir()
	t.Chdir(cwd)
	if _, _, code := run(t, "bogus"); code != 3 {
		t.Fatalf("tm bogus: want exit 3, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(lesson, "ERRORS.jsonl")); err != nil {
		t.Errorf("want ERRORS.jsonl beside the graph: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "ERRORS.jsonl")); !os.IsNotExist(err) {
		t.Errorf("ERRORS.jsonl must not land in the working directory")
	}
}
