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
operators `kp_minus kp_plus kp_multiply kp_divide kp_enter kp_clear` (e.g. `cmd_r.md`).
The format is the snippet format with `## <key> <title>` headings; text before the first
heading is a comment:

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
snippet's, with an extra `{reload}` that reloads snippets and leaders. A non-empty result
is copied to the clipboard and announced; an empty one (`|null`) does nothing more.
The HUD marks a running entry `●`, a finished one `✓` (for a second) and a failed one
`✗`. Files that name no trigger key are skipped with a notification; a file that fails to
parse shows its error in the HUD. With no leader files, trigger keys behave as usual.
The directory is watched, and the tray's Reload reloads it too.

## Development

Requires Go 1.27+.

```bash
go tool mygo dev        # live reload
go test ./...
go tool mygo vet .
go tool mygo build      # output in build/darwin-arm64/
```

## Known limitations

- A snippet whose placeholder moves focus (e.g. `{shell(open -a X)}` followed by more text) pastes into the newly focused app.
- If writing the pasteboard fails, the keyword is already deleted and cannot be restored; the failure is only logged.
- If the snippets root directory is deleted and recreated, the watcher stops; use the tray's Reload.
- `unwrap` matches `\s` and `\d` as ASCII only.
- The snippet watcher is the hand-written fsnotify one kept from earlier, not `mygo.WatchFiles`.
- Leader entry keys are keycodes of the ANSI layout; on other layouts they follow the key's position, not its label.
- Holding a modifier trigger swallows its combinations too: with `cmd_r.md` present, right Cmd+Tab does nothing.

## License

[MIT](LICENSE)
