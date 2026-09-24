// Package config owns the key=value configuration files of spec §13 and the
// pointer keys that make a graph active: `file`, `src-root`, and `doc`.
//
// Two files are layered. The per-project `.tmconfig` in the working directory
// overrides the user-level config at `$XDG_CONFIG_HOME/tm/config` (default
// `~/.config/tm/config`). Both share one format: UTF-8 text, one `key = value`
// per line, whitespace around key and value trimmed, blank lines and lines
// whose first non-space character is `#` ignored, unknown keys ignored.
//
// Pointer keys are looked up here. Converter keys are parsed by internal/source
// with the same format; this package deliberately knows nothing about them.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LocalName is the per-project config file name, looked up in the working
// directory.
const LocalName = ".tmconfig"

// UserPath returns `$XDG_CONFIG_HOME/tm/config`, or `~/.config/tm/config`
// when XDG_CONFIG_HOME is unset. When the home directory cannot be
// determined it falls back to a relative `.config/tm/config`.
func UserPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "tm", "config")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "tm", "config")
	}
	return filepath.Join(home, ".config", "tm", "config")
}

// Lookup returns the value of key from `.tmconfig` in the working directory,
// falling back to the user config. ok is false when neither file sets the key.
// A missing file is not an error; an unreadable one is.
func Lookup(key string) (val string, ok bool, err error) {
	for _, path := range []string{LocalName, UserPath()} {
		v, found, rerr := lookupIn(path, key)
		if rerr != nil {
			return "", false, rerr
		}
		if found {
			return v, true, nil
		}
	}
	return "", false, nil
}

// lookupIn returns the last value of key in the file at path. A missing file
// yields ok=false with no error.
func lookupIn(path, key string) (val string, ok bool, err error) {
	f, oerr := os.Open(path)
	if os.IsNotExist(oerr) {
		return "", false, nil
	}
	if oerr != nil {
		return "", false, fmt.Errorf("%s: %w", path, oerr)
	}
	defer f.Close() //nolint:errcheck

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, isKV := parseLine(sc.Text())
		if isKV && k == key && v != "" {
			val, ok = v, true
		}
	}
	if serr := sc.Err(); serr != nil {
		return "", false, fmt.Errorf("%s: %w", path, serr)
	}
	return val, ok, nil
}

// parseLine splits one config line into key and value. isKV is false for
// blank lines, comments, and lines without `=`.
func parseLine(line string) (key, val string, isKV bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return "", "", false
	}
	idx := strings.IndexByte(t, '=')
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(t[:idx]), strings.TrimSpace(t[idx+1:]), true
}

// Set writes each key in kv into the file at path, replacing the line that
// already sets it or appending a new one, and leaves every other line intact.
// The file and its directory are created when missing. Keys are written in
// sorted order so output is deterministic. The write is atomic: temp file in
// the same directory, then rename.
func Set(path string, kv map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", path, err)
	}

	pending := make(map[string]string, len(kv))
	for k, v := range kv {
		pending[k] = v
	}

	var lines []string
	if len(data) > 0 {
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	}
	for i, line := range lines {
		k, _, isKV := parseLine(line)
		if !isKV {
			continue
		}
		if v, ok := pending[k]; ok {
			lines[i] = k + " = " + v
			delete(pending, k)
		}
	}
	for _, k := range sortedKeys(pending) {
		lines = append(lines, k+" = "+pending[k])
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("%s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("%s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("%s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SrcRoot returns the citation root for a graph in graphDir: `$TM_SRC_ROOT`
// when set, else the `src-root` config key, else graphDir itself. Config read
// errors are treated as absent so a broken config cannot block a read-only
// command; the graph-file lookup surfaces them instead.
func SrcRoot(graphDir string) string {
	if root := os.Getenv("TM_SRC_ROOT"); root != "" {
		return root
	}
	if v, ok, err := Lookup("src-root"); err == nil && ok {
		return v
	}
	return graphDir
}

// Doc returns the path of the harness document that describes tm: `$TM_DOC`
// when set, else the `doc` config key, else "".
func Doc() string {
	if v := os.Getenv("TM_DOC"); v != "" {
		return v
	}
	if v, ok, err := Lookup("doc"); err == nil && ok {
		return v
	}
	return ""
}
