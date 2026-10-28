#!/usr/bin/env bash
# Separate local acceptance only. The private network namespace dies with this process.
set -euo pipefail
cd "$(dirname "$0")/../.."
binary=$(mktemp /tmp/stun-acceptance.XXXXXX)
trap 'rm -f "$binary"' EXIT
go test -c -tags=integration -o "$binary" ./internal/server
sha256sum "$binary"
uname -a
unshare -Urn bash -c '
set -euo pipefail
ip link set lo up
ip -6 addr add fd00::1/128 dev lo
ip -6 addr add fd00::10/128 dev lo
ip -6 addr add fd00::11/128 dev lo
export STUN_FULL_LOAD=1 GOMAXPROCS=2
exec "$1" -test.run "^TestFullLoad$" -test.v -test.timeout=15m
' bash "$binary"
