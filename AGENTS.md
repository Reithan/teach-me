# Teach-Me Repo

<!-- TODO -->

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
| `make version-sync` | Verify `internal/version/VERSION` major.minor matches `metadata.tm-version` in `skill/teach-me/SKILL.md` |

## Spec

Full specification: `docs/spec.md`
