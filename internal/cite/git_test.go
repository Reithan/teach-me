package cite_test

import (
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/cite"
)

// TestIsGit covers the IsGit helper.
func TestIsGit(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"git:r@main:f.txt", true},
		{"git:", true},
		{"git", false},
		{"https://example.com", false},
		{"path/to/file.txt", false},
		{"/abs/path.txt", false},
	} {
		if got := cite.IsGit(tc.in); got != tc.want {
			t.Errorf("IsGit(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestValidAlias covers the ValidAlias helper.
func TestValidAlias(t *testing.T) {
	valid := []string{"r", "my-repo", "my_repo", "MyRepo123", "A", "a0_B-C"}
	for _, s := range valid {
		if !cite.ValidAlias(s) {
			t.Errorf("ValidAlias(%q) = false, want true", s)
		}
	}
	invalid := []string{"", " ", "has space", "has:colon", "has@at", "has/slash", "has.dot"}
	for _, s := range invalid {
		if cite.ValidAlias(s) {
			t.Errorf("ValidAlias(%q) = true, want false", s)
		}
	}
}

// TestParseGit covers ParseGit for all valid forms and all rejection cases.
func TestParseGit(t *testing.T) {
	tests := []struct {
		name      string
		locator   string
		wantAlias string
		wantRef   string
		wantRefB  string
		wantPath  string
		wantErr   string // non-empty: expect an error containing this substring
	}{
		// ── Valid forms ──────────────────────────────────────────────────────
		{
			name:      "file at ref",
			locator:   "git:r@main:path/to/file.txt",
			wantAlias: "r",
			wantRef:   "main",
			wantPath:  "path/to/file.txt",
		},
		{
			name:      "file at SHA ref",
			locator:   "git:myrepo@abc123def456:src/main.go",
			wantAlias: "myrepo",
			wantRef:   "abc123def456",
			wantPath:  "src/main.go",
		},
		{
			name:      "commit only",
			locator:   "git:r@abc123",
			wantAlias: "r",
			wantRef:   "abc123",
		},
		{
			name:      "diff",
			locator:   "git:r@abc123..def456",
			wantAlias: "r",
			wantRef:   "abc123",
			wantRefB:  "def456",
		},
		{
			name:      "diff for path",
			locator:   "git:r@abc123..def456:src/main.go",
			wantAlias: "r",
			wantRef:   "abc123",
			wantRefB:  "def456",
			wantPath:  "src/main.go",
		},
		{
			name:      "path with %3A (percent-encoded colon)",
			locator:   "git:r@main:path%3Awith%3Acolons.txt",
			wantAlias: "r",
			wantRef:   "main",
			wantPath:  "path:with:colons.txt",
		},
		{
			name:      "alias with hyphens and underscores",
			locator:   "git:my-repo_v2@HEAD:file.txt",
			wantAlias: "my-repo_v2",
			wantRef:   "HEAD",
			wantPath:  "file.txt",
		},
		// ── Rejection cases ──────────────────────────────────────────────────
		{
			name:    "missing git: prefix",
			locator: "https://example.com",
			wantErr: "missing git: prefix",
		},
		{
			name:    "no @ separator",
			locator: "git:without-at",
			wantErr: "missing @",
		},
		{
			name:    "empty alias",
			locator: "git:@main:file.txt",
			wantErr: "empty alias",
		},
		{
			name:    "invalid alias with space",
			locator: "git:has space@main:file.txt",
			wantErr: "must match [A-Za-z0-9_-]+",
		},
		{
			name:    "invalid alias with slash",
			locator: "git:has/slash@main:file.txt",
			wantErr: "must match [A-Za-z0-9_-]+",
		},
		{
			name:    "invalid alias with dot",
			locator: "git:has.dot@main:file.txt",
			wantErr: "must match [A-Za-z0-9_-]+",
		},
		{
			name:    "empty ref",
			locator: "git:r@:file.txt",
			wantErr: "empty ref",
		},
		{
			name:    "empty left side of ..",
			locator: "git:r@..def456",
			wantErr: "empty left side",
		},
		{
			name:    "empty right side of ..",
			locator: "git:r@abc123..",
			wantErr: "empty right side",
		},
		{
			name:    "raw colon in path (lint-14)",
			locator: "git:r@main:path:with:colon.txt",
			wantErr: "raw : in git path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gl, err := cite.ParseGit(tc.locator)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseGit(%q): want error containing %q, got nil", tc.locator, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("ParseGit(%q) error = %q, want to contain %q", tc.locator, err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseGit(%q): unexpected error: %v", tc.locator, err)
			}
			if gl.Alias != tc.wantAlias {
				t.Errorf("Alias = %q, want %q", gl.Alias, tc.wantAlias)
			}
			if gl.Ref != tc.wantRef {
				t.Errorf("Ref = %q, want %q", gl.Ref, tc.wantRef)
			}
			if gl.RefB != tc.wantRefB {
				t.Errorf("RefB = %q, want %q", gl.RefB, tc.wantRefB)
			}
			if gl.Path != tc.wantPath {
				t.Errorf("Path = %q, want %q", gl.Path, tc.wantPath)
			}
		})
	}
}

// TestFormatGit covers FormatGit for all four forms.
func TestFormatGit(t *testing.T) {
	tests := []struct {
		name string
		gl   cite.GitLocator
		want string
	}{
		{
			name: "file at ref",
			gl:   cite.GitLocator{Alias: "r", Ref: "main", Path: "src/file.txt"},
			want: "git:r@main:src/file.txt",
		},
		{
			name: "commit",
			gl:   cite.GitLocator{Alias: "r", Ref: "abc123"},
			want: "git:r@abc123",
		},
		{
			name: "diff",
			gl:   cite.GitLocator{Alias: "r", Ref: "abc123", RefB: "def456"},
			want: "git:r@abc123..def456",
		},
		{
			name: "diff for path",
			gl:   cite.GitLocator{Alias: "r", Ref: "abc123", RefB: "def456", Path: "src/file.txt"},
			want: "git:r@abc123..def456:src/file.txt",
		},
		{
			name: "path with colon is percent-encoded",
			gl:   cite.GitLocator{Alias: "r", Ref: "main", Path: "path:with:colons.txt"},
			want: "git:r@main:path%3Awith%3Acolons.txt",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cite.FormatGit(tc.gl)
			if got != tc.want {
				t.Errorf("FormatGit(%+v) = %q, want %q", tc.gl, got, tc.want)
			}
		})
	}
}

// TestParseGit_RoundTrip verifies that Parse + FormatGit + Parse round-trips
// for all four forms including a range suffix (the outer Parse splits on last colon).
func TestParseGit_RoundTrip(t *testing.T) {
	forms := []string{
		"git:r@main:src/file.txt",
		"git:r@abc123def456",
		"git:r@abc123..def456",
		"git:r@abc123..def456:src/file.txt",
		"git:r@main:path%3Awith%3Acolon.txt",
	}

	for _, locator := range forms {
		t.Run(locator, func(t *testing.T) {
			// Parse as a full citation (append a range so cite.Parse works).
			full := "000000000000@" + locator + ":1-1"
			c, err := cite.Parse(full)
			if err != nil {
				t.Fatalf("cite.Parse(%q): %v", full, err)
			}
			gl, err := cite.ParseGit(c.File)
			if err != nil {
				t.Fatalf("ParseGit(%q): %v", c.File, err)
			}
			reformatted := cite.FormatGit(gl)
			if reformatted != locator {
				t.Errorf("round-trip: got %q, want %q", reformatted, locator)
			}
		})
	}
}
