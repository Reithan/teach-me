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
		// Legacy hashless form (backward compatibility).
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
		// New hashed form.
		{
			name:  "hashed relative path",
			input: "3f9a1c2b7e0d@raft.txt:202-215",
			want:  cite.Citation{Hash: "3f9a1c2b7e0d", File: "raft.txt", Start: 202, End: 215},
		},
		{
			name:  "hashed absolute path",
			input: "3f9a1c2b7e0d@/home/me/src/foo.go:10-24",
			want:  cite.Citation{Hash: "3f9a1c2b7e0d", File: "/home/me/src/foo.go", Start: 10, End: 24},
		},
		{
			name:  "hashed URI",
			input: "3f9a1c2b7e0d@https://example.com/doc:88-104",
			want:  cite.Citation{Hash: "3f9a1c2b7e0d", File: "https://example.com/doc", Start: 88, End: 104},
		},
		{
			name:  "hashed URI with @ in URL",
			input: "3f9a1c2b7e0d@https://user@example.com/doc:1-5",
			want:  cite.Citation{Hash: "3f9a1c2b7e0d", File: "https://user@example.com/doc", Start: 1, End: 5},
		},
		{
			name:  "hashed URI with port",
			input: "3f9a1c2b7e0d@https://example.com:8080/doc:1-5",
			want:  cite.Citation{Hash: "3f9a1c2b7e0d", File: "https://example.com:8080/doc", Start: 1, End: 5},
		},
		{
			name:  "hashed path with slash",
			input: "3f9a1c2b7e0d@src/raft.txt:1-2",
			want:  cite.Citation{Hash: "3f9a1c2b7e0d", File: "src/raft.txt", Start: 1, End: 2},
		},
		// Error cases.
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
		{
			name:    "locator with raw double-quote",
			input:   `my"file.txt:1-2`,
			wantErr: `citation "my\"file.txt:1-2": locator contains raw "; use %22`,
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

func TestFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		c    cite.Citation
		want string
	}{
		{
			name: "hashless",
			c:    cite.Citation{File: "raft.txt", Start: 120, End: 188},
			want: "raft.txt:120-188",
		},
		{
			name: "hashed",
			c:    cite.Citation{Hash: "3f9a1c2b7e0d", File: "raft.txt", Start: 202, End: 215},
			want: "3f9a1c2b7e0d@raft.txt:202-215",
		},
		{
			name: "hashed URI",
			c:    cite.Citation{Hash: "3f9a1c2b7e0d", File: "https://example.com/doc", Start: 1, End: 5},
			want: "3f9a1c2b7e0d@https://example.com/doc:1-5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cite.Format(tc.c)
			if got != tc.want {
				t.Fatalf("Format(%+v) = %q, want %q", tc.c, got, tc.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text unchanged",
			input: "line one\nline two\nline three",
			want:  "line one\nline two\nline three",
		},
		{
			name:  "CRLF converted to LF",
			input: "line one\r\nline two\r\nline three",
			want:  "line one\nline two\nline three",
		},
		{
			name:  "trailing whitespace stripped per line",
			input: "line one  \nline two\t\nline three ",
			want:  "line one\nline two\nline three",
		},
		{
			name:  "trailing newline removed",
			input: "line one\nline two\n",
			want:  "line one\nline two",
		},
		{
			name:  "internal whitespace preserved",
			input: "  indented line\n\tTabbed line\n  another",
			want:  "  indented line\n\tTabbed line\n  another",
		},
		{
			name:  "CRLF plus trailing spaces",
			input: "hello  \r\nworld\t\r\n",
			want:  "hello\nworld",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "single line no trailing newline",
			input: "hello",
			want:  "hello",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cite.Normalize(tc.input)
			if got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestHash(t *testing.T) {
	t.Parallel()

	// Hash must be exactly 12 lowercase hex characters.
	h := cite.Hash("hello world")
	if len(h) != 12 {
		t.Fatalf("Hash: got length %d, want 12", len(h))
	}
	for _, c := range h {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("Hash: got non-hex char %q in %q", c, h)
		}
	}

	// Hash must be deterministic.
	h2 := cite.Hash("hello world")
	if h != h2 {
		t.Fatalf("Hash: not deterministic: %q vs %q", h, h2)
	}

	// Different inputs must produce different hashes (with overwhelming probability).
	h3 := cite.Hash("goodbye world")
	if h == h3 {
		t.Fatalf("Hash: different inputs produced same hash %q", h)
	}

	// Known value: SHA-256("") first 12 hex chars.
	// sha256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	emptyHash := cite.Hash("")
	if emptyHash != "e3b0c44298fc" {
		t.Fatalf("Hash(\"\") = %q, want %q", emptyHash, "e3b0c44298fc")
	}
}

func TestIsURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"https://example.com/doc", true},
		{"http://example.com/doc", true},
		{"ftp://example.com/file", true},
		{"file:///home/user/doc", true},
		{"git+https://example.com/repo", true},
		{"raft.txt", false},
		{"/absolute/path", false},
		{"relative/path", false},
		{"C:/windows/path", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := cite.IsURI(tc.input)
			if got != tc.want {
				t.Fatalf("IsURI(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsAbsPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"/absolute/path", true},
		{"/", true},
		{"C:/windows/path", true},
		{"C:\\windows\\path", true},
		{"relative/path", false},
		{"raft.txt", false},
		{"https://example.com", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := cite.IsAbsPath(tc.input)
			if got != tc.want {
				t.Fatalf("IsAbsPath(%q) = %v, want %v", tc.input, got, tc.want)
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

	t.Run("absolute path returned as-is", func(t *testing.T) {
		c := cite.Citation{File: "/home/me/doc.txt", Start: 1, End: 5}
		got := cite.Resolve(c, "/root")
		want := "/home/me/doc.txt"
		if got != want {
			t.Fatalf("Resolve: got %q, want %q", got, want)
		}
	})

	t.Run("URI returned as-is", func(t *testing.T) {
		c := cite.Citation{File: "https://example.com/doc", Start: 1, End: 5}
		got := cite.Resolve(c, "/root")
		want := "https://example.com/doc"
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

	t.Run("URI returns error", func(t *testing.T) {
		c := cite.Citation{File: "https://example.com/doc", Start: 1, End: 5}
		_, err := cite.ReadRange(c, "/any/root")
		if err == nil {
			t.Fatal("expected error for URI citation, got nil")
		}
	})
}

func TestHashCitation(t *testing.T) {
	t.Parallel()

	writeFile := func(t *testing.T, dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	t.Run("hashless form gets hashed", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "src.txt", "line 1\nline 2\nline 3\n")
		got, err := cite.HashCitation("src.txt:1-3", dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Verify the result has the right format and correct hash.
		c, parseErr := cite.Parse(got)
		if parseErr != nil {
			t.Fatalf("Parse result %q: %v", got, parseErr)
		}
		if c.Hash == "" {
			t.Fatalf("HashCitation: result has no hash: %q", got)
		}
		if c.File != "src.txt" || c.Start != 1 || c.End != 3 {
			t.Fatalf("HashCitation: unexpected citation: %+v", c)
		}
		// Verify hash is correct.
		text, _ := cite.ReadRange(cite.Citation{File: "src.txt", Start: 1, End: 3}, dir)
		want := cite.Hash(cite.Normalize(text))
		if c.Hash != want {
			t.Fatalf("HashCitation: hash %q, want %q", c.Hash, want)
		}
	})

	t.Run("matching hash accepted", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "src.txt", "line 1\nline 2\nline 3\n")
		text := "line 1\nline 2\nline 3"
		h := cite.Hash(cite.Normalize(text))
		citeStr := h + "@src.txt:1-3"
		got, err := cite.HashCitation(citeStr, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != citeStr {
			t.Fatalf("HashCitation: got %q, want %q", got, citeStr)
		}
	})

	t.Run("hash mismatch returns error", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "src.txt", "line 1\nline 2\nline 3\n")
		citeStr := "000000000000@src.txt:1-3"
		_, err := cite.HashCitation(citeStr, dir)
		if err == nil {
			t.Fatal("expected error for hash mismatch, got nil")
		}
	})

	t.Run("URI with hash accepted as-is", func(t *testing.T) {
		citeStr := "3f9a1c2b7e0d@https://example.com/doc:1-5"
		got, err := cite.HashCitation(citeStr, "/any")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != citeStr {
			t.Fatalf("HashCitation: got %q, want %q", got, citeStr)
		}
	})

	t.Run("URI without hash refused", func(t *testing.T) {
		_, err := cite.HashCitation("https://example.com/doc:1-5", "/any")
		if err == nil {
			t.Fatal("expected error for URI without hash, got nil")
		}
	})
}

func TestCheckDrift(t *testing.T) {
	t.Parallel()

	writeFile := func(t *testing.T, dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	t.Run("no drift", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "src.txt", "hello\nworld\n")
		text := "hello\nworld"
		h := cite.Hash(cite.Normalize(text))
		citeStr := h + "@src.txt:1-2"
		drifted, err := cite.CheckDrift(citeStr, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if drifted {
			t.Fatal("expected no drift, got drifted=true")
		}
	})

	t.Run("drift detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "src.txt", "changed\nworld\n")
		citeStr := "000000000000@src.txt:1-2"
		drifted, err := cite.CheckDrift(citeStr, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !drifted {
			t.Fatal("expected drift, got drifted=false")
		}
	})

	t.Run("hashless citation never drifts", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "src.txt", "hello\n")
		drifted, err := cite.CheckDrift("src.txt:1-1", dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if drifted {
			t.Fatal("hashless citation should never report drift")
		}
	})

	t.Run("URI never drifts in M9", func(t *testing.T) {
		drifted, err := cite.CheckDrift("3f9a1c2b7e0d@https://example.com/doc:1-5", "/any")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if drifted {
			t.Fatal("URI citation should not drift in M9")
		}
	})
}
