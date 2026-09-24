package state

import (
	"fmt"
	"os"

	"github.com/reithan/teach-me/internal/config"
)

// ResolveFile returns the active graph path per §3:
//
//  1. flagFile (the global --file flag), when non-empty.
//  2. $TM_FILE, when set and non-empty.
//  3. The `file` key from .tmconfig in the working directory, then from the
//     user config (`$XDG_CONFIG_HOME/tm/config`, default `~/.config/tm/config`).
//
// Both config files use the key=value format of internal/config. A missing
// file is not an error; an unreadable one is. `tm new` and `tm load` write the
// `file` key into the user config, or into .tmconfig with --local.
//
// An error is returned only when every source is unavailable.
func ResolveFile(flagFile string) (string, error) {
	if flagFile != "" {
		return flagFile, nil
	}
	if v := os.Getenv("TM_FILE"); v != "" {
		return v, nil
	}
	v, ok, err := config.Lookup("file")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("no graph file: set --file, $TM_FILE, or run tm new / tm load")
	}
	return v, nil
}
