# Window rules

Rules live in `~/.config/burin/window_rules.md`. While Mission Control is up, a round is
skipped entirely. The left margin rule only moves a window; it narrows the window only
when it would otherwise cross the right edge of the screen.

## Format

Each `## ` heading starts a section of `:key: value` lines, with the key between two
colons at the start of the line. Every other line, and the text before the first heading,
is a comment. An unknown key, or a key repeated in a section (only `:except:` may
repeat), makes the section an error. A section with a `:margin:` line is a margin rule;
any other section is a pin rule. A section that fails to parse is skipped and counted as
an error in the tray; the other sections still load. A missing file has no rules.

The rules run every 3 seconds: pin rules first, in file order, then margin rules. They
only move standard windows that are neither minimized nor full screen.

## Pin rules

A pin rule keeps the windows of one app in a corner of a display. The heading is the
app's exact process name.

```markdown
## Slack
:screen_w: 1920
:screen_h: 1200
:anchor: topLeft
:width_min: 0.3
:width_max: 0.5
:height_min: 0.5
:height_max: 1
:fullScreenable: true
```

- `:screen_w:` and `:screen_h:` (together or not at all) pick the target display by the
  size of its full frame, within 5 points. When that display is not connected the rule
  is skipped; windows on other displays move to it. Without them, a window stays on its
  own display.
- `:anchor:` (required) is `topLeft` or `bottomLeft`, the corner of the usable area
  (the display without the menu bar and Dock) the window sticks to.
- `:width_min:`, `:width_max:`, `:height_min:` and `:height_max:` (all required) bound
  the window size as fractions of the usable area, with `0 <= min <= max <= 1`. A size
  inside the range is kept, one outside is clamped.
- `:title:` is a regular expression (Go syntax); only windows whose title matches are
  pinned.
- `:oldest:` is `true` or `false`; with `true`, only the app's oldest window, the one with
  the lowest CGWindowID, is pinned (after `:title:` and `:fullScreenable:`). That tells
  Slack's main window, created at launch, from a DM pop-out or huddle window. The oldest
  is looked up among the app's Accessibility windows, so while it is minimized or full
  screen the app is left alone, as it is when Accessibility does not answer. A main window
  in another Space is usually not seen at all, so a pop-out on the current Space may be
  pinned then. Note that a main window reopened after closing is newer than a pop-out
  that stayed open.
- `:fullScreenable:` is `true` or `false`; with `true`, only windows with a full screen
  button are pinned, which tells a main window from settings windows.

An app may have several sections: the first whose display is connected (or that names
no display) runs, so a section without `:screen_w:` and `:screen_h:` after one with them
is the fallback when that display is unplugged.

## Margin rules

A margin rule keeps the windows on one display at least `:margin:` points from the left
edge of its usable area. The heading is only a label.

```markdown
## Left margin
:screen_w: 1920
:screen_h: 1200
:margin: 66
:except: Finder
```

- `:screen_w:` and `:screen_h:` (required) pick the display by the size of its full frame.
- `:margin:` (required) is the margin in points, not negative.
- `:except:` names an app whose windows the rule leaves alone; repeat the line for more
  apps. Windows pinned by a pin rule are left alone without being listed.
