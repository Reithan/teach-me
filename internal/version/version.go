// Package version provides the CLI version string embedded from the VERSION file.
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionData string

// Version returns the trimmed version string embedded from VERSION.
func Version() string {
	return strings.TrimSpace(versionData)
}
