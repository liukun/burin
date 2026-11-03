# Badges

The Dock badges of chosen apps, shown in the bar at the top center of every display, to the
right of the [timer](timer.md): each app with a badge shows its icon and the badge's text,
`•` for a badge without a count. Click one to bring the app to the front. Apps without a
badge take no room, and the bar widens and stays centered as badges come and go.

The apps are listed in `~/.config/burin/badges.md`, one `## ` section per app, in the
order the bar shows them, after an optional switch before the first heading:

```markdown
:enabled: yes
:speak_from: 10:00
:speak_until: 22:00

## Slack
:count_only: yes
:speak: yes

## WhatsApp
## Telegram

## Chat
:bundle: com.example.chat

## Discord
:count_only: yes
```

- `:enabled:` turns the badges on or off: `yes` or `true` (the default), `no` or `false`.
  While they are off no badge icons show and the Dock is not read; with the
  [timer](timer.md) off too, the bar is not shown at all.
- The heading is matched exactly against the app's name as the Dock shows it (hover its
  icon in the Dock to see it).
- With a `:bundle:` line (the bundle identifier) the heading is only a label, and the
  badge is the one of the running app with that identifier.
- `:count_only: yes` hides the app's badge while it is a dot rather than a count, so the
  app only shows with a number. Slack and Discord badge a dot for unread channel messages
  and a number for direct messages and mentions, so this keeps the ones meant for you.
- `:speak: yes` reads the app's count aloud while you are on a break (the
  [timer](timer.md) shows one; nothing is spoken while the timer is off or the Mac sleeps
  or is locked): "Slack has 3 messages", or "99 plus messages" for a `99+`, several apps in one
  breath. A dot is not spoken. The count is spoken again every minute while the break and the badge last, even
  if it has not changed, so a message that came in as you stepped away still reaches you.
  The default is `no`.
- `:speak_from:` and `:speak_until:`, before the first heading, are the time of day counts
  may be spoken, as `HH:MM` on a 24-hour clock: 10:00 and 22:00 by default. An until
  earlier than the from crosses midnight (`22:00` to `02:00`); the same time for both
  speaks never.

Without a `badges.md`, Slack, WhatsApp, Telegram and 微信 are shown. A file that is there
but has no sections shows none. A section with an unknown key, a repeated key or a `:count_only:`
or `:speak:` other than `yes`, `no`, `true` or `false` is skipped, an `:enabled:` other than those
leaves the badges on, and a `:speak_from:` or `:speak_until:` that is not a time, or is
repeated, keeps its default; all are counted as errors in the tray, which shows `Badges off` while the badges are off. The file is watched, and the tray's Reload rereads it too; a change
shows at the next read.

The badges are read from the Dock through Accessibility every 2 seconds, so they need
Accessibility permission, which burin asks for anyway, and appear up to 2 seconds after
the Dock shows them.
