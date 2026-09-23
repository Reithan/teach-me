# teach-me setup reference

Read this file only when advising a user on converter or git configuration, or
when pointing them at the harness permission rules. Do not load it at session
start.

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

**pandoc** (HTML, EPUB, DOCX, and more to plain text):

```
convert text/html = pandoc --from html --to plain
version pandoc = 3.1.11
```

**pdftotext** (PDF to plain text):

```
convert application/pdf = pdftotext - -
version pdftotext = 24.02.0
version-cmd pdftotext = pdftotext -v
ext .pdf = application/pdf
```

The `version-cmd` line is required for pdftotext: it writes its version to stderr
with `-v`, not to stdout with `--version`. Without the override the version check
reads empty output and fails.

**git** (for HEAD blob resolution and commit recording on local repos):

```
git = git
```

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
