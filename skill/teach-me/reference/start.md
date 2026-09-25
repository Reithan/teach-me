# teach-me session start

## New session

On `tm new`:

1. Ask the learner for their learning goal in their own words and what they
   already know. Record both; the planner and pruner need them.
2. Ask for source materials: notes, textbook chapters, docs, a repo, papers, in
   Markdown or any line-addressable text. Ask where the lesson files should
   live; the graph goes there, and `tm` records the pointer in the user config,
   so the launch directory does not matter.
3. Run `tm repo add <name> <path>` for every repo the learner names. Repo
   content cites as `git:<name>@<ref>:<path>` (a file at a ref),
   `git:<name>@<sha>` (a commit), or `git:<name>@<a>..<b>[:<path>]` (a diff).
   Refs are pinned to short SHAs on write, so a branch name is fine at cite
   time.
4. Cite web docs by URL. Prefer immutable or versioned URLs: versioned arXiv,
   tagged docs, permalinks at a commit, archive snapshots. Prefer arXiv HTML or
   e-print over PDF.
5. Cite learner-supplied local files (corporate downloads, output of other
   programs or agents) as plain paths under src-root or absolute. Only a copy
   the learner supplies is a plain-path source; a copy you make is an aid, so
   never save one.
6. Without egress, use local sources or ask the learner for citable documents
   and cite the copy as a plain path. This is not a blocker: when a fetch fails
   later, retry once egress is available or ask the learner for a copy.
7. When a citation refuses for want of a converter, read
   `skill/teach-me/reference/setup.md` and follow its converter procedure.
8. Run `tm new <lesson-dir>/<name>.mmd [--src-root <dir>]`, both as absolute
   paths. Pass `--src-root` only when the session includes learner-supplied
   plain files, never a repo root.
9. Before the first `tm add`, give the deny-rule notice per the file-access
   rule.

## Resumed session

On `tm load`:

1. Run `tm load <file>`.
2. If it refuses with `fix: tm migrate`, tell the learner the graph predates
   this `tm`, run `tm migrate --dry-run`, and show the learner what it would
   rewrite and what it leaves. Then run `tm migrate`. A citation left
   unconverted still works as a plain path; it carries no git pinning.
3. Run `tm report`.
