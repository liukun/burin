# Quitter

After [Quitter for Mac](https://marco.org/apps): hide or quit an app, or close its
windows, once it has not been the frontmost app for a while. Switching to an app resets its
timer; background activity (sound, network, notifications) does not. Rules live in
`~/.config/burin/quitter.md`, one `## ` section per app of `:key: value` lines. Only lines
of that form, with the key between two colons at the start of the line, are fields; every
other line, such as `Note: ...`, and the text before the first heading, is a comment. An
unknown `:key:` makes the section an error:

```markdown
## Slack
:after: 30m
:action: hide

## Preview
:bundle: com.apple.Preview
:after: 10m
:action: quit

## Dev Chrome
:path: ~/Apps/Google Chrome Canary.app
:after: 2h
```

- `:after:` (required) is how long the app may stay in the background, a Go duration
  (`90s`, `30m`, `1h`).
- `:action:` is `hide` (the default), `quit` or `close_window`. A hidden app is not hidden
  again. Quit is the normal Cmd-Q, never a force quit: an app with unsaved documents asks
  about them, and is asked again only after another full period. `close_window` closes the
  app's standard windows (minimized ones too) with their close buttons and leaves floating
  windows, panels and dialogs open, e.g. the Hammerspoon Console but not its alerts; the app
  keeps running and is not hidden. It closes whatever standard windows are open every
  30-second check until the app is frontmost again, and needs Accessibility permission. A
  window with unsaved changes may ask about them.
- The heading is matched exactly against the app's name as the Dock shows it. With a
  `:bundle:` line (the bundle identifier) or a `:path:` line (the full path of the `.app`,
  exact, `~` for the home directory) the heading is only a label. A section may have one of
  `:bundle:` and `:path:`, not both; the first section matching an app is the one that runs.

An app burin first sees, at startup or when launched, starts its period then; apps not
running are left alone, and so is burin itself. The frontmost app is sampled every 2
seconds, so a switch to an app shorter than about 2 seconds may be missed, but anything
longer resets its timer. The running apps are checked against the rules every 30 seconds,
so an app is hidden or quit up to 30 seconds after its period ends. A section with
an unknown key, a missing or invalid `:after:` or an invalid `:action:` is skipped and
counted as an error in the tray; the other sections still load. The file is watched, and
the tray's Reload rereads it too; a reload takes effect at the next 30-second check.
