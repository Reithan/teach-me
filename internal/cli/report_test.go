package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReport(t *testing.T) {
	type tc struct {
		name      string
		extraArgs []string
		role      string
		mutate    func(srcDir string) // called after raft fixture setup
		wantCode  int
		contains  []string
		absent    []string
	}

	cases := []tc{
		{
			name:      "outline: topo order, states, GAP, fail summary, footnotes",
			extraArgs: []string{"commit_rules"},
			contains: []string{
				"## replicated_log:", "## leader_election:", "## log_matching:", "## commit_rules:",
				"state: passed", "state: open", "state: blocked",
				"GAP: treats index match as sufficient, ignores term",
				"- Says matching index is enough; never mentions term",
				"[^",
			},
		},
		{
			name:      "--hops 1: excludes 2-hop ancestor",
			extraArgs: []string{"commit_rules", "--hops", "1"},
			absent:    []string{"## replicated_log:"},
			contains:  []string{"## leader_election:", "## log_matching:", "## commit_rules:"},
		},
		{
			name:      "--passed-only: drops open/blocked concepts",
			extraArgs: []string{"commit_rules", "--passed-only"},
			contains:  []string{"## replicated_log:", "## leader_election:"},
			absent:    []string{"## log_matching:", "## commit_rules:"},
		},
		{
			name:      "--fulltext: drift line appears after source corruption",
			extraArgs: []string{"log_matching", "--fulltext", "--hops", "1"},
			mutate: func(srcDir string) {
				// Replace raft.txt with different content; stored hashes no longer match.
				newContent := strings.Repeat("X\n", 300)
				_ = os.WriteFile(filepath.Join(srcDir, "raft.txt"), []byte(newContent), 0o644)
			},
			contains: []string{"DRIFT", "```"},
		},
		{
			name:      "unknown concept: exit 3",
			extraArgs: []string{"nonexistent_concept"},
			wantCode:  3,
		},
		{
			name:      "bad --hops value: exit 3",
			extraArgs: []string{"commit_rules", "--hops", "bad"},
			wantCode:  3,
		},
		{
			name:      "grader role: forbidden, exit 1",
			extraArgs: []string{"commit_rules"},
			role:      "grader",
			wantCode:  1,
		},
		// No-concept (whole-graph) rows
		{
			name:      "no-concept outline: every concept with its state",
			extraArgs: []string{},
			contains: []string{
				"## leader_election:", "## replicated_log:",
				"## log_matching:", "## commit_rules:",
				"state: passed", "state: open", "state: blocked",
			},
		},
		{
			name:      "no-concept --hops 1: excludes depth-2 concept",
			extraArgs: []string{"--hops", "1"},
			contains:  []string{"## leader_election:", "## replicated_log:", "## log_matching:"},
			absent:    []string{"## commit_rules:"},
		},
		{
			name:      "no-concept --passed-only: only passed concepts",
			extraArgs: []string{"--passed-only"},
			contains:  []string{"## leader_election:", "## replicated_log:"},
			absent:    []string{"## log_matching:", "## commit_rules:"},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			tempErrlog(t)
			t.Setenv("TM_FILE", "")
			if c.role != "" {
				t.Setenv("TM_ROLE", c.role)
			}
			srcDir := makeRaftSrcRoot(t)
			raftPath := raftFixture(t)
			if c.mutate != nil {
				c.mutate(srcDir)
			}
			args := append([]string{"report", "--file", raftPath}, c.extraArgs...)
			out, _, code := run(t, args...)
			if code != c.wantCode {
				t.Fatalf("want exit %d, got %d; output:\n%s", c.wantCode, code, out)
			}
			for _, s := range c.contains {
				if !strings.Contains(out, s) {
					t.Errorf("missing %q;\ngot:\n%s", s, out)
				}
			}
			for _, s := range c.absent {
				if strings.Contains(out, s) {
					t.Errorf("unexpected %q;\ngot:\n%s", s, out)
				}
			}
		})
	}
}
