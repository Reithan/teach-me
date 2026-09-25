# Changelog

All notable changes to `tm` and the `teach-me` skill tree. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/) with prerelease tags while the CLI is
in beta. Each release names the `docs/spec.md` revision it implements.

## [0.4.0-beta.1] - 2026-09-25

Spec v0.25. Graphs written by earlier versions carry format 1; run `tm migrate`
once to upgrade them (see Changed). Skill `tm-version` is `0.4`.

### Added

- `tm errata <concept> "<scope>" "<reason>"` rewrites the scope of a concept
  that already has questions or a pass; `tm edit` is unchanged and still refuses
  such a concept. Questions and answers stay untouched; the reason and the
  before/after text are logged in an `errata` event. A passed concept then goes to a grader recheck
  through `tm check --errata` and `tm grade --errata keep|reopen`, mirroring the
  drift recheck. (#68)
- `tm src <locator> [START-END] [--find <regex>] [--fulldump]` prints converted
  source text with line numbers through the same resolution pipeline as `add`
  and `q`. Output is a bounded window with a `more:` trailer; `--fulldump`
  prints everything for the reader role. (#62, #65)
- `%% tm:format` marker and `tm migrate` to upgrade format-1 graphs, rewriting
  each citation only to a locator that resolves to the same hash and listing
  what it cannot convert; `--dry-run` previews. Lint check 17 enforces the
  marker. (#53, #54, #56, #58)
- `git:<alias>@<ref>:<path>` locators with machine-local repo aliases:
  `tm repo add|rm|list` (`--local` writes `.tmconfig`). (#55)
- Fetch cache for URL sources with a TTL (`TM_CACHE_TTL`, `TM_CACHE_DIR`), plus
  `tm cache clear` and `tm cache list`. (#57)
- Aids: `%% tm:aid` meta line, `tm aid <id> <path>` and `tm aid rm`, aids shown
  by `show`, `report`, and `gc`; citations under the aids-dir are refused (lint
  check 16). (#59, #60)
- `tm answer --concede` records a learner-declared fail without a grader. (#50)
- `tm answer --asked "<wording>"` records the teacher's paraphrase of a question
  so the grader honours the premise the learner actually heard. (#61)
- Monotonic question and batch IDs via the `%% tm:next` meta line, so IDs are
  never reused after `gc`. (#49)
- `teach-me-reader` sub-agent: summarises a source that exceeds one `tm src`
  window in a disposable context and returns candidate line ranges. (#67)
- `skill/teach-me/reference/map.md` with the planner and pruner spawn prompts
  and activation rules, loaded only at the Map step. (#48, #63)

### Changed

- Graph format is now 2. Every command except `migrate`, `lint`, and help
  refuses a format-1 graph with `fix: tm migrate`. (#53, #54)
- Plain path citations no longer carry git semantics; use a `git:` locator when
  the ref matters. (#56)
- Question citations are capped at 120 lines or 6,000 characters at write time;
  `tm report --fulltext` prints a bounded per-concept window. Concept citations
  stay uncapped. (#64, #66)
- Grader rubric defines a gap and honours the question's premises: a more
  complete answer is not a gap, and text outside the question's scope is not
  graded. (#61)
- Teacher skill rewritten for the Source step, repos, aids, and `tm src`; it no
  longer activates reserve concepts on its own read of the map after a prune.
  Nothing under `skill/` points at the spec, which does not ship. (#48, #63,
  #67)
- Grader agent gains the errata recheck alongside the drift recheck. (#68)

## [0.3.0-beta.1] - 2026-09-24

Spec v0.19.

### Added

- `reserve` block in the graph: `tm reserve`, `tm activate`, and `tm prune
  --keep N` park and restore concepts the goal does not need; `tm status` prints
  the reserve count; `activate` is a gate exit. (#43, #44, #45)
- `teach-me-pruner` sub-agent and goal-first planner rules: the planner primes
  inclusion and the pruner cuts the active graph down to what the goal
  requires after every planner run. (#46)
- Config pointer keys `file`, `src-root`, and `doc` in the user config;
  `tm new` and `tm load` write them, `--local` writes `.tmconfig` instead;
  `--src-root` on `new`. (#42)
- `tm --help --all` prints every command; the default help stays short. (#42)
- Sandbox setup notes in `skill/teach-me/reference/setup.md`. (#42)

### Changed

- Grader scope rule: an answer is graded against the question's scope only,
  not against source text the question did not ask for. (#42)

## [0.2.1-beta.2] - 2026-09-23

### Fixed

- Release archives include `SKILL.md`. (#41)

## [0.2.1-beta.1] - 2026-09-23

### Fixed

- Release archives ship the full `skill/teach-me/` tree; `docs/spec.md` is not
  shipped. Attestation is skipped on the private repository. (#40)

## [0.2.0-beta.1] - 2026-09-23

Spec v0.18. First tagged release: the `tm` CLI (graph model, lint, citations
with content hashes, source resolution and converters, drift and recheck,
`tm report`), the release pipeline, and the teacher, grader, and planner
adapters under `skill/teach-me/`. (#1 through #39)

[0.4.0-beta.1]: https://github.com/Reithan/teach-me/compare/v0.3.0-beta.1...v0.4.0-beta.1
[0.3.0-beta.1]: https://github.com/Reithan/teach-me/compare/v0.2.1-beta.2...v0.3.0-beta.1
[0.2.1-beta.2]: https://github.com/Reithan/teach-me/compare/v0.2.1-beta.1...v0.2.1-beta.2
[0.2.1-beta.1]: https://github.com/Reithan/teach-me/compare/v0.2.0-beta.1...v0.2.1-beta.1
[0.2.0-beta.1]: https://github.com/Reithan/teach-me/releases/tag/v0.2.0-beta.1
