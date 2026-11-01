# Snippets

Type a keyword anywhere and it is replaced by its snippet's text, with `{...}`
placeholders expanded.

## Files

Snippets live in `.md` files under `~/.config/burin/snippets/`, read recursively in
lexical order. A file may hold many snippets. Each `## ` heading starts one: the whole
rest of the heading line, trimmed, is the keyword, and the lines up to the next `## `
heading are the content, without leading and trailing blank lines. Any text before the
first heading is a comment. A content line starting with `\## ` stands for one starting
with `## `.

```markdown
## ;date
{datetime("%Y-%m-%d")}

## =b64
{clipboard|b64}
```

A keyword defined again, in the same file or a later one, replaces the earlier one. A
file that cannot be read, or is not valid UTF-8, or has an empty heading, is skipped and
counted as an error in the tray. The directory is watched, and the tray's Reload reloads
it too.

## Expansion

Burin keeps the text typed since the last reset, up to 128 characters. A modified key
(Command, Control or Option), Return, Tab, Escape, forward delete and the arrow keys
reset it; Delete removes its last character. When the text ends with a keyword, the
longest such keyword expands once typing pauses for 200ms, or at once when a space
follows it. The keyword is deleted with backspaces, the content is expanded, and the
result is pasted. A space that triggered the expansion is put back after the result
unless the result is empty or already ends with a space. If expansion panics, the
keyword is pasted back.

## Placeholders

`{name}`, `{name(arg, ...)}` and pipelines `{a|b|c}` are replaced by their output; each
stage gets the output of the one before as its input. Arguments are separated by commas
and may be quoted with double quotes (`\"` inside quotes is a quote), which a comma inside
an argument needs. `{{` stands for a literal `{`. A placeholder that fails to
parse or whose pipeline fails is kept as written and the failure is announced.

Built-ins:

- `date` is today's date, `%Y-%m-%d`.
- `datetime` is the current time, `%Y-%m-%d %H:%M:%S`, or in the strftime format given
  as its argument: `{datetime("%H:%M")}`. The directives are `%Y %m %d %H %M %S %y %b %a
  %j %Z %z`, and `%%` for `%`.
- `clipboard` is the text on the clipboard.
- `unwrap` strips the common indent of its input and joins soft-wrapped lines:
  `{clipboard|unwrap}`.
- `shell(command)` runs the command with `sh -c`, its input on stdin. The whole argument
  is one command line, commas and pipes included; it is unquoted only when it is a single
  quoted string.
- `null` outputs nothing: `{shell(open -a Kitty)|null}`.

Leader entries and mouse gestures also get `reload`, `open`, `toggle` and `key`; see
[leaders](leaders.md).

## Scripts

Any other name runs a script from `~/.config/burin/scripts/`: the file named exactly
like the placeholder, or else the first in sorted order named `<name>.<extension>`
(`b64.py` for `b64`). It must be executable (`chmod +x`). Its arguments are the
placeholder's, its stdin is the input of the stage, and its stdout, without trailing
newlines, is the output. Commands and scripts run with the PATH of your login shell and
time out after 3 seconds; a failure reports its stderr, and stderr of a script that
succeeds is announced too.

The example `=b64` snippet pipes the clipboard through `scripts/b64.py`, which decodes
its input if it looks like base64 text and encodes it otherwise:

```markdown
## =b64
{clipboard|b64}
```
