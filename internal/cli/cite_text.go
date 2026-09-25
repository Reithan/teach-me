package cli

import (
	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/source"
)

// citeResolver builds the source-aware resolver that every citation read in
// this package goes through. srcRoot is state.Config.SrcRoot (TM_SRC_ROOT or
// the graph's directory); converters, URI fetch, and git HEAD lookup come from
// the user config and .tmconfig (§13).
func citeResolver(srcRoot string) (*source.Resolver, error) {
	cfg, err := source.LoadConfig()
	if err != nil {
		return nil, err
	}
	return source.NewResolverWithConfig(cfg, srcRoot), nil
}

// readCiteText parses citeStr and reads the cited text under srcRoot.
//
// This is the single point through which citation text resolution flows in
// this package; checkCiteDrift and hashCiteText are its drift and hashing
// counterparts. Commands that need the resolver's metadata for the event log
// (add, q, edit, rehash) build their own resolver instead.
func readCiteText(citeStr, srcRoot string) (string, error) {
	cit, err := cite.Parse(citeStr)
	if err != nil {
		return "", err
	}
	r, err := citeResolver(srcRoot)
	if err != nil {
		return "", err
	}
	text, _, err := r.Read(cit)
	return text, err
}

// checkCiteDrift reports whether citeStr's stored hash no longer matches the
// current source text. A hashless citation never drifts; a read failure is an
// error, not drift.
func checkCiteDrift(citeStr, srcRoot string) (bool, error) {
	r, err := citeResolver(srcRoot)
	if err != nil {
		return false, err
	}
	drifted, _, err := r.CheckDrift(citeStr)
	return drifted, err
}

// hashCiteText reads citeStr through the resolver and returns the canonical
// hashed citation, refusing on a stored-hash mismatch.
func hashCiteText(citeStr, srcRoot string) (string, error) {
	return hashCiteTextInDir(citeStr, srcRoot, "")
}

// hashCiteTextInDir is hashCiteText with an explicit graph directory for
// aids-dir refusal checking (§4.6). Pass an empty graphDir to skip the check.
func hashCiteTextInDir(citeStr, srcRoot, graphDir string) (string, error) {
	r, err := citeResolver(srcRoot)
	if err != nil {
		return "", err
	}
	r.GraphDir = graphDir
	hashed, _, err := r.HashCitation(citeStr)
	return hashed, err
}
