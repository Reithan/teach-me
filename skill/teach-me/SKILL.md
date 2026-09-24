---
name: teach-me
description: "Drive a teaching session with the tm CLI: diagnose what a learner understands over a Mermaid concept graph, probe and grade their answers via grader sub-agents, and teach each diagnosed gap."
metadata:
  tm-version: "0.3"
---

# teach-me skill

## Task

Drive a human learner toward mastery of a body of concepts with the `tm` CLI:
diagnose what they already understand, test the frontier of what they do not, and
teach each gap a wrong answer reveals. This file is the teacher adapter of spec
§2.1 (`docs/spec.md`); it carries the procedure the CLI cannot enforce.

## Goal

Move every in-scope concept into the graph's passed block, each pass earned by
probe answers a grader scored against the source, never by your own read of the
learner. The session is done when `untested` holds no concept you intend to test.

## File-access rule

The model never reads or writes `<name>.mmd`, `<name>.mmd.jsonl`, or
`<name>.mmd.lock` by any tool, including shell reads. All reads go through
`tm status`, `tm show`, `tm find`, `tm report`, and `tm show --history`; all
writes go through a `tm` command.

On every `tm new`, before the first `tm add`, tell the user to configure harness
deny rules for those three files and point at
`skill/teach-me/reference/setup.md`. Continue once the user confirms or declines.
Declining is allowed; this rule still binds.

If the model finds it has read or written one of those files — by accident or by a
tool it did not expect to reach them — say so immediately, remind the user that the
deny rules are not in place, and point at `skill/teach-me/reference/setup.md`.
No silent recovery; an accidental write may already have broken a lint invariant.

## Context

`tm` reads and edits one Mermaid flowchart that records what the learner has shown
they understand. That file is the entire session state; every command re-parses
it. Read the graph only through `tm` output, which is terse and built for an agent
(a bare `ok`, an allocated ID, or a few lines); never open the raw `.mmd`.

Three parties share the file:

| Who | Reads | Writes |
|---|---|---|
| You (teacher) | `tm` output, cited source files | every command except `check` and `grade` |
| Grader sub-agent | `tm check` output only | `tm grade` |
| Planner sub-agent | source files, `tm status`, `tm show`, `tm find` | `tm add`, `tm link`, `tm edit`, `tm drop` |
| Pruner sub-agent | `tm status`, `tm show`, `tm find`, `tm report` | `tm prune`, `tm reserve`, `tm activate`, `tm edit` |
| Human learner | the rendered graph, your questions | answers; hand-edits |

Citations are `hash@locator:START-END`. Relative locators resolve against the
source root recorded by `tm new --src-root` (defaults to the graph's directory).

## Rules

`tm`'s baseline help must point back here: `tm --help` should print `see <path>`
naming this file. If it prints the command list instead, the `doc` key is not
set; advise the user on the line for `~/.config/tm/config` (see
`skill/teach-me/reference/setup.md`) and confirm before writing it. Do not rely
on environment variables for anything that must outlast one shell call; the
harness runs each call fresh, and `tm` reads its config file instead.

Delegate every verdict to a grader sub-agent; never grade an answer yourself. The
grader's isolation is what keeps your pass-bias out of the score, so spawn one
`teach-me-grader` sub-agent per answer and put in its prompt only the question ID
and the instruction to grade it. `tm check` inlines the cited source and the raw
answer for the grader, so it needs no file access; never pass the learner's other
answers, the teaching history, or your read of their comprehension.

When a failed probe opens a teaching round, target your teach questions at the
misunderstanding recorded in the concept's `GAP` field; never re-ask the literal
scope of the locked probe batch.

The no-memory rule: every citation must point to text read this session through
the CLI or the harness's file or fetch tools. Never author source text from memory
and never write a notes file from memory and then cite it. If no real source can
be obtained, say so and stop.

## Workflow

`tm status` is your dashboard, and the `fix:` line on any refusal names your next
move. The full state machine is spec §12; the phases:

1. **Source.** On `tm new` (fresh session only):
   - Ask the learner for their learning goal in their own words and what they
     already know. Record both; the planner and pruner need them.
   - Ask the learner for source materials — notes, textbook chapters, docs, a
     repo, papers. Markdown or any line-addressable text. Ask where the lesson
     files should live; the graph goes there, and `tm` records the pointer in
     the user config, so the launch directory does not matter.
   - A repo: pass its root as `--src-root` to `tm new`, or cite absolute paths.
     When the graph must be portable across machines, cite the remote URL at a
     commit instead.
   - Web sources: prefer immutable or versioned URLs (versioned arXiv, tagged
     docs, permalinks at a commit, archive snapshots). arXiv HTML or e-print over
     PDF. For dynamic pages, save a static copy and cite it.
   - No egress: use local sources or ask the learner for citable documents.
     Not a blocker.
   - When a citation refuses for want of a converter: read
     `skill/teach-me/reference/setup.md`, advise the user on the config lines,
     run a test conversion, and confirm with the user before writing the config.
   - Run `tm new <lesson-dir>/<name>.mmd --src-root <dir>`, both as absolute
     paths. Never set `TM_SRC_ROOT` or `TM_FILE` for a session; they do not
     survive to the next call.
   - **File-access rule (repeated).** Before the first `tm add`, tell the user
     to configure harness deny rules for `*.mmd`, `*.mmd.jsonl`, and
     `*.mmd.lock`, and point at `skill/teach-me/reference/setup.md`. Continue
     once the user confirms or declines. Declining is allowed; the rule above
     still binds.

   On `tm load` (resuming): run `tm load <file>`, then `tm report` before
   continuing.

2. **Orient.** Run `tm status`, `tm find`, `tm show` to see what is passed, open,
   blocked, and the frontier (untested concepts whose prerequisites are all
   passed).

3. **Map**, when the frontier is thin or a prerequisite is missing. Spawn the
   `teach-me-planner` sub-agent with: the learning goal as the learner stated it,
   what the learner says they already know, the source locations, and the request
   scope — `initial` for a fresh map, `extend around <concept>` when the frontier
   is thin, or `errata for <concepts>` when sources or prerequisites have changed.
   For extend or errata, also include the output of `tm report <concept>` so the
   planner sees the existing foundations. Read the planner's results through
   `tm status` and `tm show`; ignore its prose summary.

   After every planner run, spawn `teach-me-pruner` with: the goal concept ID,
   the goal in the learner's words, what the learner says they already know, the
   output of `tm report <goal> --hops 99`, and a budget (default: 3 active
   concepts beyond the goal). Read the pruner's result through `tm status`; the
   `reserve` count and the frontier show whether the prune did its job.

4. **Pick** a frontier concept.

5. **Probe.** Draft between `TM_PROBE_MIN` and `TM_PROBE_MAX` narrow probe
   questions with `tm q`, then emit the batch with `tm ask <concept>`. A question
   is immutable once written.

6. **Answer.** Present the emitted questions to the learner through the harness's
   built-in question tool (`tm ask --format json` maps onto it), then record each
   answer with `tm answer <qid>`, piping raw text via `-`. The first recorded
   answer locks the batch.

7. **Grade.** Spawn one `teach-me-grader` per answer per the grader-isolation
   rule above, choosing its model by the answer's subtlety (sonnet by default,
   opus when the judgment is fine-grained). Then read the verdicts with
   `tm status --concept <id>`.

8. **Act on the verdict** (`tm` runs the transitions of spec §8):
   - all pass → the concept passes automatically; its tests clear and it moves to
     the passed block.
   - no fail, some unclear → add one `tm q --re <qid>` replacement per unclear
     question, then `tm ask` again.
   - any fail → open a teaching round: run `tm report <concept>` so the teach
     questions build on the passed foundations, record the gap with `tm gap`,
     then teach with `tm q --teach --re <qid>`. Once the teach batch resolves
     all pass, the locked fallback probes become answerable.
   - gated → follow the `fix:` line in order: (1) `tm activate <parent>` if a
     reserve parent fits the GAP; (2) spawn the planner to add the missing
     foundation with `--child`; (3) `tm reopen <parent>` if a passed parent must
     be retested. `--override "<reason>"` only when the learner says the gate
     tripped on contested verdicts; never use it on your own read of the verdicts,
     since that is the bias grader isolation exists to block.
   - learner asks to skip a concept → `tm reserve <concept>` if it has no
     questions. It stops blocking its children and can be activated later.

9. Repeat from step 4 until `untested` holds no concept you intend to test.

## Errata

Handle drift and disputes as they arise; these are not part of the main probe
cycle.

- **Drifted ungraded question** (`DRIFT` marker on an ungraded question, or
  `answer` or a grader refusing with a drift error): `tm drop <qid>`, then
  `tm q --re <qid>` with a fresh citation. Ask the learner again; nothing is
  graded.
- **Drifted passed concept**: if the current text at a new range hashes the same,
  run `tm recite`. Otherwise spawn a `teach-me-grader` with the concept ID and
  the instruction to recheck it; the grader runs `tm check --drift` and decides
  `keep` or `reopen`. The teacher never decides whether a pass survives a source
  change. Descendants stay passed either way.
- **Replaced source or revealed missing prerequisite**: spawn the
  `teach-me-planner` in `errata for <concepts>` mode.
- **Disputed verdict**: not errata. Re-probe with `--re`; the grader decides.
- **After any hand edit to the graph**: run `tm lint`.

## Command reference

Generated from the CLI; for one command or flag, run `tm <command> --help`:

!`tm --help --all`

## Reference

Read `docs/spec.md` for full command semantics, invariants, and edge cases. Key
sections: §5 derived state (including reserve parents), §8 transitions, §9 grader
protocol, §9.1 recheck payload, §12 intended usage and Prune phase, and §13
configuration (batch sizes and gate limits: `TM_PROBE_MIN`/`MAX`,
`TM_TEACH_MIN`/`MAX`, `TM_MAX_FAILS`, `TM_MAX_TEACH`, `TM_MAX_STALL`).
