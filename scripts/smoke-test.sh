#!/usr/bin/env sh
set -eu

usage() {
  echo "usage: $0 --stun-addr HOST:PORT [--timeout DURATION]" >&2
  exit 2
}

stun_addr=
timeout=5s
while [ "$#" -gt 0 ]; do
  case "$1" in
    --stun-addr) stun_addr=${2:-}; shift 2 ;;
    --timeout) timeout=${2:-}; shift 2 ;;
    *) usage ;;
  esac
done
[ -n "$stun_addr" ] || usage

cd "$(dirname "$0")/.."
if [ -n "${SMOKE_BINARY:-}" ]; then
  "$SMOKE_BINARY" --stun-addr "$stun_addr" --timeout "$timeout"
else
  go run ./cmd/endlessnet-stun-smoke --stun-addr "$stun_addr" --timeout "$timeout"
fi
