# teach-me gate reference

A gate trips when failed probe batches reach the limit or when teach questions
in a row pass nothing. A gate means the gap is upstream, so the first three
exits each name a parent. The refusal's `fix:` line lists the exits in this
order, with any reserve parents by ID.

## Exits

Take the first one that fits:

1. `tm activate <parent>` when a reserve parent covers the GAP. This is the
   cheapest exit, since the foundation is already mapped and cited.
2. Spawn the planner in `extend around <concept>` mode to add the missing
   foundation with `--child <concept>`, when no mapped parent covers the GAP.
   Read `skill/teach-me/reference/map.md` before spawning.
3. `tm reopen <parent>` when a passed parent is the real gap: the learner passed
   it earlier, but the GAP shows they did not keep it.
4. `--override "<reason>"` on the refused command, when the learner says the
   gate tripped on contested verdicts or on probes outside the concept's scope.

## After the exit

After exits 1 to 3 the concept stays blocked until that parent passes, then
reopens with its old batches discounted. After exit 4 it reopens at once.
Probes left unanswered from before the gate are asked as written; when none
remain, draft a fresh probe batch.
