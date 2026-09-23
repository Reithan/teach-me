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
- Questions are immutable and verdict-bearing history lives in the log. Drift
  is derived at read time, never stored. A concept citation may be updated on
  drift through logged paths (section 6); a drifted ungraded question leaves
  the graph only through a CLI-verified drop.
- The no-memory rule: model knowledge drafts questions and explains during
  teaching. It never becomes source. When no real source can be obtained, the
  teacher says so and stops.
- The model reaches the graph and the log only through the CLI. The harness
  is configured to deny the model direct reads and writes of `<name>.mmd`,
  `<name>.mmd.jsonl`, and `<name>.mmd.lock`; the adapters tell the model not
  to touch them and to ask the user for that configuration on every `tm new`.

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

- Every read that resolves text (`ask --src-text`, `show`, `report`)
  recomputes the hash. On mismatch the text is still printed, preceded by a
  `DRIFT <cite>` line.
- **Drifted ungraded questions are dropped, not graded.** A question whose
  citation no longer hashes cannot be answered or graded fairly: grading it
  `unclear` wastes a learner answer, and a second `unclear` becomes `fail`
  under decision 7, which would punish the learner for a source edit.
  `tm answer` and `tm check` refuse a drifted question with `fix: tm drop
  <qid>, then tm q --re <qid> <cite>`. `tm drop <qid>` is accepted only when
  the CLI verifies the hash mismatch, so the teacher cannot drop questions at
  will and decision 21 keeps its force: answering, or CLI-verified drift, are
  the only ways a question leaves the graph. The `drop` event carries the
  full node, the recorded answer if any, and `reason: drift`. For batch
  accounting a dropped question is treated like an `unclear` one: exactly one
  `--re` replacement, with none of the verdict consequences (no failed count,
  no second-`unclear` rule). Graded questions are never touched; their
  verdicts were recorded against the text in the log.
- `tm lint` stays a static structure check: syntax and hash presence only,
  no resolution, no fetch. `tm lint --drift` resolves local citations and
  lists mismatches, one per line, exit 1 if any; `--remote` adds fetched ones.
  Both are opt-in so a rotted source never blocks mutations.
- `grade` events keep `src_text`; that is the record of what was judged. `add`
  and `q` events do not copy text: nothing has been judged against a node at
  that point, and the hash covers detection.
- **Concept citations may be updated on drift; questions never.** No verdict
  is graded against a concept's citation: grading uses the question's
  citation, questions are immutable, and each grade event stores the text
  the grader saw. The concept citation is a foundation pointer for `add`,
  `show`, and `report`, so updating it cannot rewrite what a pass was earned
  against. The graph is already not add-only (`reopen` moves nodes, `gc`
  removes them); the protected set is questions and the log. Errata nodes
  were rejected: a replacement node needs the old node's edges copied and
  the old node marked superseded, which is more mutation spread wider, for
  the same effect as `reopen`. Three logged paths:
  - `tm recite <concept> <locator>:START-END`: re-point, any state, accepted
    only if the new range hashes to the existing hash. Covers line shifts,
    moved files, converter reflows. Mechanical; keeps the pass.
  - `tm reopen <concept> "<gap>" [--src <cite>]`: content changed and the
    pass is invalid; the one command that already invalidates a pass also
    re-points the foundation.
  - **Recheck, judged by a grader.** Content changed on a passed concept and
    it is unknown whether the pass survives. The teacher spawns a grader
    with the concept ID only. `tm check --drift <concept>` reads the
    concept's `grade` events from the log, resolves each logged question
    citation against the current source, and prints per question: `Q`, the
    text graded (`src_text` from the log), the current text at that
    citation, the raw answer, and the recorded verdict. Rubric: `keep` if
    every answer still holds against the current text; `reopen` otherwise or
    when it cannot tell. `tm grade --drift <concept> keep|reopen
    "<summary>"`: `keep` re-hashes the concept citation to the current text
    and logs it; `reopen` runs `reopen` with the summary as the GAP. The
    teacher never makes this call: its bias runs toward re-testing always or
    never, depending on the model. This is the CLI's second log read, beside
    `show --history`; a missing log refuses with `fix: tm reopen`.
  - `tm edit --src` on question-less concepts is unchanged.
- A missing prerequisite revealed by drift or by a learner correction is
  mapping, not errata: `tm add ... --child` or the planner in errata mode.

## 7. Log changes

`add` and `q` events gain, when applicable: `commit` (locator inside a git
repo), `url` (final URL after redirects), `mime`, `converter`,
`converter_version`, `fetched_at`. `rehash` is a new event with `id`,
`before`, `after`; `recite` the same. `reopen` gains `src_before` and
`src_after` when `--src` is given. `recheck` carries `concept`, `verdict`
(`keep`, `reopen`), `summary`, and per question `q`, `src_text_before`,
`src_text_after`. `grade` is unchanged.

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

## 9. Adapter changes

Today two adapters exist: the teacher skill, which drives the session and
administers questions, and the grader agent, which scores one answer in
isolation. Planning a lesson, meaning reading the corpus and decomposing the
topic into a prerequisite graph with citations, is one sentence in the
teacher's Map phase, and errata handling does not exist. Both become
explicit, and planning becomes a third adapter, because it reads far more
source than the teacher should carry in context and its output is a small
set of `tm` mutations the teacher can review through `tm status`. The three
roles: the planner decomposes, the teacher tests and teaches, the grader
arbitrates.

| Adapter | File | Job |
|---|---|---|
| Teacher | `skill/teach-me/SKILL.md` | drive the session; source acquisition; delegate mapping and grading; errata |
| Grader | `skill/teach-me/agents/teach-me-grader.md` | grade one answer in isolation |
| Planner | `skill/teach-me/agents/teach-me-planner.md` | read sources, write concepts and edges with citations |
| Setup reference | `skill/teach-me/reference/setup.md` | converter and git configuration the teacher reads only when advising the user |

### 9.1 Teacher skill

A **Source** step before Orient:

- Ask the learner for materials first: notes, textbook chapters, docs, a repo,
  papers. Markdown or any line-addressable text. Set `TM_SRC_ROOT`.
- A repo: set `TM_SRC_ROOT` to its root or cite absolute paths. When the graph
  must be portable across machines, cite the remote URL at a commit instead.
- Web sources: prefer immutable or versioned URLs (versioned arXiv, tagged
  docs, permalinks at a commit, archive snapshots). arXiv HTML or e-print over
  PDF. Dynamic pages: save a static copy and cite it.
- No egress: local-only, or ask the learner for citable documents. Not a
  blocker.
- Converter setup: when a citation refuses for want of a converter, read the
  setup reference, advise the user on the config lines, run a test
  conversion, and confirm with the user before writing the config. The
  reference lives in its own file so the procedure is loaded only when needed.
- The no-memory rule, stated as a rule: never author source text from memory;
  if no real source can be obtained, say so and stop.
- `tm new` for a fresh session. `tm report` when resuming and before a
  teaching round.

A **File access** rule, stated once near the top and repeated at `tm new`:

- The model never reads or writes `<name>.mmd`, `<name>.mmd.jsonl`, or
  `<name>.mmd.lock` directly, by any tool, including shell reads. Every read
  goes through `tm status`, `tm show`, `tm find`, `tm report`, and `tm show
  --history`; every write goes through a `tm` command. This is what makes the
  CLI's invariants (lint on write, immutable questions, grader isolation,
  the drop and recheck paths) hold: a model that can open the graph can
  bypass all of them without noticing.
- On every `tm new`, before the first `tm add`, the teacher tells the user to
  configure the harness to deny the model direct access to those three files,
  points at the setup reference for the exact rules, and continues once the
  user confirms or declines. Declining is allowed; the rule still binds the
  model.
- If the model ever finds that it has read or written one of those files, by
  accident or by a tool it did not expect to reach them, it says so
  immediately, reminds the user that the deny rules are not in place, and
  points at the setup reference again. Silent recovery is not an option,
  because an accidental write may already have broken a lint invariant.

The setup reference carries the harness section for this: for a harness with
permission rules, deny entries for read, edit, and write on `**/*.mmd`,
`**/*.mmd.jsonl`, and `**/*.mmd.lock`, scoped to the lesson directory. The
reference notes that such rules cover the harness's file tools; a shell
`cat` is not covered, which is why the rule in the skill also binds the
model's own behavior.

The **Map** phase delegates to the planner. The teacher's spawn prompt carries
the learning goal as the learner stated it, the source locations, and the
scope of the request: initial map, extension around a named concept when the
frontier is thin, or errata against named concepts. For an extension or
errata request the teacher also passes the output of `tm report <concept>`
so the planner sees the existing foundations without reading the graph
itself. The planner returns a one-paragraph summary; the teacher reads the
result through `tm status` and `tm show`, never through the planner's prose.

An **Errata** section, since the teacher is who sees drift and disputes:

- `DRIFT` on an ungraded question, or an `answer` or grader refusal naming
  one: `tm drop <qid>`, then `tm q --re <qid>` with a fresh citation. The
  learner is asked again; nothing is graded.
- `DRIFT` on a passed concept: if the current text hashes the same at a new
  range, `tm recite`. Otherwise spawn a grader with the concept ID and the
  instruction to recheck it; the grader decides `keep` or `reopen` from the
  logged answers (section 6). The teacher never decides whether a pass
  survives a source change. Descendants stay passed either way.
- A source replaced or a learner correction that shows a missing
  prerequisite: spawn the planner in errata mode for the affected concepts. It
  may `tm add --child`, `tm link`, and `tm edit` or `tm drop` question-less
  concepts; it never touches a concept with questions, which is the CLI's
  rule already.
- A learner who disputes a verdict: not errata. Re-probe with `--re`; the
  grader decides.
- After any hand edit to the graph, `tm lint`.

### 9.2 Grader agent

Two additions. When `tm check` refuses because the question drifted, report
the refusal and stop; the teacher drops and replaces it. A recheck prompt
carries a concept ID instead of a question ID: run `tm check --drift
<concept>`, judge from the printed pairs of graded and current text alone,
then `tm grade --drift <concept> keep|reopen "<summary>"`. Tool scoping is
unchanged, since both are `tm check` and `tm grade`. The grader has no file
tools, so the file-access rule (9.1) needs no restating there.

### 9.3 Planner agent

`skill/teach-me/agents/teach-me-planner.md`, static body like the grader's.

- **Task.** Turn a body of source into concepts the teacher can probe: one
  `tm add` per concept with a scope and a citation, `tm link` for
  prerequisite edges, and nothing else. It writes the graph only through
  `tm`; it never writes files under the source root.
- **Input.** The spawn prompt carries the learning goal, the source
  locations, the request scope (initial, extend around `<concept>`, errata
  for `<concepts>`), and for the latter two the `tm report` output.
- **Rules.** The no-memory rule: every concept cites source the planner has
  read in this session. A scope is probe-sized, meaning two to five narrow
  probes can test it; anything larger is split. A prerequisite edge is added
  only where passing the parent is genuinely required to answer probes on the
  child, since every edge blocks the frontier. Map to a bounded depth around
  the goal, not the whole corpus; the teacher re-invokes as the frontier
  thins. `tm edit` and `tm drop` only on concepts with no questions. When the
  goal or sources are ambiguous, return the question rather than guess.
- **Tools.** `Bash(tm add *)`, `Bash(tm link *)`, `Bash(tm edit *)`,
  `Bash(tm drop *)`, `Bash(tm find *)`, `Bash(tm show *)`, `Bash(tm status*)`,
  file reading, and the harness's fetch tool when it has one. No `tm q`,
  `tm ask`, `tm answer`, `tm check`, or `tm grade`. The planner has file
  reading for sources, so the file-access rule (9.1) is restated in its body
  and the harness deny rules from the setup reference apply to it too.
- **Model.** Sonnet by default; the teacher picks Opus for dense sources.
- **Completion.** One paragraph: what was added, what was linked, what was
  left unmapped and why, and any question for the learner. Then stop.

`AGENTS.md` gains the planner row in its adapter table.


## 10. Spec revision checklist (M8)

| Section | Change |
|---|---|
| 2.1 | narrow "runs no external command" per section 5; adapter table gains the planner and the setup reference (section 9) |
| 3 | user config file and `.tmconfig` override; the log-read exception gains `check --drift` |
| 4.4 | citation field grammar (section 3) |
| 6 | `report`, `rehash`, `recite`, `reopen --src`, `check --drift`, `grade --drift`, `lint --drift`; `drop` accepts a drifted question; reads print `DRIFT` |
| 7 | refusals: fetch failed, no converter, version mismatch, not in working tree, `recite` hash mismatch, `check --drift` without a log, `answer` and `check` on a drifted question, `drop` on a question that has not drifted or is graded |
| 8 | a dropped question counts as `unclear` for batch accounting: one `--re` replacement, no verdict consequences |
| 9 | the recheck payload and its `keep`/`reopen` rubric |
| 10 | new log fields; `rehash`, `recite`, `recheck` events; `drop` on questions with `reason: drift`; `reopen` source fields |
| 11 | hash presence; `"` in locator; `--drift` |
| 12 | Source step; Map delegates to the planner; errata; no-memory rule and the file-access rule as the third and fourth unenforceable behaviors (the harness may enforce the second; the CLI cannot) |
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
| 50 | Drift derived at read time; a drifted ungraded question is dropped through a CLI-verified `tm drop` and replaced with `--re`; `answer` and `check` refuse it; graded questions untouched | immutability (21) holds because the CLI, not the teacher, decides a question may leave; grading a drifted question `unclear` wastes an answer and can turn into `fail` under 7 | brittle flag on the node; automatic re-citation; grade `unclear` on `DRIFT` |
| 55 | Concept citations may be updated on drift through `recite` (hash-preserving), `reopen --src`, and a grader recheck; each logged with before and after | no verdict is graded against a concept citation, so updating it rewrites nothing a pass was earned against; the graph is already not add-only | errata nodes with edge transfer and a superseded marker; teacher override with a reason |
| 56 | Whether a pass survives a source change is a grader's verdict from the logged answers and the current text, never the teacher's | same isolation argument as grading; the teacher's bias runs toward always or never re-testing | teacher `recite --override`; automatic reopen on any drift |
| 51 | `tm report` walks foundations: outline by default, `--fulltext` bounded by hops | requirement 1 with no model-authored intermediate text | derived study docs verified by a judge agent; anthologies of verbatim passages |
| 52 | No-memory rule in the teacher adapter | the citation machinery is defeated silently by a notes file written from memory; the rule is the one thing the model will not enforce on itself | trust the model to source honestly |
| 53 | `grade` keeps `src_text`; `add` and `q` do not copy text | audit needs what was judged; the hash covers detection; bounded log growth | copy every citation on write; filesystem compression |
| 54 | Git read through the configured command; commit recorded by reading refs directly | packfile parsing is real work; unreachable commits can be garbage-collected, so the commit is provenance, not the verification mechanism | pure-Go object reader; go-git; commit hash in the citation |
| 57 | The model reaches the graph, log, and lock only through the CLI; adapters ask for harness deny rules on `tm new`, tell the model never to touch the files, and require it to report any accidental access as a misconfiguration | every CLI invariant assumes the CLI is the only writer and the grader's isolation assumes the model cannot read verdict history except through `show --history`; a model that opens the files bypasses all of it silently | CLI-side enforcement (impossible: it cannot see who opened a file); trust the model; encrypted or obfuscated graph |

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
| M12 | teacher skill: Source step, planner delegation, errata section, setup reference file; grader `DRIFT` line | M10, M11 |
| M13 | planner agent: `teach-me-planner.md`, tool scoping, `AGENTS.md` adapter row | M11, M12 |
| Gate | end-to-end: a session from an empty directory through mapping, web and repo citations, drift, errata, and report | M9 to M13 |

M8 is blocked by M7 so the v0.1.0 release is not derailed. The grammar change
is breaking, so the owner decides whether it lands before or after the tag;
removing that dependency moves it before.
