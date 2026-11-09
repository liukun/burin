# Clipboard flash

Each time something is copied, in any app, a 📑 shows in the middle of the display under
the pointer: it grows from 80 to 220 points as it fades out, over 0.6 seconds. A copy
during a flash starts it over. The flash never takes the keyboard or a click, and shows
over other apps' windows, full screen ones included, on every space.

The pasteboard tells no one of its changes, so burin reads its change count every 200 ms:
anything that changes the pasteboard flashes, a copy from the command line included, and
two copies within the same 200 ms flash once.

The setting lives in `~/.config/burin/clipboard.md`, a `:key: value` line before any
heading. Only lines of that form, with the key between two colons at the start of the
line, are fields; every other line is a comment:

```markdown
:enabled: yes
```

- `:enabled:` turns the flash on or off: `yes` or `true` (the default), `no` or `false`.

Without a `clipboard.md` the flash is on. An `:enabled:` other than `yes`, `no`, `true`
or `false` leaves it on and is logged. The file is watched, and the tray's Reload rereads
it too. The tray shows `Clipboard flash: on` or `off`.

A [snippet](snippets.md) pastes its content through the pasteboard; that paste does not
flash: burin notes the change count its write made and skips that one change. A copy of
your own within the same 200 ms still flashes. If the write fails partway, the pasteboard
may still change without burin knowing its count, and that change flashes once. Leader
entries and mouse gestures that copy their result do flash, as that is a copy.
