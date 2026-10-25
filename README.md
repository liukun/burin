# Burin（刻字）

macOS menubar automation host written in Go, built on [MyGo](https://github.com/egoist/mygo).
A compiled, single-binary take on the Hammerspoon idea: features are Go packages under
`internal/`, built into one small app. No bundled interpreter, no runtime dependency.

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

## License

[MIT](LICENSE)
