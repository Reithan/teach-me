# Teach-Me Repo

## Getting started

After a fresh clone, run:

```sh
make tools hooks
```

## Make targets

| Target | Description |
|---|---|
| `make tools` | Install golangci-lint v2.13.2 into `.tools/bin`, diff-cover 10.5.1 via pipx, and `npm ci` in `conformance/` |
| `make hooks` | Configure git to use `.githooks/` (`git config core.hooksPath .githooks`) |
| `make precommit` | Lint, build, `go mod tidy -diff`, and version-sync check |
| `make check` | Full local CI: lint, test, diff-coverage, fuzz, conformance, vuln, and version-sync |
| `make lint` | Run golangci-lint |
| `make test` | Run `go test -race ./...` |
| `make coverage` | Generate coverage profile (excluding `cmd/tm` and `internal/tools`) |
| `make diff-coverage` | Check statement coverage of changed lines against `origin/main` at 85% |
| `make fuzz` | Run fuzz targets for 10 s each |
| `make conformance` | Run the Mermaid conformance suite |
| `make vuln` | Run `go tool govulncheck ./...` |
| `make version-sync` | Verify `internal/version/VERSION` matches `version` in `CITATION.cff`, and its major.minor matches `metadata.tm-version` in `skill/teach-me/SKILL.md` |

## Releasing

Bump the version in `internal/version/VERSION`, `CITATION.cff`, and (for a
major or minor bump) `metadata.tm-version` in `skill/teach-me/SKILL.md`, then
move `CHANGELOG.md`'s `[Unreleased]` entries under a new `## [X.Y.Z] - date`
heading and add its compare link. Pushing tag `vX.Y.Z` runs the release
workflow, which publishes that section as the GitHub release notes via
`.github/scripts/release-notes.sh` and fails if the section is missing.

## Spec

Full specification: `docs/spec.md`

## Adapters

Harness-specific adapters drive the CLI (spec §2.1); the CLI itself names no
model or agent. Reference adapters for a harness with skills and sub-agents ship
under `skill/teach-me/`:

| Adapter | File | Install to |
|---|---|---|
| Teacher | `skill/teach-me/SKILL.md` | `.claude/skills/teach-me/` |
| Grader | `skill/teach-me/agents/teach-me-grader.md` | `.claude/agents/` |
| Planner | `skill/teach-me/agents/teach-me-planner.md` | `.claude/agents/` |
| Pruner | `skill/teach-me/agents/teach-me-pruner.md` | `.claude/agents/` |
| Reader | `skill/teach-me/agents/teach-me-reader.md` | `.claude/agents/` |

The teacher drives the session: it spawns one `teach-me-planner` sub-agent to
decompose source into a concept graph, one `teach-me-pruner` sub-agent after every
planner run to cut the active graph down to what the goal requires, and one
`teach-me-grader` sub-agent per answer. The planner writes concepts and edges; the
pruner parks unneeded concepts in reserve; the grader scores each answer in
isolation through `tm check` and `tm grade`. When a source exceeds one `tm src`
window and the teacher needs the whole of it to locate ranges, the teacher spawns
one `teach-me-reader` sub-agent with the locator and a question; the reader runs
`tm src --fulldump` in a disposable context and returns a summary and candidate
line ranges without writing to the graph.

## Architecture

Prompt text for `tm check`, `tm check --drift`, `tm check --errata`, and the
`tm new` YAML frontmatter lives in `internal/cli/prompts/*.txt` as
`text/template` files rendered at runtime.

Auto-generated dependency diagrams (Mermaid, rendered inline on GitHub). Report immediately to user to regenerate if stale:

- [Repository structure](CODE_DIAGRAM.md) — top-level layout (`cmd`, `conformance`, `internal`) and external dependencies.
- [Internal package graph](internal/CODE_DIAGRAM.md) — import dependencies among the `internal/` packages.
