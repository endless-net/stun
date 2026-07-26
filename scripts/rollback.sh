#!/usr/bin/env sh
set -eu

usage() {
  echo "usage: $0 --target HOST [--failed-version vX.Y.Z] --stun-addr HOST:PORT" >&2
  exit 2
}

target=
failed_version=
stun_addr=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --target) target=${2:-}; shift 2 ;;
    --failed-version) failed_version=${2:-}; shift 2 ;;
    --stun-addr) stun_addr=${2:-}; shift 2 ;;
    *) usage ;;
  esac
done
[ -n "$target" ] && [ -n "$stun_addr" ] || usage
if [ -n "$failed_version" ]; then
  echo "$failed_version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$' || {
    echo "failed version must be an exact v-prefixed semantic version" >&2
    exit 2
  }
fi

deploy_user=${DEPLOY_USER:-root}
ssh_host="$deploy_user@$target"
# failed_version is empty or a locally validated semantic version.
# shellcheck disable=SC2029
ssh "$ssh_host" "sudo sh -s -- '$failed_version'" <<'REMOTE'
set -eu
failed_version=$1
base=/opt/endlessnet-stun
config=/etc/endlessnet-stun/stun.env
previous=$(cat "$base/previous-release" 2>/dev/null || true)
current=$(readlink -f "$base/current" 2>/dev/null || true)
case "$current" in
  "$base"/releases/*) ;;
  *) echo "current symlink is invalid" >&2; exit 1 ;;
esac

if [ -z "$failed_version" ] || [ "$(basename "$current")" = "$failed_version" ]; then
  case "$previous" in
    "$base"/releases/*) ;;
    *) echo "no valid previous release is recorded" >&2; exit 1 ;;
  esac
  [ -x "$previous/endlessnet-stun" ] || { echo "previous binary is missing" >&2; exit 1; }
  tmp_link="$base/.current.rollback.$$"
  ln -s "$previous" "$tmp_link"
  mv -Tf "$tmp_link" "$base/current"
  current=$previous
else
  echo "rollback already restored $(basename "$current"); verifying it"
fi

systemctl restart endlessnet-stun.service
systemctl is-active --quiet endlessnet-stun.service
[ -f "$config" ] || { echo "missing $config" >&2; exit 1; }
set -a
# shellcheck disable=SC1090
. "$config"
set +a
metrics_addr=${ENDLESSNET_STUN_METRICS_ADDR:-127.0.0.1:9090}
case "$metrics_addr" in
  :*) metrics_addr="127.0.0.1$metrics_addr" ;;
  0.0.0.0:*) metrics_addr="127.0.0.1:${metrics_addr##*:}" ;;
  '[::]':*) metrics_addr="127.0.0.1:${metrics_addr##*:}" ;;
esac
check_local_readiness() {
  metrics_port=${metrics_addr##*:}
  attempts=${ENDLESSNET_STUN_READINESS_ATTEMPTS:-20}
  try_readiness_candidate() {
    case "$checked" in
      *" $candidate "*) return 1 ;;
    esac
    checked="$checked$candidate "
    if curl --fail --silent --max-time 5 "http://$candidate/readyz" >/dev/null 2>&1; then
      return 0
    fi
    case "$candidate" in
      \[*\]:*) bind_host=${candidate#\[}; bind_host=${bind_host%%\]*} ;;
      *) bind_host=${candidate%:*} ;;
    esac
    case "$bind_host" in
      0.0.0.0|127.0.0.1|::|::1) return 1 ;;
    esac
    if curl --interface "$bind_host" --fail --silent --max-time 5 "http://$candidate/readyz" >/dev/null 2>&1; then
      return 0
    fi
    if command -v ip >/dev/null 2>&1; then
      device=$(ip -o addr show | awk -v address="$bind_host" '{ value=$4; sub(/\/.*/, "", value); if (value == address) { print $2; exit } }')
      device=${device%@*}
      if [ -n "$device" ] && curl --interface "$device" --fail --silent --max-time 5 "http://$candidate/readyz" >/dev/null 2>&1; then
        return 0
      fi
    fi
    return 1
  }
  attempt=1
  while [ "$attempt" -le "$attempts" ]; do
    checked=' '
    for candidate in "$metrics_addr" "127.0.0.1:$metrics_port" "0.0.0.0:$metrics_port" "[::1]:$metrics_port"; do
      if try_readiness_candidate; then
        return 0
      fi
    done
    for local_ip in $(hostname -I 2>/dev/null || true); do
      case "$local_ip" in
        *:*) candidate="[$local_ip]:$metrics_port" ;;
        *) candidate="$local_ip:$metrics_port" ;;
      esac
      if try_readiness_candidate; then
        return 0
      fi
    done
    attempt=$((attempt + 1))
    if [ "$attempt" -le "$attempts" ]; then
      sleep 1
    fi
  done
  echo "local readiness failed on metrics port $metrics_port" >&2
  if command -v ss >/dev/null 2>&1; then
    ss -H -ltn "sport = :$metrics_port" >&2 || true
  fi
  return 1
}
check_local_readiness
basename "$current" > "$base/deployed-version"
echo "rolled back to $(basename "$current"); local readiness passed"
REMOTE

"$(dirname "$0")/smoke-test.sh" --stun-addr "$stun_addr"
