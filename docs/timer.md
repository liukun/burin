# Timer

A work timer in a small bar at the top center of every display, under the menu bar. It
counts how long you have been working without a break and reminds you to rest. While you
work it shows `▶12m -2m`: the time worked, and how long you would have to stay away for it
to count as a break (`⏸︎` instead of `▶` while you are away). Past the work time it gets
warning signs, one more every 5 minutes, and a notification reminds you every minute you
keep working; clicking the notification starts a break. During a break it shows `💤 5m`.
Right-click the bar for today's active time and work sessions, and to start a break,
restart the timer, try the reminder or reset today's counts.

Settings live in `~/.config/burin/timer.md`: a switch and two thresholds before the first
heading, then one `## ` section per app of `:key: value` lines. Only lines of that form, with the key
between two colons at the start of the line, are fields; every other line is a comment:

```markdown
:enabled: yes
:idle: 2m
:work: 25m

## Slack
:idle_app: yes

## Chat
:bundle: com.example.chat
:idle_app: yes
```

- `:enabled:` turns the timer on or off: `yes` or `true` (the default), `no` or `false`.
  While it is off nothing is counted, no reminder comes and the bar shows no timer; with
  the [badges](badges.md) off too, the bar is not shown at all. Turning it back on starts
  from a break, as waking from sleep does: the time it was off is not counted.
- `:idle:` is how long you must be away from the keyboard and mouse, in a row, for the
  work period to end and a break to start, a Go duration (`90s`, `2m`). The default is
  `2m`.
- `:work:` is how long a work period runs before the reminder. The default is `25m`.
- `:idle_app: yes` (or `true`) makes using the app not end a break: while it is the
  frontmost app during a break, you still count as away, so checking a message does not
  start a work period. While you are working it counts as work like any other app. `no` or
  `false`, or no `:idle_app:` line, leaves the app counting as work. The heading is matched
  exactly against the app's name as the Dock shows it; with a `:bundle:` line (the bundle
  identifier) the heading is only a label.

Without a `timer.md` the defaults apply, with 微信, WeChat and Slack not ending a break. A
file that is there but has no sections has no such apps. An `:enabled:` other than `yes`,
`no`, `true` or `false` leaves the timer on; an invalid or non-positive threshold keeps its
default; a section with an unknown key or an `:idle_app:` other than `yes`, `no`, `true` or
`false` is skipped. Both are counted as errors in the tray. The file is watched, and
the tray's Reload rereads it too; new thresholds apply to the period that is running. The
tray shows `Timer off` while the timer is off.

Coming back from a break takes 20 seconds of activity, which count as work. Activity is
input in the last 10 seconds, sampled every 2 seconds; these are fixed. Sleep and a locked
screen end the work period. The timer's state survives restarts: it is kept in the app's
data folder, not in the config folder.
