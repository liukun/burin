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

## License

[MIT](LICENSE)
