# Mouse gestures

Hold the right, middle or a side mouse button, drag in one direction and release to run
an action: back in the browser with a drag to the left, Mission Control with a drag up.
There are eight directions. A drag of at least 30 pixels within about 23° of an axis (its
horizontal or vertical part is at least 70% of it) is `left`, `right`, `up` or `down`. A
drag of at least 60 pixels in the diagonal sectors between them is `up-left`, `up-right`,
`down-left` or `down-right`. Anything shorter, such as a diagonal drag of 30 to 59 pixels,
is a click.

Each file in `~/.config/burin/mouse/` is one button, named after it: `right.md`,
`middle.md`, or `buttonN.md` with N the button number macOS reports (`button3.md`,
`button4.md` for the side buttons; `button1.md` and `button2.md` are the right and middle
buttons). The left button is not supported. The format is the snippet format with
`## <direction> <title>` headings; text before the first heading is a comment, except a
`:hold:` line:

```markdown
## left Back
:app: Google Chrome
:app: Safari
{key(cmd+[)}

## up Mission Control
{shell(open -b com.apple.exposelauncher)|null}
```

One or more `:app:` lines (one per line) at the start of a gesture limit it to those apps,
by name as the Dock shows it or by bundle ID, matched exactly against the frontmost app.
A comma is part of a name. A blank line ends the `:app:` lines; a later `:app:` line is
content. A gesture runs the first section of its direction that has the frontmost app in
its `:app:` lines, or else the first one without any. The content is expanded
like a leader entry's, with the same built-ins, and its result is handled the same way.
A gesture whose section has no content runs nothing and only swallows the click, which
makes it a cancel (`## down-right Cancel` with nothing under it).

The button's press is held back until its release, as it is not known before whether it
is a click or a gesture. A click, or a gesture that no section takes, is then replayed,
so a right click opens its context menu on release rather than on press. Only buttons
with a file are watched; with no files, mouse events never reach burin. A file name that
is no button, an unknown direction or an empty `:app:` line is announced and counted as an
error in the tray, and the whole file is skipped. The directory is watched, and the
tray's Reload reloads it too.

Once a button has been held for its hold delay, the titles of its gestures show around
the pointer, each on the side of its direction (the direction itself when the heading has no title): those for the
frontmost app and those for every app, as a gesture would pick them. The diagonal ones sit
in the corners, farther out than the others, as they need a longer drag. They change color as
the pointer slides towards them, and turn green once releasing runs the gesture. They
disappear on release, and show while a leader is held too.

The hold delay is 200ms, so a plain click does not flash the titles. A `:hold: <duration>`
line before the first heading changes it (`:hold: 150ms`, `:hold: 0.5s`); `:hold: 0` shows
them as soon as the button goes down. The delay only affects the titles: a gesture works
the same however quickly it is made. An invalid `:hold:` value, or a second `:hold:` line,
is announced as an error, and the button keeps its gestures with the 200ms delay.
