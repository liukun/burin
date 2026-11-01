# Leader keys

Hold a trigger key to show a HUD at the top of the current display listing its entries; press an
entry's key while holding to run it. Release the trigger to hide the HUD. One hold can
run several entries, and every key pressed while holding is swallowed.

Each file in `~/.config/burin/leaders/` is one leader, named after its trigger key:
`cmd_l cmd_r opt_l opt_r ctrl_l ctrl_r shift_l shift_r fn f1`…`f20` and the keypad
operators `kp_minus kp_plus kp_multiply kp_divide kp_enter kp_clear`, and `tab` (e.g.
`cmd_r.md`). The format is the snippet format with `## <key> <title>` headings; text before
the first heading is a comment, except `:hold:` and `:layout:` lines:

```markdown
## k Kitty
{shell(open -a kitty)|null}
## l Activity Monitor
{shell(open -a "Activity Monitor")|null}
## d Copy today's date
{date}
## r Reload
{reload}
```

Entry keys are `a`–`z`, `0`–`9`, the ANSI punctuation keys, `space`, `tab`, `return`
and `f1`…`f20`, matched by physical (ANSI) position. The content is expanded like a
snippet's, with extra built-ins that snippets do not get:

- `{reload}` reloads snippets, leaders, mouse gestures and the rules files.
- `{open(App)}` brings the app to the front, launching it if it is not running.
- `{toggle(App)}` hides the app if it is frontmost, brings it to the front if it is running
  or hidden, and launches it otherwise.
- `{key(cmd+[)}` presses a key combination: modifiers `cmd ctrl opt` (or `alt`) `shift fn`
  joined with `+` to one key name, case-insensitive. Key names are the entry keys below
  plus `left right up down escape delete home end pageup pagedown`. A combination with a
  comma must be quoted: `{key("cmd+,")}`.

`App` is the app's name as the Dock shows it (`{toggle(Google Chrome)}`) or, for a running
app, its bundle ID. A non-empty result is copied to the clipboard and announced; an empty
one (`|null`) does nothing more. The HUD marks a running entry `●`, a finished one `✓` (for a second) and a failed one
`✗`. A file's own trigger key cannot be one of its entries.

A `:hold: <duration>` line before the first heading (`:hold: 200ms`, `:hold: 0.2s`) puts an
ordinary trigger key in hold mode, so it keeps its usual function: a tap types the key as
usual, and only holding it for the duration shows the HUD. Pressing another key before
then types the trigger key followed by that key, and pressing the trigger together with
Shift, Control, Option or Command (`cmd+tab`, `shift+tab`) leaves it alone. Without
`:hold:`, the trigger key is swallowed — a `tab.md` without it disables Tab everywhere.
An invalid `:hold:` value is shown as an error in the HUD and holds for 200ms; `:hold:` in
a modifier's file is an error, as modifiers keep their function anyway.

```markdown
:hold: 200ms
## k Kitty
{shell(open -a kitty)|null}
```

A `:layout:` line before the first heading sets how the HUD arranges the entries. `:layout: list`,
the default, lists them top to bottom in columns of up to 12. `:layout: rows` puts them left to
right in rows, in file order; an entry whose content starts with a `:row: break` line starts a
new row (the line is not part of the content, and a `:row:` line further down is). A row does
not wrap, so the HUD is as wide as the widest row. Any other `:layout:` value, a second
`:layout:` line, a `:row:` value other than `break`, or `:row:` without `:layout: rows` is an
error shown in the HUD.

```markdown
:layout: rows
## 1 Chrome
{shell(open -a "Google Chrome")|null}
## 2 Yab
{shell(open -a "Yab")|null}
## q Moshi
:row: break
{shell(open -a "Moshi")|null}
```

Files that name no trigger key are skipped with a notification; a file that fails to
parse shows its error in the HUD. With no leader files, trigger keys behave as usual.
The directory is watched, and the tray's Reload reloads it too.
