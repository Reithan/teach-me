// Package lockfile implements the advisory lock described in §3 and §16.4.
//
// Every mutating command acquires the lock before writing the graph. Parallel
// graders can therefore run without corrupting the file.
package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reithan/teach-me/internal/errlog"
)

const (
	pollInterval  = 25 * time.Millisecond
	lockDeadline  = 10 * time.Second
	staleAge      = 30 * time.Second
	renameRetries = 3
	renameSleep   = 5 * time.Millisecond
)

// Lock is an acquired advisory lock. Release removes the lock file.
type Lock struct {
	path string
	f    *os.File
}

// LockPath returns the lock file path for the given graph file.
func LockPath(graphFile string) string {
	return graphFile + ".lock"
}

// Acquire creates the lock file for graphFile and returns the Lock handle.
//
// If the lock file already exists and is not stale, Acquire polls every
// 25 ms until the file is removed or the 10 s deadline passes. If the
// stored timestamp inside the lock file is older than 30 s the lock is
// considered stale and is taken over immediately.
//
// The lock file contains two lines: the acquisition time in RFC3339 format
// and the acquiring process's PID. Both are written on success.
func Acquire(graphFile string, clk errlog.Clock) (*Lock, error) {
	path := LockPath(graphFile)
	deadline := clk.Now().Add(lockDeadline)

	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			now := clk.Now().UTC()
			_, _ = fmt.Fprintf(f, "%s\n%d\n", now.Format(time.RFC3339), os.Getpid())
			return &Lock{path: path, f: f}, nil
		}

		if !os.IsExist(err) {
			return nil, fmt.Errorf("lockfile: open %s: %w", path, err)
		}

		// Lock file exists. Check if it is stale by reading the stored timestamp.
		if data, readErr := os.ReadFile(path); readErr == nil {
			first, _, _ := strings.Cut(string(data), "\n")
			if t, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(first)); parseErr == nil {
				if clk.Now().UTC().Sub(t.UTC()) > staleAge {
					// Stale lock: remove it and retry the O_EXCL create immediately.
					_ = os.Remove(path)
					continue
				}
			}
		}

		// Deadline expired?
		if !clk.Now().Before(deadline) {
			return nil, fmt.Errorf("graph is locked (%s)", path)
		}

		time.Sleep(pollInterval)
	}
}

// Release removes the lock file and closes the file handle.
func (l *Lock) Release() error {
	removeErr := os.Remove(l.path)
	closeErr := l.f.Close()
	if removeErr != nil {
		return removeErr
	}
	return closeErr
}

// WriteTempAndRename writes data to a temporary file adjacent to finalPath,
// fsyncs, and renames over finalPath atomically. On platforms where
// rename-over-existing can fail transiently the rename is retried a few times.
//
// The temporary file is cleaned up on any error.
func WriteTempAndRename(finalPath string, data []byte) error {
	dir := filepath.Dir(finalPath)
	tmp := fmt.Sprintf("%s.tmp.%d", filepath.Base(finalPath), os.Getpid())
	tmpPath := filepath.Join(dir, tmp)

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("lockfile: create temp: %w", err)
	}

	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("lockfile: write temp: %w", err)
	}

	if err = f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("lockfile: sync temp: %w", err)
	}

	if err = f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("lockfile: close temp: %w", err)
	}

	for i := 0; i < renameRetries; i++ {
		if err = os.Rename(tmpPath, finalPath); err == nil {
			return nil
		}
		if i < renameRetries-1 {
			time.Sleep(renameSleep)
		}
	}

	_ = os.Remove(tmpPath)
	return fmt.Errorf("lockfile: rename: %w", err)
}
