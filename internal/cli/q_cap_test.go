package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// qWriteBigSrc writes src.txt with n numbered lines ("line I") into dir, padding
// each with 'x' up to width so a caller can force the §7 character cap to bite
// before the line cap. Lines 1-5 keep their bare "line I" form (width 0 leaves
// them untouched) so the fixture concept's src.txt:1-5 hash still matches.
func qWriteBigSrc(t *testing.T, dir string, n, width int) {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		line := fmt.Sprintf("line %d", i)
		if pad := width - len(line); pad > 0 {
			line += strings.Repeat("x", pad)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestQ_CitationCap exercises the §7 question citation cap: tm q refuses a
// citation whose hashed text exceeds 120 lines or 6,000 characters, for every q
// form, before any lock or event log; a citation at the cap is written.
func TestQ_CitationCap(t *testing.T) {
	tests := []struct {
		name      string
		srcLines  int
		srcWidth  int
		citeRange string
		flags     []string
		wantExit  int
		wantErr   string // substring expected on stderr (refusals)
		wantQID   string // expected stdout (pass)
	}{
		{
			name:      "over by lines",
			srcLines:  130,
			citeRange: "1-121",
			wantExit:  1,
			wantErr:   "citation spans 121 lines",
		},
		{
			name:      "over by chars",
			srcLines:  110,
			srcWidth:  60, // 100 lines of 60 + 99 newlines = 6099 chars
			citeRange: "1-100",
			wantExit:  1,
			wantErr:   "chars; a question cites at most 120 lines or 6000 chars",
		},
		{
			name:      "exactly at cap is written",
			srcLines:  130,
			citeRange: "1-120",
			wantExit:  0,
			wantQID:   "q1",
		},
		{
			name:      "re form refuses",
			srcLines:  130,
			citeRange: "1-121",
			flags:     []string{"--re", "q1"},
			wantExit:  1,
			wantErr:   "citation spans 121 lines",
		},
		{
			name:      "teach form refuses",
			srcLines:  130,
			citeRange: "1-121",
			flags:     []string{"--teach", "--re", "q1"},
			wantExit:  1,
			wantErr:   "citation spans 121 lines",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dir, errPath := qSetupDir(t)
			qWriteBigSrc(t, dir, tc.srcLines, tc.srcWidth)
			qWriteGraph(t, dir, qSimpleGraph())

			args := append([]string{"q", "mycon", "src.txt:" + tc.citeRange, "scope"}, tc.flags...)
			out, errOut, code := run(t, args...)
			if code != tc.wantExit {
				t.Fatalf("exit %d, want %d; stderr:\n%s", code, tc.wantExit, errOut)
			}

			if tc.wantExit == 0 {
				if got := strings.TrimSpace(out); got != tc.wantQID {
					t.Errorf("stdout = %q, want %q", got, tc.wantQID)
				}
				return
			}

			if !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", errOut, tc.wantErr)
			}
			if !strings.Contains(errOut, "fix: narrow with tm src src.txt --find <regex>") {
				t.Errorf("stderr = %q, want the narrow fix line", errOut)
			}
			rows := readErrlog(t, errPath)
			if len(rows) != 1 || rows[0].Exit != 1 {
				t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
			}
		})
	}
}
