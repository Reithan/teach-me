package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	icite "github.com/reithan/teach-me/internal/cite"
)

// buildRule1Graph creates a format-1 graph string with one concept (id=c1) and
// citation hash@file.txt:1-2.
func buildRule1Graph(hash string) string {
	cite := hash + "@file.txt:1-2"
	return fmt.Sprintf(`flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        c1["Concept one<br/>%s"]
    end
    subgraph reserve["Concepts held in reserve"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`, cite)
}

// writeEventLog writes a minimal event log with one "add" event that includes
// the commit field (simulating pre-PR2 behaviour where tm add stored HEAD SHA).
func writeEventLog(t *testing.T, logPath, id, citeStr, commit string) {
	t.Helper()
	ev := map[string]any{
		"ev":     "add",
		"id":     id,
		"cite":   citeStr,
		"commit": commit,
		"t":      "2026-01-01T00:00:00Z",
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if err := os.WriteFile(logPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write event log: %v", err)
	}
}

// TestMigrateRule1_Rewrite tests the full lifecycle: format-1 graph with a
// plain-path citation that has a stored commit → migrate rewrites to git: form.
func TestMigrateRule1_Rewrite(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	freshConfig(t)
	tempErrlog(t)

	gitBin, _ := exec.LookPath("git")
	repoDir := t.TempDir()
	fullSHA := repoInitGit(t, repoDir)
	sha12 := fullSHA[:12]

	// Hash the content of lines 1-2 of the file.
	contentHash := icite.Hash("line1\nline2")

	// Set up XDG config with git and repo alias.
	srcSetupXDG(t, fmt.Sprintf("git=%s\nrepo r = %s\n", gitBin, repoDir))

	// Set TM_SRC_ROOT so the relative citation resolves.
	t.Setenv("TM_SRC_ROOT", repoDir)

	// Create the graph and event log in a temp working directory.
	graphDir := t.TempDir()
	t.Chdir(graphDir)
	graphFile := filepath.Join(graphDir, "g.mmd")
	relFile := "file.txt"
	citeStr := contentHash + "@" + relFile + ":1-2"

	if err := os.WriteFile(graphFile, []byte(buildRule1Graph(contentHash)), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEventLog(t, graphFile+".jsonl", "c1", citeStr, fullSHA)

	// --dry-run: output shows the rewrite but file is unchanged.
	dryOut, dryErr, dryCode := run(t, "migrate", graphFile, "--dry-run")
	if dryCode != 0 {
		t.Fatalf("tm migrate --dry-run: exit %d; stderr: %s", dryCode, dryErr)
	}
	if !strings.Contains(dryOut, "ok c1") {
		t.Errorf("dry-run output should contain 'ok c1'; got:\n%s", dryOut)
	}
	if !strings.Contains(dryOut, "git:r@") {
		t.Errorf("dry-run output should contain 'git:r@'; got:\n%s", dryOut)
	}
	// Graph file must be unchanged after --dry-run.
	data, _ := os.ReadFile(graphFile)
	if strings.Contains(string(data), "tm:format") {
		t.Errorf("--dry-run must not write the file; found tm:format in graph")
	}

	// Actual migrate.
	migrOut, migrErr, migrCode := run(t, "migrate", graphFile)
	if migrCode != 0 {
		t.Fatalf("tm migrate: exit %d; stderr: %s", migrCode, migrErr)
	}
	if !strings.Contains(migrOut, "ok c1") {
		t.Errorf("migrate output should contain 'ok c1'; got:\n%s", migrOut)
	}

	// Graph must now be format 2 with a git: citation.
	graphData, err := os.ReadFile(graphFile)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	graphStr := string(graphData)
	if !strings.Contains(graphStr, "%% tm:format 2") {
		t.Errorf("graph does not contain %%%% tm:format 2:\n%s", graphStr)
	}
	wantLocator := "git:r@" + sha12 + ":file.txt"
	if !strings.Contains(graphStr, wantLocator) {
		t.Errorf("graph does not contain SHA-pinned locator %q:\n%s", wantLocator, graphStr)
	}

	// After migration the graph should lint clean.
	lintOut, lintErr, lintCode := run(t, "lint", graphFile)
	if lintCode != 0 {
		t.Errorf("lint after migrate: exit %d; stdout: %s; stderr: %s", lintCode, lintOut, lintErr)
	}
}

// TestMigrateRule1_Table covers three edge-case paths through migrateRule1
// using the same git repo fixture as TestMigrateRule1_Rewrite.
func TestMigrateRule1_Table(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	gitBin, _ := exec.LookPath("git")
	repoDir := t.TempDir()
	fullSHA := repoInitGit(t, repoDir)
	contentHash := icite.Hash("line1\nline2")
	wrongHash := icite.Hash("content that does not match line1 or line2")

	tests := []struct {
		name       string
		citeHash   string // hash embedded in the citation
		makeLog    func(t *testing.T, logPath, id, citeStr string)
		wantLeft   bool
		wantReason string // substring expected in the left reason line
	}{
		{
			// The citation hash does not match the file content at the logged
			// commit; the rule must stop the pipeline with a mismatch reason.
			name:     "hash differs: left with mismatch reason",
			citeHash: wrongHash,
			makeLog: func(t *testing.T, logPath, id, citeStr string) {
				writeEventLog(t, logPath, id, citeStr, fullSHA)
			},
			wantLeft:   true,
			wantReason: "hash mismatch",
		},
		{
			// The add event exists but carries no "commit" field; the rule
			// must return ok=false with an empty reason so the pipeline falls
			// through to the default "plain path".
			name:     "event without commit: falls through to plain path",
			citeHash: contentHash,
			makeLog: func(t *testing.T, logPath, id, citeStr string) {
				t.Helper()
				ev := map[string]any{
					"ev":   "add",
					"id":   id,
					"cite": citeStr,
					"t":    "2026-01-01T00:00:00Z",
					// no "commit" field
				}
				data, err := json.Marshal(ev)
				if err != nil {
					t.Fatalf("marshal event: %v", err)
				}
				if err := os.WriteFile(logPath, append(data, '\n'), 0o644); err != nil {
					t.Fatalf("write event log: %v", err)
				}
			},
			wantLeft:   true,
			wantReason: "plain path",
		},
		{
			// The event logs a commit SHA that does not exist in the repo;
			// git fails inside HashCitation and the rule stops the pipeline
			// with a git error reason.
			name:     "logged commit absent from repo: left with git error",
			citeHash: contentHash,
			makeLog: func(t *testing.T, logPath, id, citeStr string) {
				writeEventLog(t, logPath, id, citeStr, "deadbeef0000")
			},
			wantLeft:   true,
			wantReason: "cannot hash git: form",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			freshConfig(t)
			tempErrlog(t)
			srcSetupXDG(t, fmt.Sprintf("git=%s\nrepo r = %s\n", gitBin, repoDir))
			t.Setenv("TM_SRC_ROOT", repoDir)

			relFile := "file.txt"
			citeStr := tc.citeHash + "@" + relFile + ":1-2"

			graphDir := t.TempDir()
			t.Chdir(graphDir)
			graphFile := filepath.Join(graphDir, "g.mmd")
			if err := os.WriteFile(graphFile, []byte(buildRule1Graph(tc.citeHash)), 0o644); err != nil {
				t.Fatal(err)
			}
			tc.makeLog(t, graphFile+".jsonl", "c1", citeStr)

			out, errOut, code := run(t, "migrate", graphFile)
			if code != 0 {
				t.Fatalf("tm migrate: exit %d; stderr: %s", code, errOut)
			}

			if tc.wantLeft {
				if !strings.Contains(out, "left c1") {
					t.Errorf("want 'left c1' in output; got:\n%s", out)
				}
				if tc.wantReason != "" && !strings.Contains(out, tc.wantReason) {
					t.Errorf("want reason %q in output; got:\n%s", tc.wantReason, out)
				}
			} else if !strings.Contains(out, "ok c1") {
				t.Errorf("want 'ok c1' in output; got:\n%s", out)
			}
		})
	}
}

// TestMigrateRule1_SymlinkedRepoPath verifies that an alias registered through
// a symlinked directory path still matches the citation after EvalSymlinks.
func TestMigrateRule1_SymlinkedRepoPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	freshConfig(t)
	tempErrlog(t)

	gitBin, _ := exec.LookPath("git")
	repoDir := t.TempDir()
	fullSHA := repoInitGit(t, repoDir)
	sha12 := fullSHA[:12]
	contentHash := icite.Hash("line1\nline2")

	// Register the alias using a symlinked path.
	symlinkDir := filepath.Join(t.TempDir(), "linked-repo")
	if err := os.Symlink(repoDir, symlinkDir); err != nil {
		t.Skip("symlink not supported:", err)
	}

	srcSetupXDG(t, fmt.Sprintf("git=%s\nrepo r = %s\n", gitBin, symlinkDir))
	t.Setenv("TM_SRC_ROOT", repoDir) // source root uses the real path

	relFile := "file.txt"
	citeStr := contentHash + "@" + relFile + ":1-2"

	graphDir := t.TempDir()
	t.Chdir(graphDir)
	graphFile := filepath.Join(graphDir, "g.mmd")
	if err := os.WriteFile(graphFile, []byte(buildRule1Graph(contentHash)), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEventLog(t, graphFile+".jsonl", "c1", citeStr, fullSHA)

	out, errOut, code := run(t, "migrate", graphFile)
	if code != 0 {
		t.Fatalf("tm migrate (symlinked path): exit %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(out, "ok c1") {
		t.Errorf("migrate should rewrite c1 via symlinked alias; got:\n%s", out)
	}

	// The graph must contain the SHA-pinned git: locator.
	graphData, _ := os.ReadFile(graphFile)
	wantLocator := "git:r@" + sha12 + ":file.txt"
	if !strings.Contains(string(graphData), wantLocator) {
		t.Errorf("graph should contain %q; got:\n%s", wantLocator, string(graphData))
	}
}

// TestMigrateRule1_NoAlias tests the case where no alias covers the path.
// The rule should leave the citation with a "needs: tm repo add" reason.
func TestMigrateRule1_NoAlias(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	freshConfig(t)
	tempErrlog(t)

	gitBin, _ := exec.LookPath("git")
	repoDir := t.TempDir()
	fullSHA := repoInitGit(t, repoDir)

	// Set up XDG config with git but NO repo alias.
	srcSetupXDG(t, fmt.Sprintf("git=%s\n", gitBin))
	t.Setenv("TM_SRC_ROOT", repoDir)

	contentHash := icite.Hash("line1\nline2")
	relFile := "file.txt"
	citeStr := contentHash + "@" + relFile + ":1-2"

	graphDir := t.TempDir()
	t.Chdir(graphDir)
	graphFile := filepath.Join(graphDir, "g.mmd")
	if err := os.WriteFile(graphFile, []byte(buildRule1Graph(contentHash)), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEventLog(t, graphFile+".jsonl", "c1", citeStr, fullSHA)

	out, errOut, code := run(t, "migrate", graphFile)
	if code != 0 {
		t.Fatalf("tm migrate: exit %d; stderr: %s", code, errOut)
	}
	// The citation should be left (partial migrate is ok, exit 0).
	if strings.Contains(out, "ok c1") {
		t.Errorf("migrate should not have rewritten c1 (no alias); got:\n%s", out)
	}
	if !strings.Contains(out, "left") || !strings.Contains(out, "c1") {
		t.Errorf("migrate output should show c1 as left; got:\n%s", out)
	}
	if !strings.Contains(out, "tm repo add") {
		t.Errorf("migrate output should suggest 'tm repo add'; got:\n%s", out)
	}

	// Graph must still be format 2 (format was bumped even without rewrites).
	graphData, _ := os.ReadFile(graphFile)
	if !strings.Contains(string(graphData), "%% tm:format 2") {
		t.Errorf("graph should be format 2 even with no rewrites")
	}
}
