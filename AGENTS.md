# Licensing

- This project is MIT licensed (see `LICENSE`).
- Before adding a new dependency, check its license. Only permissive licenses are acceptable: MIT, BSD, Apache-2.0, ISC, Unlicense, 0BSD, MPL-2.0 (file-level copyleft is fine).
- Do not add dependencies under GPL, LGPL, AGPL, SSPL, BUSL, or any "non-commercial" / source-available license. They conflict with MIT distribution of a single static binary.
- Quick check: `head -3 "$(go env GOMODCACHE)/<module>@<version>/LICENSE"*`.

# Running the app

- After changing Go code, restart the running app so the change takes
  effect. Do it without asking:

  ```sh
  scripts/restart.sh
  ```

  It rebuilds a signed bundle with the build time linked in (the tray
  menu's "Built" line) and relaunches it from build/darwin-arm64. Skip it
  when the user is already running `go tool mygo dev`: that rebuilds on
  save by itself, and the script would kill the dev instance.

- The log is at `~/Library/Logs/burin.log` (also stderr). Check it after a
  restart to confirm the rules loaded.

# Config file syntax

- Field keys (`:key: value` lines, parsed by `internal/field`) allow only
  letters, digits and underscores. Name a multi-word key with an
  underscore (`:count_only:`, `:height_max:`), never a hyphen: a
  hyphenated key is not a field at all and is silently read as a
  comment line.
