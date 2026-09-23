package cli

import (
	"github.com/reithan/teach-me/internal/cite"
)

// readCiteText parses citeStr and reads the cited text under srcRoot.
//
// This is the single point through which all citation text resolution flows in
// this package. The sibling branch (source resolution) will redirect this
// function to add URI fetch and git HEAD support; no other call site in this
// package needs to change.
func readCiteText(citeStr, srcRoot string) (string, error) {
	cit, err := cite.Parse(citeStr)
	if err != nil {
		return "", err
	}
	return cite.ReadRange(cit, srcRoot)
}
