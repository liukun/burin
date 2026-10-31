#!/bin/sh
# Rebuilds the app with the build time linked in and relaunches it from
# build/darwin-arm64. This is a development loop for running a real,
# signed bundle: an alternative to `go tool mygo dev`, not a step after
# it. Under `mygo dev` the app already rebuilds on save; do not run this
# then, it would kill the dev instance and start a second one.
#
# The build time shows in the tray menu as "Built ...". Without
# MYGO_LDFLAGS the menu falls back to the commit time.
set -e
cd "$(dirname "$0")/.."
pkill -x Burin || true
MYGO_LDFLAGS="-X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" go tool mygo build -skip-dmg
open build/darwin-arm64/Burin.app
