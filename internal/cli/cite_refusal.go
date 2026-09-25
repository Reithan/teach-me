package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/reithan/teach-me/internal/source"
)

// citeHashError handles an error returned by resolver.HashCitation or
// resolver.Read. A source.RefusalError is a §7 invariant row (exit 1); any
// other error is a usage/config error (exit 3).
func citeHashError(ctx *Context, citeStr string, err error, usageLine string) int {
	var ref *source.RefusalError
	if errors.As(err, &ref) {
		ctx.ErrMsg = ref.Err
		ctx.FixMsg = ref.Fix
		writeErrFix(ctx.ErrOut, ref.Err, ref.Fix)
		return 1
	}
	ctx.ErrMsg = fmt.Sprintf("citation %q: %v", citeStr, err)
	ctx.FixMsg = usageLine
	writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
	return 3
}

// aidRefusalWithID replaces the "<id>" placeholder in a RefusalError Fix line
// with the real node id. Returns the original error if it is not a
// source.RefusalError or does not contain the placeholder.
func aidRefusalWithID(err error, id string) error {
	var ref *source.RefusalError
	if errors.As(err, &ref) && strings.Contains(ref.Fix, "<id>") {
		newRef := *ref
		newRef.Fix = strings.Replace(ref.Fix, "<id>", id, 1)
		return &newRef
	}
	return err
}
