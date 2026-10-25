# Licensing

- This project is MIT licensed (see `LICENSE`).
- Before adding a new dependency, check its license. Only permissive licenses are acceptable: MIT, BSD, Apache-2.0, ISC, Unlicense, 0BSD, MPL-2.0 (file-level copyleft is fine).
- Do not add dependencies under GPL, LGPL, AGPL, SSPL, BUSL, or any "non-commercial" / source-available license. They conflict with MIT distribution of a single static binary.
- Quick check: `head -3 "$(go env GOMODCACHE)/<module>@<version>/LICENSE"*`.
