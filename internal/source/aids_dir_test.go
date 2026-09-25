package source_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/source"
)

// ── AidsDir helper ────────────────────────────────────────────────────────────

func TestAidsDir_Default(t *testing.T) {
	dir := t.TempDir()
	got := source.AidsDir(nil, dir)
	want := filepath.Join(dir, "aids")
	if got != want {
		t.Errorf("AidsDir(nil, dir): want %q, got %q", want, got)
	}
}

func TestAidsDir_ConfiguredRelative(t *testing.T) {
	dir := t.TempDir()
	cfg := &source.Config{AidsDir: "my-aids"}
	got := source.AidsDir(cfg, dir)
	want := filepath.Clean(filepath.Join(dir, "my-aids"))
	if got != want {
		t.Errorf("AidsDir(relative): want %q, got %q", want, got)
	}
}

func TestAidsDir_ConfiguredAbsolute(t *testing.T) {
	absDir := "/tmp/absaids"
	cfg := &source.Config{AidsDir: absDir}
	got := source.AidsDir(cfg, t.TempDir())
	if got != filepath.Clean(absDir) {
		t.Errorf("AidsDir(absolute): want %q, got %q", filepath.Clean(absDir), got)
	}
}

// ── aids-dir config key ───────────────────────────────────────────────────────

func TestAidsDir_ConfigKey(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".tmconfig")
	if err := os.WriteFile(cfgPath, []byte("aids-dir = myaids\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := source.LoadConfigPaths("", cfgPath)
	if err != nil {
		t.Fatalf("LoadConfigPaths: %v", err)
	}
	if cfg.AidsDir != "myaids" {
		t.Errorf("Config.AidsDir: want 'myaids', got %q", cfg.AidsDir)
	}
}

// ── citation refusal when inside aids dir ─────────────────────────────────────

// writeAidFile creates a file at <graphDir>/aids/<name> and returns the
// relative locator string "aids/<name>".
func writeAidFile(t *testing.T, graphDir, name, content string) string {
	t.Helper()
	aidsDir := filepath.Join(graphDir, "aids")
	if err := os.MkdirAll(aidsDir, 0o755); err != nil {
		t.Fatalf("mkdir aids: %v", err)
	}
	if err := os.WriteFile(filepath.Join(aidsDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write aid file: %v", err)
	}
	return "aids/" + name
}

func TestAidsDir_RefusalInsideDefault(t *testing.T) {
	// Plain-path citation resolving inside default aids dir must be refused.
	dir := t.TempDir()
	locator := writeAidFile(t, dir, "ref.txt", "content\n")

	r := source.NewResolverWithConfig(&source.Config{}, dir)
	r.GraphDir = dir
	_, _, err := r.HashCitation(locator + ":1-1")
	if err == nil {
		t.Fatal("want RefusalError for aid citation, got nil")
	}
	var ref *source.RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("want RefusalError, got %T: %v", err, err)
	}
	if !strings.Contains(ref.Err, "is an aid, not a source") {
		t.Errorf("want 'is an aid, not a source' in Err; got %q", ref.Err)
	}
	// Fix carries only the short form; the CLI layer appends the full hint via aidRefusalWithID.
	if ref.Fix != "cite the primary source" {
		t.Errorf("Fix = %q, want \"cite the primary source\"", ref.Fix)
	}
	if ref.AidPath == "" {
		t.Error("AidPath must be non-empty for aids-dir refusal")
	}
}

func TestAidsDir_RefusalInsideConfigured(t *testing.T) {
	dir := t.TempDir()
	cfg := &source.Config{AidsDir: "my-aids"}
	// Create file inside configured aids dir.
	myAids := filepath.Join(dir, "my-aids")
	if err := os.MkdirAll(myAids, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(myAids, "note.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := source.NewResolverWithConfig(cfg, dir)
	r.GraphDir = dir
	_, _, err := r.HashCitation("my-aids/note.txt:1-1")
	if err == nil {
		t.Fatal("want RefusalError for aid inside configured dir, got nil")
	}
	var ref *source.RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("want RefusalError, got %T", err)
	}
}

func TestAidsDir_NoRefusalForSiblingPrefix(t *testing.T) {
	// aids2/ shares a prefix with aids/ but is not inside it → no refusal.
	dir := t.TempDir()
	aids2 := filepath.Join(dir, "aids2")
	if err := os.MkdirAll(aids2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aids2, "src.txt"), []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := source.NewResolverWithConfig(&source.Config{}, dir)
	r.GraphDir = dir
	// Should not be refused — aids2/ is not inside aids/.
	hashed, _, err := r.HashCitation("aids2/src.txt:1-1")
	if err != nil {
		var ref *source.RefusalError
		if errors.As(err, &ref) {
			t.Errorf("got unexpected RefusalError for sibling prefix: %v", ref.Err)
		}
	}
	_ = hashed
}

func TestAidsDir_NoRefusalForURI(t *testing.T) {
	dir := t.TempDir()
	r := source.NewResolverWithConfig(&source.Config{}, dir)
	r.GraphDir = dir
	// URI locators are never refused for aids-dir.
	_, _, err := r.HashCitation("https://example.com/doc.txt:1-1")
	// We don't check whether it succeeds (network not available in CI),
	// but it must NOT return an aids-dir RefusalError.
	if err != nil {
		var ref *source.RefusalError
		if errors.As(err, &ref) && strings.Contains(ref.Err, "is an aid") {
			t.Errorf("URI locator got aids-dir refusal: %v", ref.Err)
		}
	}
}
