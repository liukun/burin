# Input source mark

Each time the frontmost app changes or the keyboard input source changes, the current
input source shows as a large character (120 points) in the middle of the display of the
current window, that is the focused window of the app brought forward, or of the
frontmost app for an input source change: faint (35% opaque) with a dark shadow, so it
reads over light and dark backgrounds alike without hiding much. Should that window not be
found (the app has none, burin lacks the Accessibility permission, or the app does not
answer within half a second), it shows on the display under the pointer instead.

Each time you click with the left button, it shows as a small character (18 points, 90%
opaque) with a dark shadow beside the pointer: just right of it, or left of it at the
display's right edge. It follows the pointer while it shows, onto another display too, and
stays put while the pointer does.

Either mark holds for 1.5 seconds and then fades out over 0.5 seconds; another change or
click meanwhile shows that mark again and starts its hold over. The two are apart: a click
that brings another app forward shows only the small mark beside the pointer, not the large
one. The marks never take the keyboard or a click, and show over other apps' windows, full
screen ones included, on every space.

burin learns of both changes from the system's notifications, so it reads nothing while
neither changes. An input source macOS switches to for the app brought forward counts as
a change too; the app and the input source may then show one right after the other. A
click is a press and release of the left button no more than 5 pixels apart; a drag, as
when selecting text or moving a window, shows nothing. The click itself goes through to
the app untouched.

The settings live in `~/.config/burin/input_sources.md`: a switch before the first
heading, then one `## ` section per input source, headed by its ID. Only lines of the
form `:key: value`, with the key between two colons at the start of the line, are fields;
every other line is a comment:

```markdown
:enabled: yes

## com.apple.keylayout.ABC
:text: A
:color: #ffffff

## com.tencent.inputmethod.wetype.pinyin
:text: 微
:color: #1ab31a
```

- `:enabled:` turns the mark on or off: `yes` or `true` (the default), `no` or `false`.
  While it is off burin does not listen for the app and input source changes.
- `:text:` is what shows for the input source, usually one character. Without it, the
  first letter of the input source's name shows.
- `:color:` is the text's color, `#rrggbb` or `#rrggbbaa`; white by default. Its alpha
  multiplies the mark's own: 35% for the large mark, 90% for the small one.
- `:show:` is `yes` (the default) or `no`: whether the input source shows at all. Set it
  to `no` for one you do not want to see, such as the layout you type in most of the time.

An input source without a section shows the first letter of its name in white, and its
ID and name are logged once to `~/Library/Logs/burin.log`, as
`input sources: no section for <ID> (<name>)`, so you can copy the ID into a heading.

Without an `input_sources.md` the mark is on and every input source shows the first
letter of its name. A value that cannot be read
(an `:enabled:` or `:show:` other than `yes`, `no`, `true` or `false`, a `:color:` that is
not `#rrggbb` or `#rrggbbaa`, an empty `:text:`) falls back to its default and is logged.
A section with an unknown or repeated key, or a heading seen before, is skipped and
logged. The file is watched, and the tray's Reload rereads it too. The tray shows
`Input source mark: on` or `off`.
