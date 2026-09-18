package state

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ResolveFile resolves the active graph file path using the §3 precedence
// rule:
//
//  1. flagFile (the --file argument), if non-empty.
//  2. $TM_FILE environment variable, if set and non-empty.
//  3. The `file` key from .tmconfig in the current working directory.
//
// # .tmconfig format
//
// UTF-8 text; one key=value pair per line (leading/trailing whitespace around
// key and value is trimmed). A `#` as the very first non-space character on a
// line introduces a comment and is ignored. Blank lines are ignored. Recognised
// keys: `file` (path to the graph), `doc` (TM_DOC equivalent). Unknown keys
// are silently ignored so future keys can be added without breaking older
// readers.
//
// A missing .tmconfig is not an error. A present but unreadable .tmconfig is.
// A .tmconfig that contains no `file` key is treated as absent (the error
// message reflects this). The file written by `tm new` and `tm load` (M5)
// must use this same format — at minimum writing a `file=<path>` line.
//
// An error is returned only when all three sources are unavailable.
func ResolveFile(flagFile string) (string, error) {
	if flagFile != "" {
		return flagFile, nil
	}
	if v := os.Getenv("TM_FILE"); v != "" {
		return v, nil
	}

	// Attempt .tmconfig in the working directory.
	f, err := os.Open(".tmconfig")
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no graph file: set --file, $TM_FILE, or write .tmconfig")
	}
	if err != nil {
		return "", fmt.Errorf(".tmconfig: %w", err)
	}
	defer f.Close() //nolint:errcheck

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if key == "file" && val != "" {
			return val, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf(".tmconfig: %w", err)
	}
	return "", fmt.Errorf("no graph file: .tmconfig has no file= key; set --file or $TM_FILE")
}
