# teach-me map reference

Load this file before spawning either the planner or the pruner. It documents
the spawn prompts and the activation rules for the Map step.

## When to map

Trigger the Map step on any of these conditions:

- Fresh session: no concepts are in the graph yet.
- Thin frontier: fewer active, untested concepts than needed to continue probing.
- Missing prerequisite: a gate's `fix:` line names a concept not in the graph.
- Errata: a source has changed or a prerequisite has been revealed as wrong.

## Spawning the planner

Pass the following to the `teach-me-planner` sub-agent:

- The learning goal in the learner's own words.
- What the learner says they already know.
- The source locations (paths or URLs).
- The request scope:
  - `initial` — fresh map from scratch.
  - `extend around <concept>` — fill out thin frontier around that concept.
  - `errata for <concepts>` — revise concepts whose sources or prerequisites
    have changed.
- For `extend` and `errata` scopes, include the output of
  `tm report <concept>` so the planner sees the existing foundations.

Read the planner's results through `tm status` and `tm show`. Ignore its prose
summary.

## Spawning the pruner

After every planner run, pass the following to the `teach-me-pruner` sub-agent:

- The goal concept ID.
- The learning goal in the learner's own words.
- What the learner says they already know.
- The output of `tm report <goal> --hops 99`.
- A budget: active concepts to leave beyond the goal (default: 3). The pruner
  passes `--keep <budget + 1>` to `tm prune`, adding 1 for the goal itself.

Read the pruner's result through `tm status`; the `reserve` count and the
frontier show whether the prune did its job.

## Activation rules

Expect the pruner to park more than you would. That is its job: a parked concept
costs nothing until a probe fails, and `tm activate` restores it in one command,
while an unneeded active concept costs the learner a full probe batch. Do not
activate concepts after a prune on your own read of the map. Activate only from
a gate's `fix:` line, or when the learner asks. If the frontier is empty after a
prune, the goal is blocked by a kept foundation; probe that foundation, do not
un-park others.
