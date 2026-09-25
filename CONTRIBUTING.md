# Contributing

Contributions are welcome: bug reports, fixes, features, and improvements to the
skill and agent files.

## Before you start

1. Read [AGENTS.md](AGENTS.md) for setup, make targets, and architecture, and
   [docs/spec.md](docs/spec.md) for the design.
2. For anything larger than a small fix, open an issue first so we can agree on
   the approach.

## Pull requests

- Branch from `main` with a descriptive name, such as `fix-grader-scope-check`.
- Run `make tools hooks` once after cloning. The hooks run the same checks as
  CI; `make check` runs them all by hand.
- Keep each pull request to one change.

## License and CLA

teach-me is free for individual use under the [teach-me Individual Use
License](LICENSE), and the maintainer sells separate licenses for
organizational use. To keep both possible, every contributor signs the
[Contributor License Agreement](CLA.md) once. You keep your copyright; the CLA
lets your contribution ship under both kinds of license. A bot prompts you to
sign on your first pull request.
