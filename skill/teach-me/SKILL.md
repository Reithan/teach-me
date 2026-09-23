---
name: teach-me
description: "Drive a teaching session with the tm CLI: diagnose what a learner understands over a Mermaid concept graph, probe and grade their answers via grader sub-agents, and teach each diagnosed gap."
metadata:
  tm-version: "0.1"
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
| Human learner | the rendered graph, your questions | answers; hand-edits |

Citations are `file:START-END`, resolved against `$TM_SRC_ROOT` (defaults to the
graph's directory).

## Rules

Set `TM_DOC` to this file's path at session start, so `tm`'s baseline help points
back here.

Delegate every verdict to a grader sub-agent; never grade an answer yourself. The
grader's isolation is what keeps your pass-bias out of the score, so spawn one
`teach-me-grader` sub-agent per answer and put in its prompt only the question ID
and the instruction to grade it. `tm check` inlines the cited source and the raw
answer for the grader, so it needs no file access; never pass the learner's other
answers, the teaching history, or your read of their comprehension.

When a failed probe opens a teaching round, target your teach questions at the
misunderstanding recorded in the concept's `GAP` field; never re-ask the literal
scope of the locked probe batch.

## Workflow

`tm status` is your dashboard, and the `fix:` line on any refusal names your next
move. The full state machine is spec §12; the phases:

1. **Orient.** Run `tm load <file>`, then `tm status`, `tm find`, `tm show` to see
   what is passed, open, blocked, and the frontier (untested concepts whose
   prerequisites are all passed).
2. **Map**, when the frontier is thin or a prerequisite is missing. Use `tm add`
   and `tm link` to fill in concepts; `tm edit` and `tm drop` touch only concepts
   that have no questions yet.
3. **Pick** a frontier concept.
4. **Probe.** Draft between `TM_PROBE_MIN` and `TM_PROBE_MAX` narrow probe
   questions with `tm q`, then emit the batch with `tm ask <concept>`. A question
   is immutable once written.
5. **Answer.** Present the emitted questions to the learner through the harness's
   built-in question tool (`tm ask --format json` maps onto it), then record each
   answer with `tm answer <qid>`, piping raw text via `-`. The first recorded
   answer locks the batch.
6. **Grade.** Spawn one `teach-me-grader` per answer per the grader-isolation
   rule above, choosing its model by the answer's subtlety (sonnet by default,
   opus when the judgment is fine-grained). Then read the verdicts with
   `tm status --concept <id>`.
7. **Act on the verdict** (`tm` runs the transitions of spec §8):
   - all pass → the concept passes automatically; its tests clear and it moves to
     the passed block.
   - no fail, some unclear → add one `tm q --re <qid>` replacement per unclear
     question, then `tm ask` again.
   - any fail → open a teaching round: record the gap with `tm gap`, then teach
     with `tm q --teach --re <qid>`. Once the teach batch resolves all pass, the
     locked fallback probes become answerable.
8. Repeat from step 3 until `untested` holds no concept you intend to test.

## Command reference

Generated from the CLI; for one command or flag, run `tm <command> --help`:

!`env -u TM_DOC tm --help`

## Reference

Read `docs/spec.md` for full command semantics, invariants, and edge cases. Key
sections: §8 transitions, §9 grader protocol, §5 derived state, and §13
configuration (batch sizes and gate limits: `TM_PROBE_MIN`/`MAX`,
`TM_TEACH_MIN`/`MAX`, `TM_MAX_FAILS`, `TM_MAX_TEACH`, `TM_MAX_STALL`).
