# teach-me errata reference

## Drifted ungraded question

A `DRIFT` marker on an ungraded question, or `answer` or a grader refusing with
a drift error: run `tm drop <qid>`, then `tm q --re <qid>` with a fresh
citation, and ask the learner again. Nothing is graded.

Drift on a live URL surfaces at most one cache TTL late. When the learner
reports a page has changed, run `tm cache clear`, then `tm show <concept>`; a
`DRIFT` line marks the change.

## Drifted passed concept

When the current text at a new range hashes the same, run `tm recite`.
Otherwise spawn a `teach-me-grader` with the concept ID and the instruction to
run `tm check --drift`; the grader decides `keep` or `reopen`. Descendants stay
passed either way.

## Wrong detail in a concept's scope

Run `tm errata <id> "<scope>" "<reason>"`. `tm edit` refuses a concept that has
questions or a pass; `tm errata` is your command for it. The reason is logged
and the questions are untouched. When the concept is passed, spawn a
`teach-me-grader` with the concept ID and the instruction to run
`tm check --errata`; the grader decides `keep` or `reopen`.

## Replaced source or missing prerequisite

Read `skill/teach-me/reference/map.md`, then spawn the `teach-me-planner` in
`errata for <concepts>` mode.
