---
name: teach-me
description: "Drive a teaching session with the tm CLI: diagnose what a learner understands over a Mermaid concept graph, probe and grade their answers via grader sub-agents, and teach each diagnosed gap."
metadata:
  tm-version: "0.5"
---

# teach-me skill

## Task

Drive a human learner toward mastery of a body of concepts with the `tm` CLI:
diagnose what they already understand, test the frontier of what they do not, and
teach each gap a wrong answer reveals.

## Goal

Move every in-scope concept into the graph's passed block, each pass earned by
probe answers a grader scored against the source, never by your own read of the
learner. The session is done when `tm status` shows the goal concept passed.

## File-access rule

Read the graph through `tm status`, `tm show`, `tm find`, `tm report`, and
`tm show --history`; write it through a `tm` command. Never read or write
the graph file (`<name>.tm.mmd`, or `<name>.mmd` in older lessons), its
`.jsonl` log, its `.cards.jsonl` cards file, or its `.lock` file by any tool,
including shell reads. Read cards through `tm card show`, `tm show`, and
`tm due`.

On every `tm new`, before the first `tm add`, tell the user to configure harness
deny rules for those four files and for writes under src-root, and point at
`skill/teach-me/reference/setup.md`. Continue once the user confirms or
declines. Declining is allowed; this rule still binds.

If you find you have read or written one of those files, by accident or through
a tool you did not expect to reach them, say so at once, remind the user that
the deny rules are not in place, and point at
`skill/teach-me/reference/setup.md`. Never recover silently; an accidental
write may already have broken a lint invariant.

## Context

`tm` reads and edits one Mermaid flowchart that records what the learner has shown
they understand. That file is the entire state of what the learner has been
taught and tested on; every command re-parses it. Flashcards for remembering it
afterwards live in one cards file beside it. `tm` output is terse and built for
an agent: a bare `ok`, an allocated ID, or a few lines.

Who reads and writes the file:

| Who | Reads | Writes |
|---|---|---|
| You (teacher) | `tm` output, cited source files | every command except `check` and `grade` |
| Grader sub-agent | `tm check` output only | `tm grade` |
| Planner sub-agent | `tm src`, `tm status`, `tm show`, `tm find` | `tm add`, `tm link`, `tm edit`, `tm drop` |
| Pruner sub-agent | `tm status`, `tm show`, `tm find`, `tm report` | `tm prune`, `tm reserve`, `tm activate`, `tm edit` |
| Reader sub-agent | `tm src --fulldump` | nothing |
| Human learner | the rendered graph, your questions, cards in `tmcards` | answers; hand-edits; card reviews and card edits in `tmcards` |

Citations are `hash@locator:START-END`. Relative locators resolve against the
source root recorded by `tm new --src-root` (defaults to the graph's directory).

## Rules

`tm`'s baseline help must point back here: `tm --help` should print `see <path>`
naming this file. If it prints the command list instead, the `doc` key is not
set; advise the user on the line for `~/.config/tm/config` (see
`skill/teach-me/reference/setup.md`) and confirm before writing it. Keep every
session setting in `~/.config/tm/config`; env vars die with each shell call.

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

Write everything you author (study guides, generated diffs, summaries) under
aids-dir (default `<lesson dir>/aids`) and link it with `tm aid <id> <path>`;
never write under src-root and never cite an aid. `tm add`, `tm q`, `tm recite`,
`tm src`, and lint refuse a citation under aids-dir with
`fix: cite the primary source`. A file you write outside aids-dir cannot be
detected by the CLI; the harness deny rule is the only guard. The graph and
`tm show` are the source registry; keep no other.

The cards rule: a card added with `tm card add` states one fact from the
passage it cites, in your wording; add nothing the passage does not say. Import
cards you write from your own knowledge with `--origin teacher` and cards the
learner dictates with `--origin user`, so the review page shows where each came
from. Never cite a card, never quote one to a grader, and never review a card
on the learner's behalf: the learner rates their own recall, and no grader
reads a card review.

**Line ranges come only from `tm src`.** Find a candidate source (search results
and summaries give URLs or paths, never line numbers); run `tm src <locator>`
to see the converted, numbered text; narrow with `--find <regex>` or
`START-END`; cite the numbers it printed with `tm add` or `tm q`. Cite promptly:
a cache entry that expires between `tm src` and `tm add` on a changed page
stores the new page's hash. Read and WebFetch may decide whether a source is
worth citing; they never supply a range.

**Windows and the reader.** `tm src` prints at most one window (200 lines or
8,000 characters) and ends with a `more:` line naming the next command; page
with it when a few more windows cover the area you need. When the source is
long and you need the whole of it to locate ranges, spawn `teach-me-reader`
with the locator and one question; it returns a summary and candidate ranges in
`tm`'s numbering. Confirm each range with `tm src <locator> START-END` before
citing. If the reader returns `err:`, fix the locator; if it returns
`unanswered:`, find another source. Only the reader runs `--fulldump`.

## Workflow

`tm status` is your dashboard, and the `fix:` line on any refusal names your next
move. The phases:

1. **Source.** On `tm new` or `tm load`, read
   `skill/teach-me/reference/start.md` and follow it before Orient.

2. **Orient.** Run `tm status`, `tm find`, `tm show` to see what is passed, open,
   blocked, and the frontier (untested concepts whose prerequisites are all
   passed).

3. **Map**, when a fresh map is needed, the frontier is thin, a prerequisite is
   missing, or errata applies. Read `skill/teach-me/reference/map.md` before
   spawning either sub-agent; it is loaded only then. Spawn the
   `teach-me-planner` sub-agent, then spawn the `teach-me-pruner` sub-agent
   after it.

4. **Pick** a frontier concept.

5. **Probe.** Draft at least the minimum batch of narrow probe questions with
   `tm q`, then emit the batch with `tm ask <concept>`; `tm ask` refuses a
   short batch. A question is immutable once written. `tm q` refuses a citation
   over 120 lines or 6,000 characters; narrow with
   `tm src <locator> --find <regex>` or a tighter range. A question needing two
   passages is two questions or a concept drawn too wide.

6. **Answer.** Present the emitted questions through the harness's built-in
   question tool (`tm ask --format json` maps onto it), with an explicit
   "I don't know" choice on every question. Record each answer with
   `tm answer <qid>`, piping raw text via `-`; the first recorded answer locks
   the batch. When the wording shown differs from `Q` in any way, pass the
   verbatim shown text with `--asked "<wording>"`, never a summary: `tm check`
   prints it as `ASKED` and the grader judges scope from it. Record "I don't
   know" with `tm answer <qid> "I don't know" --concede`; that writes the fail
   grade and needs no grader. Any typed answer, however weak, goes to a grader.

7. **Grade.** For every answer that was not conceded, spawn one `teach-me-grader`
   per answer per the grader-isolation rule above, choosing its model by the
   answer's subtlety (sonnet by default, opus when the judgment is fine-grained).
   Then read the verdicts with `tm status --concept <id>`.

8. **Act on the verdict** (`tm` runs the transitions):
   - all pass → the concept passes automatically; its tests clear and it moves to
     the passed block.
   - no fail, some unclear → add one `tm q --re <qid>` replacement per unclear
     question, then `tm ask` again.
   - any fail → open a teaching round: run `tm report <concept> --fulltext` so
     the teach questions build on the passed foundations (each concept's inlined
     text is bounded to one window; a cut concept ends with a `more:` trailer
     naming the next `tm src` command), record the gap with `tm gap`, then teach
     with `tm q --teach --re <qid>`. Once the teach batch resolves all pass,
     the locked fallback probes become answerable. When `q --teach` refuses
     because teaching is spent, ask the locked fallback probes.
   - gated → `q`, `ask`, and `answer` refuse on the concept. Take one exit
     now, before touching another concept: read
     `skill/teach-me/reference/gate.md` and follow it. Use `--override` only
     when the learner asks, never on your own read of the verdicts; that is
     the bias grader isolation exists to block.
   - learner asks to skip a concept → `tm reserve <concept>` if it has no
     questions, or `tm reserve <concept> --reason "<why>"` if it has been
     probed and every batch is resolved or a locked fallback probe batch
     with no answers (an open or draft batch must finish first). It stops
     blocking its children and can be activated later with its round intact.

9. Repeat from step 4 until `tm status` shows the goal concept passed.

## Flashcards

Cards help the learner keep what they passed. They never change the graph, and
a missed card never fails a concept.

**Make cards.**

- After a concept passes, offer to make cards for it. If the learner agrees,
  write each with `tm card add <concept> <cite> "<front>" "<back>"`, one fact
  per card, favoring the facts the learner missed on the way. Take the range
  for `<cite>` from `tm src`, as for any citation.
- When a source comes with a list (vocabulary, command flags, dates), import it
  during Map with `tm card import <locator> --concept <id>`. The file is
  tab-separated text, one card per line: front, a tab, back. Cards tied to a
  concept wait until that concept passes.
- For loose facts that belong to no concept, use `tm card import <path>` with
  `--origin user` for cards the learner dictates and `--origin teacher` for
  cards you write. `<path>` may be `-` for stdin.

**Review.** When `tm status` shows `cards due <n>`, tell the learner and offer
a review. The learner reviews in `tmcards`, a second binary that costs no
tokens:

- Start `tmcards serve` in the background. It prints one line, the page URL.
  Give the learner that URL unchanged; the token in it is the key, so never
  shorten it, and never write it to a file. The server stops by itself after
  30 minutes without use (`--idle <duration>` changes that), and every start
  prints a new URL.
- When `tmcards` runs in a container whose port is forwarded to the learner's
  machine, run `tmcards serve --bind 0.0.0.0 --port <n> --public localhost:<n>`
  so the printed URL is the one the learner's browser can open.
- A learner at a terminal can run `tmcards review` themselves.
- When no browser can reach the machine, review through the harness's question
  tool. `tm due --format json` prints up to four due cards, each with its
  front and answer options in an order the CLI shuffled. Show each front with
  its options, then run `tm review <n> --pick "<chosen text>"`; it prints
  `right` or `wrong | <back>`, and you never judge the pick. A card printed
  with no options has no other card to draw them from: show its front, then
  its back from `tm card show <n>`, and record the learner's own rating with
  `tm review <n> again|hard|good|easy`.

**Leeches.** A line `<concept>  passed  leech <m>/<n> cards` in `tm status`
means the learner keeps missing those cards. Ask the learner which it is: the
concept needs teaching again, or a card is badly worded. Run `tm reopen` only
when the learner says the concept is lost; its cards then wait until it passes
again. Reword a bad card with `tm card edit <n> "<front>" "<back>"` once the
learner agrees, or let them edit it on the review page; the card keeps its
schedule. `tm card rm <n>` removes one.

## Errata

On a `DRIFT` marker, a drift refusal, a wrong detail in a concept's scope, or a
replaced source, read `skill/teach-me/reference/errata.md` and follow it. You
never decide whether a pass survives; a grader does.

- **Disputed verdict**: not errata. Re-probe with `--re`; the grader decides.
- **After any hand edit to the graph**: run `tm lint`.

## Reporting problems

Report any issue with `tm` or this skill, mechanical or procedural: a crash,
a refusal whose `fix:` does not work, output that contradicts these
instructions, a workflow step that dead-ends or does not fit the case in front
of you, a lesson plan that stalls or loops, a grader or planner verdict that
looks wrong, or a user unhappy with how the session is unfolding. Open an issue
at https://github.com/Reithan/teach-me/issues if your user, project, harness,
and environment all allow it; otherwise suggest the user open one. Describe
what happened, what you expected, and the `tm --version` output, with the
commands and output involved; leave out learner answers and private source
text.

## Command reference

Generated from the CLI; for one command or flag, run `tm <command> --help`:

!`tm --help --all`
