# teach-me map reference

## Spawning the planner

Pass the following to the `teach-me-planner` sub-agent:

- The learning goal in the learner's own words.
- What the learner says they already know.
- The source locations: the recorded source root, absolute paths, or URLs.
- The request scope:
  - `initial`: a fresh map from scratch.
  - `extend around <concept>`: fill out a thin frontier around that concept.
  - `errata for <concepts>`: revise concepts whose sources or prerequisites
    have changed.
- For `extend` and `errata`, the output of `tm report <concept>`, one per
  named concept, so the planner sees the existing foundations.
- For an extend from a gate exit: the gated concept, its GAP, and the
  instruction to attach each new foundation with `--child <concept>`.
- The registered repo names (from `tm repo list`), for `git:` locators.
- The aids-dir path, so the planner never cites an aid.
- Any reader summaries and candidate ranges gathered so far, so the planner
  does not re-read long sources.

Read the planner's graph changes through `tm status` and `tm show`. Read its
paragraph for three things only: `QUESTION:` lines (ask the learner), `ERR:`
lines (fix the converter config per `skill/teach-me/reference/setup.md`), and
concepts it found in reserve.

## Spawning the pruner

Spawn the pruner after every planner run while the goal is still untested;
`tm prune` refuses once the goal has questions or has passed, so skip the
pruner then. Pass:

- The goal concept ID.
- The learning goal in the learner's own words.
- What the learner says they already know.
- The output of `tm report <goal> --hops 99`.
- The budget: the number of active foundations to leave beyond the goal. Pass
  3 unless the learner has asked for a wider or narrower session.
- After an extend from a gate exit: the new foundation's ID, and that it stays
  active.

Read the pruner's result through `tm status`; the `reserve` count and the
frontier show whether the prune did its job. When the pruner's paragraph names
a missing foundation, spawn the planner in `extend around <concept>` mode for
it.

## Activation rules

Activate only from a gate's `fix:` line, or when the learner asks. Expect the
pruner to park more than you would. That is its job: a parked concept costs
nothing until a probe fails, and `tm activate` restores it in one command,
while an unneeded active concept costs the learner a full probe batch.

An empty frontier with untested concepts left means an open or gated concept
blocks them. Finish its batch, or take a gate exit per
`skill/teach-me/reference/gate.md`; never un-park others to refill the
frontier.
