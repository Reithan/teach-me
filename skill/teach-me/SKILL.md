---
name: teach-me
description: Teacher agent adapter for the tm CLI. Drives teaching sessions using tm commands.
metadata:
  tm-version: "0.1"
---

# teach-me skill

This skill drives teaching sessions using the `tm` CLI. It is the teacher agent adapter described in section 2.1 of the spec (`docs/spec.md`).

## Unenforceable behaviors (section 12)

Two behaviors the CLI cannot enforce belong here:

1. **Teach questions target the diagnosed gap, not the scopes of the locked probes.** When drafting a teaching round, address the specific misunderstanding identified in the GAP field of the concept, not the literal scope of the failed probe batch.

2. **The grader's spawn prompt carries the question ID and nothing about the user.** When spawning a grader sub-agent, pass only the question ID and the instruction to run `tm check <qid>` then `tm grade`. Do not include any context about the user's prior answers, the teaching history, or your own assessment of their comprehension.

## Usage

See `docs/spec.md` for the full command reference, invariants, and transitions.

Run `tm --help` (or set `TM_DOC` to this file's path) for command usage.
