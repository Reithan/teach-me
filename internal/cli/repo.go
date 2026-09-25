package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/config"
	"github.com/reithan/teach-me/internal/source"
)

// repoRun is the Run handler for:
//
//	tm repo add <alias> <path> [--local]
//	tm repo rm <alias>
//	tm repo list
func repoRun(ctx *Context) int {
	sub := ctx.Positionals[0]
	switch sub {
	case "add":
		return repoAdd(ctx)
	case "rm":
		return repoRM(ctx)
	case "list":
		return repoList(ctx)
	default:
		// Should not happen: parseArgs already validated the enum.
		ctx.ErrMsg = fmt.Sprintf("unknown repo subcommand %q", sub)
		ctx.FixMsg = FindCommand("repo").Usage()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}
}

// repoAdd handles `tm repo add <alias> <path> [--local]`.
func repoAdd(ctx *Context) int {
	usage := "tm repo add <alias> <path> [--local]"
	if len(ctx.Positionals) < 3 {
		ctx.ErrMsg = "repo add requires <alias> and <path>"
		ctx.FixMsg = usage
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}
	alias := ctx.Positionals[1]
	rawPath := ctx.Positionals[2]

	if !cite.ValidAlias(alias) {
		ctx.ErrMsg = fmt.Sprintf("invalid alias %q (must match [A-Za-z0-9_-]+)", alias)
		ctx.FixMsg = usage
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}

	abs, err := filepath.Abs(rawPath)
	if err != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot resolve %q: %v", rawPath, err)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	fi, statErr := os.Stat(abs)
	if statErr != nil || !fi.IsDir() {
		ctx.ErrMsg = fmt.Sprintf("%q is not an existing directory", rawPath)
		ctx.FixMsg = usage
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}

	// If git is configured, verify the directory is a git repo.
	srcCfg, cfgErr := source.LoadConfig()
	if cfgErr != nil {
		ctx.ErrMsg = fmt.Sprintf("source config: %v", cfgErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	if srcCfg.Git != "" {
		cmd := exec.Command(srcCfg.Git, "-C", abs, "rev-parse", "--git-dir") //nolint:gosec
		if runErr := cmd.Run(); runErr != nil {
			ctx.ErrMsg = fmt.Sprintf("%q is not a git repo (git rev-parse --git-dir failed)", abs)
			ctx.FixMsg = usage
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 3
		}
	}

	_, local := ctx.Flags["local"]
	cfgPath := config.UserPath()
	if local {
		cfgPath = config.LocalName
	}

	if setErr := config.Set(cfgPath, map[string]string{"repo " + alias: abs}); setErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot write %s: %v", cfgPath, setErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	_, _ = fmt.Fprintln(ctx.Out, "ok")
	return 0
}

// repoRM handles `tm repo rm <alias>`.
func repoRM(ctx *Context) int {
	usage := "tm repo rm <alias>"
	if len(ctx.Positionals) < 2 {
		ctx.ErrMsg = "repo rm requires <alias>"
		ctx.FixMsg = usage
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}
	alias := ctx.Positionals[1]

	if !cite.ValidAlias(alias) {
		ctx.ErrMsg = fmt.Sprintf("invalid alias %q (must match [A-Za-z0-9_-]+)", alias)
		ctx.FixMsg = usage
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}

	// Remove from both user config and .tmconfig (whichever holds the key).
	for _, cfgPath := range []string{config.UserPath(), config.LocalName} {
		if _, statErr := os.Stat(cfgPath); statErr == nil {
			if setErr := config.Set(cfgPath, map[string]string{"repo " + alias: ""}); setErr != nil {
				ctx.ErrMsg = fmt.Sprintf("cannot write %s: %v", cfgPath, setErr)
				writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
				return 3
			}
		}
	}

	_, _ = fmt.Fprintln(ctx.Out, "ok")
	return 0
}

// repoList handles `tm repo list`.
func repoList(ctx *Context) int {
	srcCfg, cfgErr := source.LoadConfig()
	if cfgErr != nil {
		ctx.ErrMsg = fmt.Sprintf("source config: %v", cfgErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	aliases := make([]string, 0, len(srcCfg.Repos))
	for alias := range srcCfg.Repos {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)

	for _, alias := range aliases {
		_, _ = fmt.Fprintf(ctx.Out, "%s %s\n", alias, srcCfg.Repos[alias])
	}
	return 0
}
