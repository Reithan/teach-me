# teach-me setup reference

## Config file

User-level config: `$XDG_CONFIG_HOME/tm/config` (default `~/.config/tm/config`).

Per-project override: `.tmconfig` in the project directory. Keys set in `.tmconfig`
override the corresponding user-config keys for that project.

Format is key=value, one key per line. Blank lines and lines starting with `#`
are ignored.

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

**Claude Code example** (`.claude/settings.json` or the project's settings file):

```json
{
  "permissions": {
    "deny": [
      "Read(**/*.mmd)",
      "Edit(**/*.mmd)",
      "Write(**/*.mmd)",
      "Read(**/*.mmd.jsonl)",
      "Edit(**/*.mmd.jsonl)",
      "Write(**/*.mmd.jsonl)",
      "Read(**/*.mmd.lock)",
      "Edit(**/*.mmd.lock)",
      "Write(**/*.mmd.lock)"
    ]
  }
}
```

These rules cover the harness's file tools (Read, Edit, Write). They do not cover
shell reads, which is why the file-access rule in the skill also binds the model's
own behavior directly.
