# Burin（刻字）

macOS menubar automation host written in Go, built on [MyGo](https://github.com/egoist/mygo).
A compiled, single-binary take on the Hammerspoon idea: features are Go packages under
`internal/`, built into one small app. No bundled interpreter, no runtime dependency.

## Leader keys

Hold a trigger key to show a HUD at the top of the current display listing its entries; press an
entry's key while holding to run it. Release the trigger to hide the HUD. One hold can
run several entries, and every key pressed while holding is swallowed.

Each file in `~/.config/burin/leaders/` is one leader, named after its trigger key:
`cmd_l cmd_r opt_l opt_r ctrl_l ctrl_r shift_l shift_r fn f1`…`f20` and the keypad
operators `kp_minus kp_plus kp_multiply kp_divide kp_enter kp_clear`, and `tab` (e.g.
`cmd_r.md`). The format is the snippet format with `## <key> <title>` headings; text before
the first heading is a comment, except `hold:` and `layout:` lines:

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

A `hold: <duration>` line before the first heading (`hold: 200ms`, `hold: 0.2s`) puts an
ordinary trigger key in hold mode, so it keeps its usual function: a tap types the key as
usual, and only holding it for the duration shows the HUD. Pressing another key before
then types the trigger key followed by that key, and pressing the trigger together with
Shift, Control, Option or Command (`cmd+tab`, `shift+tab`) leaves it alone. Without
`hold:`, the trigger key is swallowed — a `tab.md` without it disables Tab everywhere.
An invalid `hold:` value is shown as an error in the HUD and holds for 200ms; `hold:` in
a modifier's file is an error, as modifiers keep their function anyway.

```markdown
hold: 200ms
## k Kitty
{shell(open -a kitty)|null}
```

A `layout:` line before the first heading sets how the HUD arranges the entries. `layout: list`,
the default, lists them top to bottom in columns of up to 12. `layout: rows` puts them left to
right in rows, in file order; an entry whose content starts with a `row: break` line starts a
new row (the line is not part of the content, and a `row:` line further down is). A row does
not wrap, so the HUD is as wide as the widest row. Any other `layout:` value, a second
`layout:` line, a `row:` value other than `break`, or `row:` without `layout: rows` is an
error shown in the HUD.

```markdown
layout: rows
## 1 Chrome
{shell(open -a "Google Chrome")|null}
## 2 Yab
{shell(open -a "Yab")|null}
## q Moshi
row: break
{shell(open -a "Moshi")|null}
```

Files that name no trigger key are skipped with a notification; a file that fails to
parse shows its error in the HUD. With no leader files, trigger keys behave as usual.
The directory is watched, and the tray's Reload reloads it too.

## Mouse gestures

Hold the right, middle or a side mouse button, drag in one direction and release to run
an action: back in the browser with a drag to the left, Mission Control with a drag up.
A drag of at least 30 pixels whose horizontal or vertical part is at least 70% of it is a
gesture `left`, `right`, `up` or `down`; anything else is a click.

Each file in `~/.config/burin/mouse/` is one button, named after it: `right.md`,
`middle.md`, or `buttonN.md` with N the button number macOS reports (`button3.md`,
`button4.md` for the side buttons; `button1.md` and `button2.md` are the right and middle
buttons). The left button is not supported. The format is the snippet format with
`## <direction> <title>` headings; text before the first heading is a comment:

```markdown
## left Back
app: Google Chrome
app: Safari
{key(cmd+[)}

## up Mission Control
{shell(open -b com.apple.exposelauncher)|null}
```

One or more `app:` lines (one per line) at the start of a gesture limit it to those apps,
by name as the Dock shows it or by bundle ID, matched exactly against the frontmost app.
A comma is part of a name. A blank line ends the `app:` lines; a later `app:` line is
content. A gesture runs the first section of its direction that has the frontmost app in
its `app:` lines, or else the first one without any. The content is expanded
like a leader entry's, with the same built-ins, and its result is handled the same way.

The button's press is held back until its release, as it is not known before whether it
is a click or a gesture. A click, or a gesture that no section takes, is then replayed,
so a right click opens its context menu on release rather than on press. Only buttons
with a file are watched; with no files, mouse events never reach burin. A file name that
is no button, an unknown direction or an empty `app:` line is announced and counted as an
error in the tray, and the whole file is skipped. The directory is watched, and the
tray's Reload reloads it too.

While a button is held, the titles of its gestures show around the pointer, each on the
side of its direction (the direction itself when the heading has no title): those for the
frontmost app and those for every app, as a gesture would pick them. They change color as
the pointer slides towards them, and turn green once releasing runs the gesture. They
disappear on release, and show while a leader is held too.

## Quitter

After [Quitter for Mac](https://marco.org/apps): hide or quit an app, or close its
windows, once it has not been the frontmost app for a while. Switching to an app resets its
timer; background activity (sound, network, notifications) does not. Rules live in
`~/.config/burin/quitter.md`, one `## ` section per app of `key: value` lines; other lines,
and the text before the first heading, are comments. Any `word:` line inside a section is
parsed as a key, so a comment must not take that form (an unknown key makes the section an
error):

```markdown
## Slack
after: 30m
action: hide

## Preview
bundle: com.apple.Preview
after: 10m
action: quit

## Dev Chrome
path: ~/Apps/Google Chrome Canary.app
after: 2h
```

- `after:` (required) is how long the app may stay in the background, a Go duration
  (`90s`, `30m`, `1h`).
- `action:` is `hide` (the default), `quit` or `close_window`. A hidden app is not hidden
  again. Quit is the normal Cmd-Q, never a force quit: an app with unsaved documents asks
  about them, and is asked again only after another full period. `close_window` closes the
  app's standard windows (minimized ones too) with their close buttons and leaves floating
  windows, panels and dialogs open, e.g. the Hammerspoon Console but not its alerts; the app
  keeps running and is not hidden. It closes whatever standard windows are open every
  30-second check until the app is frontmost again, and needs Accessibility permission. A
  window with unsaved changes may ask about them.
- The heading is matched exactly against the app's name as the Dock shows it. With a
  `bundle:` line (the bundle identifier) or a `path:` line (the full path of the `.app`,
  exact, `~` for the home directory) the heading is only a label. A section may have one of
  `bundle:` and `path:`, not both; the first section matching an app is the one that runs.

An app burin first sees, at startup or when launched, starts its period then; apps not
running are left alone, and so is burin itself. The frontmost app is sampled every 2
seconds, so a switch to an app shorter than about 2 seconds may be missed, but anything
longer resets its timer. The running apps are checked against the rules every 30 seconds,
so an app is hidden or quit up to 30 seconds after its period ends. A section with
an unknown key, a missing or invalid `after:` or an invalid `action:` is skipped and
counted as an error in the tray; the other sections still load. The file is watched, and
the tray's Reload rereads it too; a reload takes effect at the next 30-second check.

## Window rules

Rules live in `~/.config/burin/window_rules.md`. While Mission Control is up, a round is
skipped entirely. The left margin rule only moves a window; it narrows the window only
when it would otherwise cross the right edge of the screen.

## Development

Requires Go 1.27+.

```bash
go tool mygo dev        # live reload
go test ./...
go tool mygo vet .
go tool mygo build      # output in build/darwin-arm64/
```

### Permissions and signing

The event tap needs Accessibility (which also covers Input Monitoring).
The app only checks at startup; a "Grant Accessibility" tray item
triggers the system prompt when it is missing, and relaunches the app
when clicked again after granting.

macOS remembers the grant per code signature, and an ad-hoc signature
changes with every build. Create a self-signed identity once so it
sticks:

```bash
scripts/signing-cert.sh                      # creates "Burin Signing" in the login keychain
go tool mygo dev -sign "Burin Signing"       # mygo.json already sets it for mygo build
tccutil reset Accessibility io.github.liukun.burin   # once, to drop the stale ad-hoc entry
```

## Open at login

The tray menu's "Open at Login" registers the app as a login item
(System Settings > General > Login Items). It needs the packaged app:
build it with `go tool mygo build` and run the bundle from /Applications.
Under `mygo dev` the item is disabled, so the temporary dev bundle is
never registered.

## Known limitations

- A snippet whose placeholder moves focus (e.g. `{shell(open -a X)}` followed by more text) pastes into the newly focused app.
- If writing the pasteboard fails, the keyword is already deleted and cannot be restored; the failure is only logged.
- If the snippets root directory is deleted and recreated, the watcher stops; use the tray's Reload.
- `unwrap` matches `\s` and `\d` as ASCII only.
- The snippet watcher is the hand-written fsnotify one kept from earlier, not `mygo.WatchFiles`.
- Leader entry keys are keycodes of the ANSI layout; on other layouts they follow the key's position, not its label.
- A mouse gesture holds back the drag events of its button too; with two gesture buttons down at once, only the one pressed last is tracked.
- Holding a modifier trigger swallows its combinations too: with `cmd_r.md` present, right Cmd+Tab does nothing.

## License

[MIT](LICENSE)
