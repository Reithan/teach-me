package cite_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reithan/teach-me/internal/cite"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    cite.Citation
		wantErr string
	}{
		{
			name:  "simple path",
			input: "raft.txt:120-188",
			want:  cite.Citation{File: "raft.txt", Start: 120, End: 188},
		},
		{
			name:  "path with slash",
			input: "src/raft.txt:1-2",
			want:  cite.Citation{File: "src/raft.txt", Start: 1, End: 2},
		},
		{
			name:  "single line range",
			input: "foo.txt:5-5",
			want:  cite.Citation{File: "foo.txt", Start: 5, End: 5},
		},
		{
			name:    "no colon",
			input:   "raft.txt",
			wantErr: `citation "raft.txt": missing line range`,
		},
		{
			name:    "empty file part",
			input:   ":1-2",
			wantErr: `citation ":1-2": empty file`,
		},
		{
			name:    "no dash in range",
			input:   "raft.txt:5",
			wantErr: `citation "raft.txt:5": line range must be START-END`,
		},
		{
			name:    "empty start",
			input:   "raft.txt:-5",
			wantErr: `citation "raft.txt:-5": line range must be START-END`,
		},
		{
			name:    "empty end",
			input:   "raft.txt:5-",
			wantErr: `citation "raft.txt:5-": line range must be START-END`,
		},
		{
			name:    "non-digit start",
			input:   "raft.txt:a-5",
			wantErr: `citation "raft.txt:a-5": line range must be START-END`,
		},
		{
			name:    "non-digit end",
			input:   "raft.txt:1-z",
			wantErr: `citation "raft.txt:1-z": line range must be START-END`,
		},
		{
			name:    "zero start",
			input:   "raft.txt:0-5",
			wantErr: `citation "raft.txt:0-5": line numbers must be positive`,
		},
		{
			name:    "end before start",
			input:   "raft.txt:10-5",
			wantErr: `citation "raft.txt:10-5": end line 5 precedes start line 10`,
		},
		{
			name:    "file with newline",
			input:   "foo\nbar.txt:1-2",
			wantErr: `citation "foo\nbar.txt:1-2": file contains newline`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := cite.Parse(tc.input)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Parse(%q): expected error %q, got nil", tc.input, tc.wantErr)
				}
				if err.Error() != tc.wantErr {
					t.Fatalf("Parse(%q): error = %q, want %q", tc.input, err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("Parse(%q): got %+v, want %+v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSrcRoot(t *testing.T) {
	t.Run("env set", func(t *testing.T) {
		t.Setenv("TM_SRC_ROOT", "/custom/root")
		if got := cite.SrcRoot("/graph/dir"); got != "/custom/root" {
			t.Fatalf("SrcRoot: got %q, want %q", got, "/custom/root")
		}
	})

	t.Run("env unset returns graphDir", func(t *testing.T) {
		t.Setenv("TM_SRC_ROOT", "")
		if got := cite.SrcRoot("/graph/dir"); got != "/graph/dir" {
			t.Fatalf("SrcRoot: got %q, want %q", got, "/graph/dir")
		}
	})
}

func TestResolve(t *testing.T) {
	t.Parallel()

	t.Run("relative file under root", func(t *testing.T) {
		c := cite.Citation{File: "raft.txt", Start: 1, End: 5}
		got := cite.Resolve(c, "/root")
		want := filepath.Join("/root", "raft.txt")
		if got != want {
			t.Fatalf("Resolve: got %q, want %q", got, want)
		}
	})

	t.Run("slash path", func(t *testing.T) {
		c := cite.Citation{File: "src/raft.txt", Start: 1, End: 5}
		got := cite.Resolve(c, "/root")
		want := filepath.Join("/root", "src", "raft.txt")
		if got != want {
			t.Fatalf("Resolve: got %q, want %q", got, want)
		}
	})
}

func TestReadRange(t *testing.T) {
	t.Parallel()

	writeFile := func(t *testing.T, dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	t.Run("multi-line range", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "file.txt", "line1\nline2\nline3\nline4\n")
		c := cite.Citation{File: "file.txt", Start: 2, End: 3}
		got, err := cite.ReadRange(c, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "line2\nline3" {
			t.Fatalf("got %q, want %q", got, "line2\nline3")
		}
	})

	t.Run("single line range", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "file.txt", "line1\nline2\nline3\n")
		c := cite.Citation{File: "file.txt", Start: 2, End: 2}
		got, err := cite.ReadRange(c, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "line2" {
			t.Fatalf("got %q, want %q", got, "line2")
		}
	})

	t.Run("range reaching last line with trailing newline", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "file.txt", "a\nb\n")
		c := cite.Citation{File: "file.txt", Start: 1, End: 2}
		got, err := cite.ReadRange(c, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "a\nb" {
			t.Fatalf("got %q, want %q", got, "a\nb")
		}
	})

	t.Run("range reaching last line without trailing newline", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "file.txt", "a\nb")
		c := cite.Citation{File: "file.txt", Start: 1, End: 2}
		got, err := cite.ReadRange(c, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "a\nb" {
			t.Fatalf("got %q, want %q", got, "a\nb")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		dir := t.TempDir()
		c := cite.Citation{File: "nofile.txt", Start: 1, End: 1}
		_, err := cite.ReadRange(c, dir)
		if err == nil {
			t.Fatal("expected error for missing file, got nil")
		}
	})

	t.Run("end out of bounds", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "file.txt", "a\nb\n")
		c := cite.Citation{File: "file.txt", Start: 1, End: 5}
		_, err := cite.ReadRange(c, dir)
		if err == nil {
			t.Fatal("expected out-of-bounds error, got nil")
		}
		want := `citation "file.txt": lines 1-5 out of bounds (file has 2 lines)`
		if err.Error() != want {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
	})

	t.Run("start less than 1", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "file.txt", "a\nb\n")
		c := cite.Citation{File: "file.txt", Start: 0, End: 1}
		_, err := cite.ReadRange(c, dir)
		if err == nil {
			t.Fatal("expected error for start < 1, got nil")
		}
	})
}
