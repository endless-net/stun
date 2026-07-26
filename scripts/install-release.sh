#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 VERSION UPLOADED_BINARY EXPECTED_SHA256" >&2
  exit 2
fi

version=$1
uploaded=$2
expected=$3
test_mode=${ENDLESSNET_STUN_TEST_MODE:-0}
if [[ "$test_mode" == 1 ]]; then
  base=${ENDLESSNET_STUN_INSTALL_BASE:?test install base is required}
  config=${ENDLESSNET_STUN_CONFIG_FILE:?test config file is required}
  service=${ENDLESSNET_STUN_SERVICE_NAME:-endlessnet-stun.service}
  systemctl_bin=${ENDLESSNET_STUN_SYSTEMCTL:?test systemctl command is required}
  curl_bin=${ENDLESSNET_STUN_CURL:?test curl command is required}
else
  base=/opt/endlessnet-stun
  config=/etc/endlessnet-stun/stun.env
  service=endlessnet-stun.service
  systemctl_bin=systemctl
  curl_bin=curl
fi
release_dir="$base/releases/$version"

[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]] || {
  echo "version must be an exact v-prefixed semantic version" >&2
  exit 2
}
[[ "$expected" =~ ^[0-9a-fA-F]{64}$ ]] || {
  echo "expected checksum must be SHA256" >&2
  exit 2
}
[[ -f "$config" ]] || { echo "missing $config" >&2; exit 1; }
[[ -f "$uploaded" ]] || { echo "missing uploaded artifact $uploaded" >&2; exit 1; }

actual=$(sha256sum "$uploaded" | awk '{print $1}')
[[ "$actual" == "$expected" ]] || { echo "uploaded artifact checksum mismatch" >&2; exit 1; }
if [[ "$test_mode" == 1 ]]; then
  install -d -m 0755 "$release_dir"
  install -m 0755 "$uploaded" "$release_dir/endlessnet-stun"
else
  install -d -o root -g root -m 0755 "$release_dir"
  install -o root -g root -m 0755 "$uploaded" "$release_dir/endlessnet-stun"
fi
rm -f "$uploaded"

set -a
# shellcheck disable=SC1090
. "$config"
set +a
"$release_dir/endlessnet-stun" --check-config >/dev/null

metrics_addr=${ENDLESSNET_STUN_METRICS_ADDR:-127.0.0.1:9090}
case "$metrics_addr" in
  :*) metrics_addr="127.0.0.1$metrics_addr" ;;
  0.0.0.0:*) metrics_addr="127.0.0.1:${metrics_addr##*:}" ;;
  '[::]':*) metrics_addr="127.0.0.1:${metrics_addr##*:}" ;;
esac
check_local_readiness() {
  local attempt attempts bind_host candidate checked device local_ip local_ips metrics_port
  metrics_port=${metrics_addr##*:}
  attempts=${ENDLESSNET_STUN_READINESS_ATTEMPTS:-20}
  if [[ "$test_mode" == 1 ]]; then
    local_ips=${ENDLESSNET_STUN_TEST_LOCAL_IPS:-}
  else
    local_ips=$(hostname -I 2>/dev/null || true)
  fi
  try_readiness_candidate() {
    case "$checked" in
      *" $candidate "*) return 1 ;;
    esac
    checked="$checked$candidate "
    if "$curl_bin" --fail --silent --max-time 5 "http://$candidate/readyz" >/dev/null 2>&1; then
      return 0
    fi
    case "$candidate" in
      \[*\]:*) bind_host=${candidate#\[}; bind_host=${bind_host%%\]*} ;;
      *) bind_host=${candidate%:*} ;;
    esac
    case "$bind_host" in
      0.0.0.0|127.0.0.1|::|::1) return 1 ;;
    esac
    if "$curl_bin" --interface "$bind_host" --fail --silent --max-time 5 "http://$candidate/readyz" >/dev/null 2>&1; then
      return 0
    fi
    if command -v ip >/dev/null 2>&1; then
      device=$(ip -o addr show | awk -v address="$bind_host" '{ value=$4; sub(/\/.*/, "", value); if (value == address) { print $2; exit } }')
      device=${device%@*}
      if [[ -n "$device" ]] && "$curl_bin" --interface "$device" --fail --silent --max-time 5 "http://$candidate/readyz" >/dev/null 2>&1; then
        return 0
      fi
    fi
    return 1
  }
  for (( attempt = 1; attempt <= attempts; attempt++ )); do
    checked=" "
    for candidate in "$metrics_addr" "127.0.0.1:$metrics_port" "0.0.0.0:$metrics_port" "[::1]:$metrics_port"; do
      if try_readiness_candidate; then
        return 0
      fi
    done
    for local_ip in $local_ips; do
      case "$local_ip" in
        *:*) candidate="[$local_ip]:$metrics_port" ;;
        *) candidate="$local_ip:$metrics_port" ;;
      esac
      if try_readiness_candidate; then
        return 0
      fi
    done
    if (( attempt < attempts )); then
      sleep 1
    fi
  done
  echo "local readiness failed on metrics port $metrics_port" >&2
  if command -v ss >/dev/null 2>&1; then
    ss -H -ltn "sport = :$metrics_port" >&2 || true
  fi
  return 1
}

previous=
if [[ -L "$base/current" ]]; then
  previous=$(readlink -f "$base/current" 2>/dev/null || true)
  [[ -n "$previous" ]] || { echo "current symlink cannot be resolved" >&2; exit 1; }
  case "$previous" in
    "$base"/releases/*) printf '%s\n' "$previous" > "$base/previous-release" ;;
    *) echo "current symlink points outside the release tree" >&2; exit 1 ;;
  esac
elif [[ -e "$base/current" ]]; then
  echo "current exists but is not a symlink" >&2
  exit 1
else
  rm -f "$base/previous-release"
fi

rollback() {
  failure=$?
  trap - ERR
  set +e
  if [[ -n "$previous" && -x "$previous/endlessnet-stun" ]]; then
    rollback_link="$base/.current.rollback.$$"
    if ln -s "$previous" "$rollback_link" &&
      mv -Tf "$rollback_link" "$base/current" &&
      "$systemctl_bin" restart "$service" &&
      "$systemctl_bin" is-active --quiet "$service" &&
      check_local_readiness
    then
      printf '%s\n' "$(basename "$previous")" > "$base/deployed-version"
      echo "automatic rollback restored $(basename "$previous"); local readiness passed" >&2
    else
      echo "automatic rollback to $(basename "$previous") could not be verified" >&2
    fi
  else
    failed_current=$(readlink -f "$base/current" 2>/dev/null || true)
    if [[ "$failed_current" == "$release_dir" ]]; then
      rm -f "$base/current" "$base/deployed-version"
      "$systemctl_bin" stop "$service"
    fi
    echo "deployment failed and no previous release exists for rollback" >&2
  fi
  exit "$failure"
}

next_link="$base/.current.next.$$"
ln -s "$release_dir" "$next_link"
mv -Tf "$next_link" "$base/current"
trap rollback ERR
"$systemctl_bin" restart "$service"
"$systemctl_bin" is-active --quiet "$service"
check_local_readiness
printf '%s\n' "$version" > "$base/deployed-version"
trap - ERR
echo "installed $version"
