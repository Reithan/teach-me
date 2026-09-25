# teach-me setup reference

## Config file

User-level config: `$XDG_CONFIG_HOME/tm/config` (default `~/.config/tm/config`).

Per-directory override: `.tmconfig` in the working directory. Keys set in
`.tmconfig` override the corresponding user-config keys.

Format is key=value, one key per line. Blank lines and lines starting with `#`
are ignored.

## Pointer keys

| Key | Meaning | Who writes it |
|---|---|---|
| `file` | the active graph | `tm new` and `tm load` |
| `src-root` | root for relative citations | `tm new --src-root`, `tm load --src-root` |
| `doc` | the file that documents `tm` for this harness; `tm --help` defers to it | the user, once, at install |

`tm new` and `tm load` write absolute paths into the user config, so the pointer
holds from any working directory and for sub-agents that receive only a question
ID. Pass `--local` to write `.tmconfig` in the working directory instead, for two
lessons on one machine. `TM_FILE`, `TM_SRC_ROOT`, and `TM_DOC` override the
matching key for a single call; they do not persist across calls in an agent
harness, so never rely on them for a session.

**Install line** (`~/.config/tm/config`), pointing at wherever the skill was
installed:

```
doc = /home/<user>/.claude/skills/teach-me/SKILL.md
```

## Converter keys

| Key | Format | Meaning |
|---|---|---|
| `convert <mime>` | `= <command...>` | shell command that reads source bytes on stdin and writes plain text on stdout |
| `ext <ext>` | `= <mime>` | map a file extension to a MIME type for converter lookup |
| `version <program>` | `= <string>` | required version pin; the CLI checks that this string is a substring of the version output before the first use |
| `version-cmd <program>` | `= <command...>` | version command override (default: `<program> --version`) |
| `git` | `= <command>` | git executable; enables HEAD blob resolution and commit recording |

Every `convert` key requires a matching `version` key. The CLI refuses to use a
converter without a pinned version.

## Suggested defaults

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

**pandoc:** `-t gfm` keeps headings intact; `--wrap=none` preserves source
line boundaries so line ranges stay stable.

**pdftotext:** `-layout` preserves column layout. `version-cmd` is required
because pdftotext prints its version to stderr with `-v`, not to stdout with
`--version`. Without the override the version check reads empty output and
refuses every citation.

**Version-mismatch refusal** (example):

```
err: pandoc is 3.2.0, config pins 3.1.11
fix: set version pandoc = 3.2.0 in <config>; citations made under 3.1.11 may drift
```

Update the `version` line to match the installed version. Citations made under
the old version may have drifted; verify them after upgrading.

## Repo names

Register a repo alias for any source that lives in a git repository:

```
tm repo add <name> <path>
```

This writes a `repo` key into machine config (`key = value` format):

```
repo myrepo = /path/to/checkout
```

The alias lives in machine config; the graph stores only the alias, making it
portable across machines. When the graph moves to another machine, register the
same alias name pointing at wherever the repo is checked out there:

```
tm repo add <name> /path/on/other/machine
```

`tm repo rm <name>` removes an alias. `tm repo list` shows all registered
aliases. Pass `--local` to write into `.tmconfig` instead of the user config,
for a per-project override.

The alias must match `[A-Za-z0-9_-]+`; it cannot contain `@` or `:`.

## Cache

The conversion and fetch cache lives in the user cache dir
(`$XDG_CACHE_HOME/tm` on Linux, `~/Library/Caches/tm` on macOS,
`%LocalAppData%\tm` on Windows). It holds converted text keyed by locator,
converter command, and converter version. It is regenerable: deleting it costs
only time.

TTL is controlled by `cache-ttl` in config (Go duration; default `24h`):

```
cache-ttl = 24h
```

`TM_CACHE_TTL` overrides the key for one call. Set to `0` to disable the
cache entirely (every read re-fetches and re-converts).

Useful commands:

- `tm cache list` — show cache entries.
- `tm cache clear` — empty the cache; use this when a learner reports that a
  live page has changed and you want an immediate re-fetch rather than waiting
  for the TTL to expire.

**TTL-late drift trade.** Drift on a live URL is detected at most one TTL
late. Prefer immutable or versioned URLs to avoid the trade entirely.

## Aids

Everything the teacher writes — study guides, generated diffs, summaries,
copies fetched for itself — is an aid, not a source. Aids live in aids-dir:

```
aids-dir = <lesson dir>/aids
```

The default is `<graph dir>/aids`; override the `aids-dir` key in config.

Link an aid to a concept or question with `tm aid <id> <path>`. Aids are
listed by `tm show <id>` and `tm report`.

`tm add`, `tm q`, `tm recite`, and lint refuse a citation that resolves under
aids-dir:

```
err: <path> is an aid, not a source
fix: cite the primary source; link the aid with tm aid <id> <path>
```

`tm src` gives the same `err:` line with only `fix: cite the primary source`
(no concept or question id, since `src` is read-only).

**aids-dir must not sit under src-root.** The deny rule on writes under
src-root (see Harness permissions below) cannot carve an exception for
aids-dir; the two trees must be separate.

## Migrating an older graph

A graph written before format 2 refuses every command (except `migrate`,
`lint`, and help) with `fix: tm migrate`. To upgrade:

```
tm migrate --dry-run [<file>]
```

`<file>` defaults to the configured graph when omitted. Read the output: each
line shows what would be rewritten and what stays. Then:

```
tm migrate [<file>]
```

`tm migrate` rewrites citations in two passes:

1. A plain-path citation whose `add`/`q` event logged a `commit` and whose
   file is inside a registered repo alias is rewritten to the
   `git:<alias>@<commit>:<path>` form.
2. A plain-path citation whose event logged a `url` (a saved copy of a fetched
   page) is rewritten to cite the URL directly.

Citations that neither rule can convert stay as plain paths; they still resolve
as long as the file exists on disk, and carry no git pinning. After migration,
use `tm recite` to re-point any concept whose source is now registered.

## Test conversion

Before writing a converter line to the config, pipe a test file through the
command and confirm the output is readable plain text with sensible line breaks.

## Security note

Converters process untrusted bytes fetched from URLs named in the graph. A
hand-edited graph can make the CLI fetch and convert anything it names. The user
chooses the converters. The skill has the model advise on the config lines and
confirm with the user before writing the config.

## Harness permissions

For a harness with permission rules, deny direct read and write access to the
three graph files so the model cannot bypass the CLI's invariants:

- `**/*.mmd`
- `**/*.mmd.jsonl`
- `**/*.mmd.lock`

Scope the deny rules to the lesson directory.

Also deny writes under src-root so the model cannot author a file there and
cite it as a learner-supplied source. `Read` under src-root stays allowed so the
model can inspect what the learner has placed there. Use the absolute src-root
path for the rule.

**Claude Code example** (`~/.claude/settings.json` or the project's settings file):

```json
{
  "permissions": {
    "deny": [
      "Read(**/*.mmd)",
      "Edit(**/*.mmd)",
      "Read(**/*.mmd.jsonl)",
      "Edit(**/*.mmd.jsonl)",
      "Read(**/*.mmd.lock)",
      "Edit(**/*.mmd.lock)",
      "Edit(/absolute/src-root/**)"
    ]
  },
  "sandbox": {
    "excludedCommands": ["tm"]
  }
}
```

Replace `/absolute/src-root` with the real path. `Edit` rules cover file
creation as well; Claude Code accepts `Write(...)` path rules but never
consults them, so do not add them.

With the bash sandbox enabled, `Read` and `Edit` deny rules are also enforced on
file operations inside shell commands, which would block `tm` itself from its own
files. The `excludedCommands` entry runs `tm` outside the sandbox so it can read
and write the graph while the model's tools and other shell commands still
cannot. `tm src` runs outside the sandbox with the rest of `tm`. Sub-agents
inherit both the deny rules and the sandbox, so the grader and planner are
covered by the same settings.

Without the sandbox, the deny rules cover only the harness's file tools, not
shell reads, which is why the file-access rule in the skill also binds the
model's own behavior directly.

A harness that caps or rewrites tool output should exempt the reader sub-agent
from that cap. `--fulldump` is designed to land the whole converted source in
the reader's context at once, and a truncated dump defeats its purpose. Every
other role — teacher, planner, grader, pruner — reads through the `more:`
window and needs no exemption. The sandbox note above already covers `tm src`
for all sub-agents; no additional `excludedCommands` entry is needed for the
reader beyond the one already in place.

## Session limits

Set these environment variables to override the defaults. Each is read once per
`tm` call; they do not persist across calls in an agent harness.

| Variable | Bounds | Default |
|---|---|---|
| `TM_PROBE_MIN` | minimum questions per probe batch | 2 |
| `TM_PROBE_MAX` | maximum questions per probe batch | 5 |
| `TM_TEACH_MIN` | minimum questions per teaching round | 1 |
| `TM_TEACH_MAX` | maximum questions per teaching round | 3 |
| `TM_MAX_FAILS` | failed probe batches before the gate trips | 2 |
| `TM_MAX_TEACH` | total teach questions per concept before teaching is spent | 8 |
| `TM_MAX_STALL` | consecutive unclear answers before the stall gate trips | 4 |
