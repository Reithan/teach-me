package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fixedClock is a test clock that always returns the same time.
type fixedClock struct {
	t time.Time
}

func (c *fixedClock) Now() time.Time { return c.t }

// advancingClock advances its time by step on every Now() call.
// This lets tests drive deadline expiry without real-time sleeps.
type advancingClock struct {
	mu   sync.Mutex
	base time.Time
	step time.Duration
	n    int
}

func (c *advancingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.base.Add(time.Duration(c.n) * c.step)
	c.n++
	return t
}

func TestLockPath(t *testing.T) {
	got := LockPath("foo/graph.mmd")
	want := "foo/graph.mmd.lock"
	if got != want {
		t.Errorf("LockPath = %q, want %q", got, want)
	}
}

func TestAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	graph := filepath.Join(dir, "g.mmd")
	clk := &fixedClock{t: time.Now()}

	lk, err := Acquire(graph, clk)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Lock file must exist.
	if _, err := os.Stat(LockPath(graph)); err != nil {
		t.Fatalf("lock file missing after Acquire: %v", err)
	}

	if err := lk.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Lock file must be gone after Release.
	if _, err := os.Stat(LockPath(graph)); !os.IsNotExist(err) {
		t.Fatalf("lock file still present after Release")
	}
}

// TestSecondAcquireFailsWhileLocked verifies that a second Acquire fails when
// the first lock is held and the clock advances past the deadline.
//
// The advancing clock simulates time passing without real sleeps: each Now()
// call advances by 1 s, so the 10 s deadline expires after ~5 poll iterations
// (roughly 5 × 25 ms = 125 ms of real time).
func TestSecondAcquireFailsWhileLocked(t *testing.T) {
	dir := t.TempDir()
	graph := filepath.Join(dir, "g.mmd")
	clk := &fixedClock{t: time.Now()}

	lk, err := Acquire(graph, clk)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer func() { _ = lk.Release() }()

	// An advancing clock that ticks forward 1 s per Now() call expires the
	// 10 s deadline after a handful of poll iterations.
	adv := &advancingClock{base: time.Now(), step: 1 * time.Second}
	_, err = Acquire(graph, adv)
	if err == nil {
		t.Fatal("second Acquire should have failed while first lock is held")
	}
}

// TestSecondAcquireSucceedsAfterRelease verifies that a blocked Acquire
// succeeds once the holding goroutine releases the lock.
func TestSecondAcquireSucceedsAfterRelease(t *testing.T) {
	dir := t.TempDir()
	graph := filepath.Join(dir, "g.mmd")
	clk := &fixedClock{t: time.Now()}

	lk, err := Acquire(graph, clk)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	// Release in a goroutine after a brief real delay.
	done := make(chan struct{})
	go func() {
		time.Sleep(60 * time.Millisecond)
		_ = lk.Release()
		close(done)
	}()

	clk2 := &fixedClock{t: time.Now()}
	lk2, err := Acquire(graph, clk2)
	if err != nil {
		t.Fatalf("second Acquire after Release: %v", err)
	}
	_ = lk2.Release()
	<-done
}

// TestStaleTakeover verifies that Acquire takes over a lock whose stored
// timestamp is older than staleAge.
func TestStaleTakeover(t *testing.T) {
	dir := t.TempDir()
	graph := filepath.Join(dir, "g.mmd")
	lockPath := LockPath(graph)

	// Write a lock file with a timestamp that is > 30 s old.
	staleTime := time.Now().UTC().Add(-(staleAge + 5*time.Second))
	content := fmt.Sprintf("%s\n99999\n", staleTime.Format(time.RFC3339))
	if err := os.WriteFile(lockPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	clk := &fixedClock{t: time.Now()}

	// Acquire should recognise the stale lock and take over immediately.
	lk, err := Acquire(graph, clk)
	if err != nil {
		t.Fatalf("Acquire stale: %v", err)
	}
	_ = lk.Release()
}

// TestStaleTakeoverWithFreshLockDoesNotTakeover verifies that Acquire does NOT
// take over a fresh lock. The advancing clock drives deadline expiry so the
// test fails fast without a 10 s real-time wait.
func TestStaleTakeoverWithFreshLockDoesNotTakeover(t *testing.T) {
	dir := t.TempDir()
	graph := filepath.Join(dir, "g.mmd")
	lockPath := LockPath(graph)

	// Write a lock file with a fresh timestamp (≈ now, well under staleAge).
	freshTime := time.Now().UTC()
	content := fmt.Sprintf("%s\n99999\n", freshTime.Format(time.RFC3339))
	if err := os.WriteFile(lockPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write fresh lock: %v", err)
	}

	// Advancing clock expires the deadline after a handful of iterations.
	adv := &advancingClock{base: time.Now(), step: 1 * time.Second}
	_, err := Acquire(graph, adv)
	if err == nil {
		t.Fatal("expected Acquire to fail when lock is fresh and deadline expired")
	}

	// Lock file must still exist (not taken over as stale).
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Fatalf("fresh lock was incorrectly removed: %v", statErr)
	}
	// Clean up.
	_ = os.Remove(lockPath)
}

func TestWriteTempAndRename(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "graph.mmd")
	data := []byte("hello graph\n")

	if err := WriteTempAndRename(dst, data); err != nil {
		t.Fatalf("WriteTempAndRename: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content = %q, want %q", got, data)
	}

	// No temp file should remain.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "graph.mmd" {
			t.Errorf("unexpected leftover file: %s", e.Name())
		}
	}
}

func TestWriteTempAndRenameOverwrites(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "graph.mmd")
	if err := os.WriteFile(dst, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	newData := []byte("new content\n")
	if err := WriteTempAndRename(dst, newData); err != nil {
		t.Fatalf("WriteTempAndRename: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(newData) {
		t.Errorf("content = %q, want %q", got, newData)
	}
}
