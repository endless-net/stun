#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 METRICS_BIND_ADDRESS" >&2
  exit 2
fi

address=$1
if [[ ${ENDLESSNET_STUN_TEST_MODE:-0} == 1 ]]; then
  config=${ENDLESSNET_STUN_CONFIG_FILE:?test config file is required}
  test_mode=1
else
  config=/etc/endlessnet-stun/stun.env
  test_mode=0
fi

[[ "$address" =~ ^(\[[0-9A-Fa-f:]+\]|[A-Za-z0-9.-]+):([0-9]{1,5})$ ]] || {
  echo "metrics bind address must be HOST:PORT" >&2
  exit 2
}
port=${BASH_REMATCH[2]}
[[ "$address" == 127.0.0.1:* ]] || {
  echo "metrics bind address must use 127.0.0.1" >&2
  exit 2
}
(( port >= 1 && port <= 65535 )) || { echo "metrics bind port is out of range" >&2; exit 2; }
[[ -f "$config" ]] || { echo "missing $config" >&2; exit 1; }

tmp=$(mktemp "${config}.XXXXXX")
cleanup() {
  rm -f "$tmp"
}
trap cleanup EXIT
awk -v value="$address" '
  BEGIN { written = 0 }
  /^ENDLESSNET_STUN_METRICS_ADDR=/ {
    if (!written) {
      print "ENDLESSNET_STUN_METRICS_ADDR=" value
      written = 1
    }
    next
  }
  { print }
  END {
    if (!written) {
      print "ENDLESSNET_STUN_METRICS_ADDR=" value
    }
  }
' "$config" > "$tmp"

if [[ "$test_mode" == 1 ]]; then
  chmod --reference="$config" "$tmp"
else
  chown --reference="$config" "$tmp"
  chmod --reference="$config" "$tmp"
fi
mv -f "$tmp" "$config"
trap - EXIT
echo "configured metrics bind address $address"
