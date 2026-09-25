# teach-me setup reference

## Config file

User config: `$XDG_CONFIG_HOME/tm/config` (default `~/.config/tm/config`).

Per-directory override: `.tmconfig` in the working directory. Keys set in
`.tmconfig` override the matching user-config keys.

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
matching key for a single call only.

### Installing the `doc` key

1. Find where the skill was installed, for example
   `/home/<user>/.claude/skills/teach-me/SKILL.md`.
2. Show the user the line and confirm before writing it to
   `~/.config/tm/config`:

   ```
   doc = /home/<user>/.claude/skills/teach-me/SKILL.md
   ```

3. Run `tm --help`. Done when it prints `see <path>` naming that file.

## Converters

A converter is a shell command that reads source bytes on stdin and writes plain
text on stdout.

| Key | Format | Meaning |
|---|---|---|
| `convert <mime>` | `= <command...>` | converter for a MIME type |
| `ext <ext>` | `= <mime>` | map a file extension to a MIME type for converter lookup |
| `version <program>` | `= <string>` | required pin; before first use, the CLI checks that it is a substring of the first line of the version command's combined stdout and stderr |
| `version-cmd <program>` | `= <command...>` | version command override (default: `<program> --version`) |
| `git` | `= <command>` | git executable; required for any `git:` locator. `HEAD` is not special, and the CLI never contacts a remote |

When a citation refuses for want of a converter:

1. Pick the converter for the MIME type. Suggested commands:

   ```
   convert text/html = pandoc -f html -t gfm --wrap=none
   convert application/pdf = pdftotext -layout - -
   ext html = text/html
   ext pdf = application/pdf
   version-cmd pdftotext = pdftotext -v
   git = git
   ```

   pandoc's `-t gfm` keeps headings intact and `--wrap=none` preserves source
   line boundaries, so line ranges stay stable. pdftotext's `-layout`
   preserves column layout; it documents `-v` as its version flag, hence the
   `version-cmd` line.
2. Run the version command (`<program> --version`, or the `version-cmd`
   override) and read the first line it prints. Pin that line, or a
   version-number substring of it, as `version <program> = <string>`. Never
   copy a version from this file.
3. Pipe a sample of the source through the command and confirm the output is
   readable plain text with sensible line breaks.
4. Converters process untrusted bytes fetched from URLs named in the graph, and
   a hand-edited graph can make the CLI fetch and convert anything it names, so
   the user chooses the converters. Show the user the config lines and confirm
   before writing them.
5. Write the lines to `~/.config/tm/config`.

Done when `tm src <locator>` on the source prints text.

When the installed program changes version, citations refuse:

```
err: pandoc is pandoc 3.2.0, config pins 3.1.11
fix: set version pandoc = pandoc 3.2.0 in <config>; citations made under 3.1.11 may drift
```

Update the `version` line as the `fix:` line says, then check the concepts
cited under the old version for drift with `tm show`.

## Repo names

Register an alias for any source that lives in a git repository:

```
tm repo add <name> <path>
```

This writes a `repo` key into the user config:

```
repo myrepo = /path/to/checkout
```

The graph stores only the alias, so it moves across machines. On another
machine, register the same alias name at wherever the repo is checked out
there. `tm repo rm <name>` removes an alias; `tm repo list` shows them all.
Pass `--local` to write `.tmconfig` instead of the user config.

The alias must match `[A-Za-z0-9_-]+`; it cannot contain `@` or `:`.

## Cache

The conversion and fetch cache lives in the user cache dir
(`$XDG_CACHE_HOME/tm` on Linux, `~/Library/Caches/tm` on macOS,
`%LocalAppData%\tm` on Windows); `TM_CACHE_DIR` overrides the location for one
call. It holds converted text keyed by locator, converter command, and
converter version, and is regenerable: deleting it costs only time.

TTL is the `cache-ttl` key (Go duration; default `24h`). `TM_CACHE_TTL`
overrides it for one call; `0` disables the cache, so every read re-fetches and
re-converts.

`tm cache list` shows entries; `tm cache clear` empties the cache. Drift on a
live URL is detected at most one TTL late; immutable or versioned URLs avoid the
trade.

## Aids

Everything the teacher writes (study guides, generated diffs, summaries,
copies fetched for itself) is an aid, not a source. Aids live in aids-dir,
`<graph dir>/aids` by default; override it with the `aids-dir` key:

```
aids-dir = <lesson dir>/aids
```

Link an aid to a concept or question with `tm aid <id> <path>`. `tm show <id>`
and `tm report` list aids. `tm add`, `tm q`, `tm recite`, and lint refuse a
citation under aids-dir:

```
err: <path> is an aid, not a source
fix: cite the primary source; link the aid with tm aid <id> <path>
```

`tm src` gives the same `err:` line with only `fix: cite the primary source`.

Keep aids-dir outside src-root. The deny rule on writes under src-root (see
Harness permissions) cannot carve an exception for aids-dir.

## Harness permissions

For a harness with permission rules, deny direct read and write access to the
three graph files, scoped to the lesson directory, so the model cannot bypass
the CLI's invariants:

- `**/*.mmd`
- `**/*.mmd.jsonl`
- `**/*.mmd.lock`

Also deny writes under src-root, using its absolute path, so the model cannot
author a file there and cite it as a learner-supplied source. Reads under
src-root stay allowed so the model can inspect what the learner placed there.

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
consults them, so leave them out.

With the bash sandbox enabled, `Read` and `Edit` deny rules also apply to file
operations inside shell commands, which would block `tm` from its own files.
The `excludedCommands` entry runs every `tm` command, `tm src` included,
outside the sandbox, while the model's tools and other shell commands stay
blocked. Sub-agents inherit both the deny rules and the sandbox.

Without the sandbox, the deny rules cover only the harness's file tools, not
shell reads, which is why the file-access rule also binds the model directly.

A harness that caps or rewrites tool output should exempt the reader
sub-agent: `--fulldump` lands the whole converted source in the reader's
context at once, and a truncated dump defeats it. Every other role reads
through the `more:` window and needs no exemption.

## Session limits

`tm` uses these defaults and reads each override from the environment on every
call. A variable set in one shell call dies with it, so to change a limit for a
whole session, set it in the harness's session-wide environment (in Claude
Code, the `env` block of the settings file). Confirm with the user before
writing it.

| Variable | Bounds | Default |
|---|---|---|
| `TM_PROBE_MIN` | minimum questions per probe batch | 2 |
| `TM_PROBE_MAX` | maximum questions per probe batch | 5 |
| `TM_TEACH_MIN` | minimum questions per teach batch | 1 |
| `TM_TEACH_MAX` | maximum questions per teach batch | 3 |
| `TM_MAX_FAILS` | failed probe batches before the gate | 2 |
| `TM_MAX_TEACH` | in-scope teach questions per teaching round before teaching ends and the locked probes are asked | 8 |
| `TM_MAX_STALL` | in-scope teach questions in a row, across zero-pass teach batches, before the gate | 4 |
