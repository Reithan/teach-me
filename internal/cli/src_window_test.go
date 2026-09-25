package cli_test

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// srcGen builds n numbered lines from tmpl (a fmt template taking the 1-based
// line number). When width exceeds the formatted line length the line is padded
// with 'x' so a caller can force the character bound to bite before the line
// bound.
func srcGen(tmpl string, n, width int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		line := fmt.Sprintf(tmpl, i)
		if pad := width - len(line); pad > 0 {
			line += strings.Repeat("x", pad)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestTmSrc_Window exercises the §1 output window on a plain-path source: the
// window applies to the whole file, an explicit range, and --find hits alike,
// with a more: trailer when text is cut and none otherwise; --fulldump prints
// everything with no trailer.
func TestTmSrc_Window(t *testing.T) {
	tests := []struct {
		name         string
		tmpl         string // fmt template; defaults to "L%d"
		n            int
		width        int
		extra        []string // args after the file locator
		wantNumbered int
		trailerTail  string // "" means no trailer expected
	}{
		{
			name:         "under window",
			n:            5,
			wantNumbered: 5,
		},
		{
			name:         "exactly at window",
			n:            200,
			wantNumbered: 200,
		},
		{
			name:         "over by lines",
			n:            201,
			wantNumbered: 200,
			trailerTail:  "201-201",
		},
		{
			name:         "over by chars with whole-line cut",
			n:            15,
			width:        799, // 799 + newline = 800 bytes; 10 lines reach 8000
			wantNumbered: 10,
			trailerTail:  "11-15",
		},
		{
			name:         "explicit range over window",
			n:            300,
			extra:        []string{"50-260"},
			wantNumbered: 200,
			trailerTail:  "250-260",
		},
		{
			name:         "find over window bare regex",
			n:            250,
			extra:        []string{"--find", "L"},
			wantNumbered: 200,
			trailerTail:  "201-250 --find L",
		},
		{
			name:         "find over window spaced regex is quoted",
			tmpl:         "aa bb %d",
			n:            250,
			extra:        []string{"--find", "aa bb"},
			wantNumbered: 200,
			trailerTail:  `201-250 --find "aa bb"`,
		},
		{
			name:         "fulldump over window has no trailer",
			n:            250,
			extra:        []string{"--fulldump"},
			wantNumbered: 250,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			dir := t.TempDir()
			t.Chdir(dir)
			srcMakeGraph(t, dir)
			srcSetupXDG(t, "")

			tmpl := tc.tmpl
			if tmpl == "" {
				tmpl = "L%d"
			}
			f := srcWriteFile(t, dir, srcGen(tmpl, tc.n, tc.width))

			args := append([]string{"src", f}, tc.extra...)
			out, errOut, code := run(t, args...)
			if code != 0 {
				t.Fatalf("tm src exit %d; stderr: %s", code, errOut)
			}

			numbered, trailer := 0, ""
			for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
				switch {
				case ln == "":
				case strings.HasPrefix(ln, "more: "):
					trailer = ln
				default:
					numbered++
				}
			}
			if numbered != tc.wantNumbered {
				t.Errorf("numbered lines = %d, want %d", numbered, tc.wantNumbered)
			}
			if tc.trailerTail == "" {
				if trailer != "" {
					t.Errorf("unexpected trailer %q", trailer)
				}
				return
			}
			want := fmt.Sprintf("more: tm src %s %s", f, tc.trailerTail)
			if trailer != want {
				t.Errorf("trailer = %q, want %q", trailer, want)
			}
		})
	}
}

// TestTmSrc_WindowGit verifies a git: source over the window keeps its src:
// header line and adds a more: trailer that names the original git: locator.
func TestTmSrc_WindowGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tempErrlog(t)
	dir := t.TempDir()
	t.Chdir(dir)

	sha := srcInitGitRepo(t, dir, srcGen("L%d", 250, 0))
	short12 := sha[:12]

	gitBin, _ := exec.LookPath("git")
	srcSetupXDG(t, fmt.Sprintf("git=%s\nrepo r = %s\n", gitBin, dir))

	branchOut, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	branch := strings.TrimSpace(string(branchOut))
	locator := fmt.Sprintf("git:r@%s:file.txt", branch)

	out, errOut, code := run(t, "src", locator)
	if code != 0 {
		t.Fatalf("tm src exit %d; stderr: %s", code, errOut)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	wantHeader := fmt.Sprintf("src: git:r@%s:file.txt", short12)
	if lines[0] != wantHeader {
		t.Errorf("header = %q, want %q", lines[0], wantHeader)
	}
	wantTrailer := fmt.Sprintf("more: tm src %s 201-250", locator)
	if last := lines[len(lines)-1]; last != wantTrailer {
		t.Errorf("trailer = %q, want %q", last, wantTrailer)
	}

	numbered := 0
	for _, ln := range lines {
		if ln == "" || strings.HasPrefix(ln, "src: ") || strings.HasPrefix(ln, "more: ") {
			continue
		}
		numbered++
	}
	if numbered != 200 {
		t.Errorf("numbered lines = %d, want 200", numbered)
	}
}
