# Changelog

All notable changes to `tm` and the `teach-me` skill tree. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/) with prerelease tags while the CLI is
in beta. Each release names the `docs/spec.md` revision it implements.

## [Unreleased]

## [0.5.0-beta.1] - 2026-09-25

Spec v0.30. Skill `tm-version` is `0.5`.

### Added

- `LICENSE` (Apache-2.0) and `NOTICE`.
- `CITATION.cff`, `CONTRIBUTING.md`, and `THIRD_PARTY_NOTICES.md` (the Go
  standard library license). Release archives now include `LICENSE`, `NOTICE`,
  `THIRD_PARTY_NOTICES.md`, and `CITATION.cff`.
- `tm reserve <concept> --reason "<text>"` now accepts a concept that already
  has questions, provided all its batches are resolved (every question has a
  non-pending answer). The reason is logged on the `reserve` event. Concepts
  with no questions still work without `--reason`. (#75)
- `tm unlink <from> <to>` removes a concept→concept prerequisite edge. Reserve
  endpoints are accepted, consistent with `tm link`. Logs an `unlink` event
  with `from` and `to`. (#75)

### Changed

- `make version-sync` also requires `version` in `CITATION.cff` to match
  `internal/version/VERSION`.
- `tm add --parent/--child` and `tm link` accept `reserve` concepts as
  endpoints. A reserve parent still never blocks the frontier, so the planner
  can wire a parked foundation to its children and `tm activate` wakes it
  already linked. The cycle check now spans reserve concepts.
- Lint check 12 no longer flags reserve concepts with questions. A reserve
  concept may hold its testing history while parked. (#75)

### Fixed

- `tm add` on an ID in reserve refuses with exit 1 and `fix: tm activate <id>`
  instead of failing the post-write lint (duplicate declaration, exit 2).
- `tm answer` and `tm ask` no longer refuse the remaining questions of an
  open-and-failed probe batch with "teaching round not complete". Those questions
  must be answered to close the batch; the teaching-round restriction applies
  only to later (fallback) probe batches. Fixes a deadlock where neither
  `tm answer` nor `tm q` could make progress (#78).

## [0.4.0-beta.1] - 2026-09-25

Spec v0.26. Graphs written by earlier versions carry format 1; run `tm migrate`
once to upgrade them (see Changed). Skill `tm-version` is `0.4`.

### Added

- `tm errata <concept> "<scope>" "<reason>"` rewrites the scope of a concept
  that already has questions or a pass; `tm edit` is unchanged and still refuses
  such a concept. Questions and answers stay untouched; the reason and the
  before/after text are logged in an `errata` event. A passed concept then goes to a grader recheck
  through `tm check --errata` and `tm grade --errata keep|reopen`, mirroring the
  drift recheck. (#68)
- `tm check <qid>` prints the question's concept scope as its first line,
  `CONCEPT <id>: <scope>`, and the rubric grades a question outside that scope
  `unclear` with a summary starting `out of scope:`, so the teacher replaces
  it with `tm q --re`. (#69)
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
- `SKILL.md` loads session start, gate exits, and errata handling on demand
  from `reference/start.md`, `reference/gate.md`, and `reference/errata.md`;
  the session ends when the goal concept passes. (#70)
- Agents and references revised after a prompt review: the planner labels
  `QUESTION:` and `ERR:` lines the teacher acts on, the pruner walks outward
  from the goal under a checkable done condition, the reader returns inclusive
  ranges or `ranges: none`, and `setup.md` states the session limits as the
  spec defines them. (#71)

### Fixed

- `tm drop --help` names its argument `<id>`, since it also takes a drifted
  question ID; `tm grade --help` and `tm check --help` print the `--drift` and
  `--errata` forms that take a `<concept>`. (#69)

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

[Unreleased]: https://github.com/Reithan/teach-me/compare/v0.5.0-beta.1...HEAD
[0.5.0-beta.1]: https://github.com/Reithan/teach-me/compare/v0.4.0-beta.1...v0.5.0-beta.1
[0.4.0-beta.1]: https://github.com/Reithan/teach-me/compare/v0.3.0-beta.1...v0.4.0-beta.1
[0.3.0-beta.1]: https://github.com/Reithan/teach-me/compare/v0.2.1-beta.2...v0.3.0-beta.1
[0.2.1-beta.2]: https://github.com/Reithan/teach-me/compare/v0.2.1-beta.1...v0.2.1-beta.2
[0.2.1-beta.1]: https://github.com/Reithan/teach-me/compare/v0.2.0-beta.1...v0.2.1-beta.1
[0.2.0-beta.1]: https://github.com/Reithan/teach-me/releases/tag/v0.2.0-beta.1
