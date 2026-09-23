package cli

import (
	"errors"
	"fmt"

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
