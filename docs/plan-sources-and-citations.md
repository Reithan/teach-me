# Sources and citations: design plan

Status: agreed 2026-09-23, not implemented. `spec.md` describes the shipped
CLI; this document describes the target. The spec revision (task M8) folds
this in, after which this file is deleted.

## 1. Problem

The teacher adapter starts at `tm load` and says nothing about where source
material comes from. Every `tm add` and `tm q` needs a citation that resolves
to real lines, so no session can begin without a corpus, and the `fix:` line
cannot help because the fix lies outside the CLI. A capable model resolves this
the obvious way: it writes notes from memory and cites them. That defeats the
isolated grader silently, since the grader then checks the model against
itself, and it is worst on the topics the model half-knows.

Three points of failure support two requirements.

Points of failure:

1. The model finds sources for a concept.
2. The model records quotations or summaries from those sources.
3. The model writes questions from the recorded material.

Requirements:

1. The teacher needs context to decide what and how to teach.
2. Questions need citations, for provenance and for grading.

Point 2 is removed entirely: nothing model-authored is stored as source. Point
1 is bounded by the source procedure in the skill (section 9). Point 3 is
already covered by the grader, which reads the cited text, not the model's
description of it.

## 2. Principles

- The graph is the corpus index. The only text the model writes is a scope
  label; the passage it points at is a citation the CLI verifies at write
  time. No model-authored summaries, anthologies, or quotations exist anywhere.
- Deterministic checks wherever possible; judged checks only where determinism
  is impossible. Citation resolution, hash comparison, and report generation
  are deterministic. Grading stays judged and isolated.
- By default the CLI fetches nothing and runs nothing. A user may configure
  converters and git; only then does either happen.
- Nodes are immutable. Drift is derived at read time, never stored. Errata are
  additive.
- The no-memory rule: model knowledge drafts questions and explains during
  teaching. It never becomes source. When no real source can be obtained, the
  teacher says so and stops.

## 3. Citation grammar

```
<hash>@<locator>:START-END
```

| Part | Rule |
|---|---|
| `hash` | first 12 hex characters of SHA-256 over the normalized cited text (3.2). Fixed width. Read first; the `@` after it is the delimiter, so `@` inside a locator is harmless |
| `locator` | a path relative to `TM_SRC_ROOT`; an absolute path (`/...`, or a drive letter on Windows); or a URI with a scheme (`https://...`). Distinguished by prefix; no per-kind syntax |
| `START-END` | 1-based inclusive line range into the resolved text, after conversion if any. Split on the last colon; the range never contains one, so scheme separators, ports, and drive letters are harmless |

Examples:

```
3f9a1c2b7e0d@raft.txt:202-215
3f9a1c2b7e0d@/home/me/projects/other/internal/foo.go:10-24
3f9a1c2b7e0d@https://arxiv.org/html/2401.12345v2:88-104
```

The model never types the hash. `tm add` and `tm q` accept the hashless form
`<locator>:START-END`, resolve the text, compute the hash, and write the full
form. The hash is a content hash of the cited lines, not a commit hash; the
position invites that reading, so the spec says so in one line.

### 3.1 Labels

Citations live in the node label as today. A `"` in a locator must be
percent-encoded; lint rejects a raw one. Long URIs lengthen labels, so the
deferred Mermaid size limits (spec 15) become worth measuring in M9.

### 3.2 Normalization

Before hashing: CRLF to LF; trailing whitespace stripped per line; lines joined
with LF; no trailing newline; hash over the UTF-8 bytes. Internal whitespace is
preserved because indentation is meaningful in code.

### 3.3 Migration

Pre-1.0, so no compatibility shim. `tm rehash` is a one-shot command: for every
citation without a hash it resolves the text, writes the hash, and logs a
`rehash` event. Lint refuses hashless citations otherwise. Golden graphs and
the conformance corpus are regenerated.

## 4. Resolution

Order: parse, locate, convert if a converter matches, slice the range, compare
the hash.

### 4.1 Paths

- If a converter is configured for the file's extension (section 5), the file
  is read through it; otherwise raw.
- **Missing file inside a git repo.** The path may not exist locally: sparse
  checkout, a directory never checked out, a file deleted in the working tree.
  Walk up from the longest existing ancestor of the path until a `.git` entry
  is found (a directory, or a file with `gitdir:` for worktrees and
  submodules). Compute the repo-relative path. If git is configured (section
  5), request the blob at `HEAD` through it. If git is not configured, or
  `HEAD` has no such blob, refuse:

  ```
  err: my-folder/file.txt is not in the working tree
  fix: check it out, or set git in <config> to read it from HEAD
  ```

  The CLI never talks to a remote git server. A file that exists only on the
  remote requires the user to fetch, or the teacher cites the remote URL at a
  commit instead. The hash check applies to whatever text was obtained, so a
  `HEAD` blob that differs from what was cited is reported as drift like any
  other.
- **Commit recording.** On `add` and `q`, when the locator is inside a repo,
  the CLI reads `HEAD` itself (`.git/HEAD`, then the ref under `refs/heads/`
  or in `packed-refs`, following `commondir` for worktrees) and records the
  commit in the log event (section 7). No exec.

### 4.2 URIs

- Fetch with the standard library: follow redirects and record the final URL;
  a timeout and a size cap; UTF-8 assumed; no script execution. `Content-Type`
  gives the MIME type; a converter is matched by MIME, then by the extension
  of the URL path.
- `text/plain` and `text/markdown` are read raw. Any other type with no
  configured converter refuses with a `fix:` naming the config file.
- A failed fetch (no egress, timeout, non-2xx) refuses:

  ```
  err: fetch https://... failed: <reason>
  fix: save a static copy under TM_SRC_ROOT and cite it
  ```

- Dynamic pages are a stated limitation. The correct move is a static copy,
  cited locally.

### 4.3 One conversion rule

A converter applies whenever one is configured for the MIME type, or for the
extension of a local file, regardless of whether the source is local or
remote. Consequence: a local `.html` file is cited by converted line numbers
once a converter for it exists. Conversion is deterministic given the same
input bytes and the same converter version, so converted output is
regenerable and never stored durably. A cache under the OS temp directory,
keyed by input hash, converter, and version, is an optional optimization
(section 12).

### 4.4 PDF

Through the same mechanism, with `pdftotext`. For arXiv prefer the versioned
HTML rendering or the e-print source; PDF is the fallback, not the default.

## 5. Converter configuration

Location: user-level `$XDG_CONFIG_HOME/tm/config` (default
`~/.config/tm/config`), with `.tmconfig` keys overriding per project. Same
`key=value` style as `.tmconfig`. Converters are machine-specific, which is
why the default location is user-level.

```
convert text/html = pandoc -f html -t gfm --wrap=none
convert application/pdf = pdftotext -layout - -
ext html = text/html
ext pdf = application/pdf
version pandoc = 3.1.11
version pdftotext = 24.02.0
version-cmd pdftotext = pdftotext -v
git = git
```

Rules:

- A converter reads the source bytes on stdin and writes text on stdout. A
  non-zero exit refuses the citation with the converter's stderr in the
  `err:` line.
- **Version pin, checked at runtime.** Every converter named in `convert` must
  have a `version` line. Before its first use in a process the CLI runs the
  version command (default `<program> --version`; `version-cmd` overrides it,
  since `pdftotext` uses `-v` and prints to stderr), takes the first line of
  combined output, and requires the pinned string as a substring. Mismatch
  refuses:

  ```
  err: pandoc is 3.2.0, config pins 3.1.11
  fix: set version pandoc = 3.2.0 in <config>; citations made under 3.1.11 may drift
  ```

- `git` enables `HEAD` blob resolution (4.1) and nothing else.
- Suggested defaults, documented in the skill: pandoc for HTML and document
  formats, poppler's `pdftotext` for PDF, both pinned.
- With no config the CLI execs nothing and fetches nothing. Spec 2.1 is
  narrowed, not reversed: the CLI runs no external command except converters
  and git the user has configured, and it names no harness or model.
- Security: converters process untrusted bytes fetched from URLs named in the
  graph, and a hand-edited graph can make the CLI fetch and convert. The user
  chooses the converters. The skill has the model advise on the config and
  confirm with the user before writing it.

## 6. Drift

- Every read that resolves text (`check`, `ask --src-text`, `show`, `report`)
  recomputes the hash. On mismatch the text is still printed, preceded by a
  `DRIFT <cite>` line.
- `tm check` adds one rubric line: if `DRIFT` is present, grade `unclear`.
  Questions are immutable (decision 21), so a rotted question resolves
  `unclear` and its `--re` replacement carries a fresh citation.
- `tm lint` stays a static structure check: syntax and hash presence only,
  no resolution, no fetch. `tm lint --drift` resolves local citations and
  lists mismatches, one per line, exit 1 if any; `--remote` adds fetched ones.
  Both are opt-in so a rotted source never blocks mutations.
- `grade` events keep `src_text`; that is the record of what was judged. `add`
  and `q` events do not copy text: nothing has been judged against a node at
  that point, and the hash covers detection.
- Errata never edit a node. When drift invalidates a pass, `tm reopen
  <concept> "<gap>"`; when it reveals a missing prerequisite, `tm add ...
  --child`. Otherwise the replacement question re-cites.

## 7. Log changes

`add` and `q` events gain, when applicable: `commit` (locator inside a git
repo), `url` (final URL after redirects), `mime`, `converter`,
`converter_version`, `fetched_at`. `rehash` is a new event with `id`,
`before`, `after`. `grade` is unchanged.

## 8. Report

```
tm report <concept> [--hops N] [--fulltext] [--passed-only]
```

Read-only. Markdown on stdout. Teacher-facing: it exists to give a teacher, in
particular a fresh one resuming a session, the learner's foundations for a
concept without any model-authored summary. It is never handed to a grader;
the grader surface stays `tm check`.

- Walk parent edges from the concept up to `N` hops (default unbounded for
  outline, 2 for `--fulltext`). Emit in topological order, roots first.
- Per concept: heading with id and scope, state, `GAP` if set, grader
  summaries of failed probes, and a footnote reference per citation.
- Outline mode: no source text. Footnotes list every distinct citation once at
  the end as `[^n]: <cite>`, rendered as a link when the locator is a URI.
- `--fulltext`: the cited text is inlined under the concept in a fenced block.
  A citation already inlined earlier links back to that heading's anchor
  instead of repeating.
- `--passed-only` drops open and blocked concepts.
- A learner-facing study guide is the same walk over the passed block with
  question text omitted. Deferred (section 12).

## 9. Skill changes

A **Source** step before Orient in `SKILL.md`:

- Ask the learner for materials first: notes, textbook chapters, docs, a repo,
  papers. Markdown or any line-addressable text. Set `TM_SRC_ROOT`.
- A repo: set `TM_SRC_ROOT` to its root or cite absolute paths. When the graph
  must be portable across machines, cite the remote URL at a commit instead.
- Web sources: prefer immutable or versioned URLs (versioned arXiv, tagged
  docs, permalinks at a commit, archive snapshots). arXiv HTML or e-print over
  PDF. Dynamic pages: save a static copy and cite it.
- No egress: local-only, or ask the learner for citable documents. Not a
  blocker.
- Converter setup: name pandoc and `pdftotext` as suggested defaults with
  pinned versions, give the config location and lines, run a test conversion,
  and confirm with the user before writing the config.
- The no-memory rule, stated as a rule: never author source text from memory;
  if no real source can be obtained, say so and stop.
- `tm new` for a fresh session. `tm report` when resuming and before a
  teaching round.

Grader adapter: one line, `DRIFT` means `unclear`.

## 10. Spec revision checklist (M8)

| Section | Change |
|---|---|
| 2.1 | narrow "runs no external command" per section 5 |
| 3 | user config file and `.tmconfig` override |
| 4.4 | citation field grammar (section 3) |
| 6 | `report`, `rehash`, `lint --drift`; `check` prints `DRIFT` |
| 7 | refusals: fetch failed, no converter, version mismatch, not in working tree |
| 9 | `DRIFT` line and rubric sentence |
| 10 | new log fields and `rehash` event |
| 11 | hash presence; `"` in locator; `--drift` |
| 12 | Source step; no-memory rule as the third unenforceable behavior |
| 13 | config file variables |
| 14 | decision rows below |
| 15 | move resolved items out; add section 12 items |
| 16 | layout: `internal/source/` (resolve, fetch, convert, git), `internal/report/`; decision 39 clarified |

Draft decision rows:

| # | Decision | Reason | Rejected |
|---|---|---|---|
| 46 | Citations carry a content hash, hash first: `<hash>@<locator>:START-END` | drift detection on every read with no side state; the hash travels wherever the citation is printed; reads like a git revision | meta line per citation; hash in the log only |
| 47 | One locator grammar: relative path, absolute path, URI | three source kinds with one parser and one document; the kind is a prefix, not a syntax | sister lookup file of selectors; per-kind citation forms; XPath |
| 48 | Sources converted by user-configured external converters keyed by MIME and extension, pinned by version and checked at runtime | deterministic per version; zero CLI dependencies; user-extensible to any format | built-in tag stripper; pure-Go HTML library; runtime-loaded modules (Go has none) |
| 49 | Spec 2.1 narrowed: no external command except configured converters and git | default behavior unchanged; the rule's purpose was harness independence, which a content-only filter keeps | fetch and conversion inside the CLI with dependencies; adapter-side conversion only |
| 50 | Drift derived at read time; `check` prints `DRIFT` and the rubric says `unclear`; nodes never edited | immutability (21); a rotted question is replaced, not repaired | brittle flag on the node; automatic re-citation |
| 51 | `tm report` walks foundations: outline by default, `--fulltext` bounded by hops | requirement 1 with no model-authored intermediate text | derived study docs verified by a judge agent; anthologies of verbatim passages |
| 52 | No-memory rule in the teacher adapter | the citation machinery is defeated silently by a notes file written from memory; the rule is the one thing the model will not enforce on itself | trust the model to source honestly |
| 53 | `grade` keeps `src_text`; `add` and `q` do not copy text | audit needs what was judged; the hash covers detection; bounded log growth | copy every citation on write; filesystem compression |
| 54 | Git read through the configured command; commit recorded by reading refs directly | packfile parsing is real work; unreachable commits can be garbage-collected, so the commit is provenance, not the verification mechanism | pure-Go object reader; go-git; commit hash in the citation |

## 11. Trade-offs accepted

- Line ranges into a converted page shift on any edit above the cited spot.
  The hash catches it and the fix is to re-cite. Versioned URLs avoid it.
- A converter upgrade is indistinguishable from a page change at read time.
  Both are drift with the same fix; the log holds the version that produced
  the original.
- Absolute paths make a graph machine-specific. The skill says when to cite a
  remote URL at a commit instead.
- Non-UTF-8 pages are not handled.
- The CLI cannot see a file that exists only on a git remote.

## 12. Deferred

- Quote and position selectors, and fragment anchoring, for web citations.
- Conversion cache.
- Learner-facing study guide from `report`.
- Remote git fetch.
- `tm lint --remote`.
- Mermaid size limits under long URIs: measure in M9.

## 13. Milestones

| Task | Scope | Blocked by |
|---|---|---|
| M8 | spec revision per section 10 | M7 |
| M9 | citation grammar, hash, normalization, `rehash`, lint rules, golden and corpus regeneration | M8 |
| M10 | source resolution: config file, converters, version check, fetch, git `HEAD` resolution, commit recording, log fields, `DRIFT` in every read | M9 |
| M11 | `tm report` | M10 |
| M12 | skill Source step, converter setup guidance, grader `DRIFT` line | M10, M11 |
| Gate | end-to-end: a session from an empty directory through web and repo citations, drift, and report | M9 to M12 |

M8 is blocked by M7 so the v0.1.0 release is not derailed. The grammar change
is breaking, so the owner decides whether it lands before or after the tag;
removing that dependency moves it before.
