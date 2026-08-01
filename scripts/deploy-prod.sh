#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 --version vX.Y.Z --targets host1[,host2] --stun-endpoints host:port[,host:port] [--metrics-bind-address 127.0.0.1:PORT] [--strategy rolling]" >&2
  exit 2
}

version=
targets_csv=
endpoints_csv=
strategy=rolling
metrics_bind_address=127.0.0.1:9090
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) version=${2:-}; shift 2 ;;
    --targets) targets_csv=${2:-}; shift 2 ;;
    --stun-endpoints) endpoints_csv=${2:-}; shift 2 ;;
    --metrics-bind-address) metrics_bind_address=${2:-}; shift 2 ;;
    --strategy) strategy=${2:-}; shift 2 ;;
    *) usage ;;
  esac
done
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]] || { echo "version must be an exact v-prefixed semantic version" >&2; exit 2; }
[[ -n "$targets_csv" && -n "$endpoints_csv" ]] || usage
[[ "$strategy" == rolling ]] || { echo "only the rolling deployment strategy is supported" >&2; exit 2; }
[[ "$metrics_bind_address" =~ ^127\.0\.0\.1:([0-9]{1,5})$ ]] || {
  echo "metrics bind address must use 127.0.0.1:PORT" >&2
  exit 2
}
metrics_bind_port=${BASH_REMATCH[1]}
(( metrics_bind_port >= 1 && metrics_bind_port <= 65535 )) || { echo "metrics bind port is out of range" >&2; exit 2; }
command -v gh >/dev/null || { echo "gh is required" >&2; exit 1; }
command -v ssh >/dev/null || { echo "ssh is required" >&2; exit 1; }
command -v scp >/dev/null || { echo "scp is required" >&2; exit 1; }

IFS=',' read -r -a targets <<< "$targets_csv"
IFS=',' read -r -a endpoints <<< "$endpoints_csv"
if (( ${#targets[@]} == 0 || ${#targets[@]} != ${#endpoints[@]} )); then
  echo "targets and STUN endpoints must be non-empty lists of equal length" >&2
  exit 2
fi

deploy_user=${DEPLOY_USER:-root}
repo=${GITHUB_REPOSITORY:-endless-net/stun}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
gh release download "$version" --repo "$repo" --pattern checksums.txt --dir "$work"

for index in "${!targets[@]}"; do
  target=${targets[$index]//[[:space:]]/}
  endpoint=${endpoints[$index]//[[:space:]]/}
  [[ -n "$target" && -n "$endpoint" ]] || { echo "deployment list contains an empty value" >&2; exit 2; }
  ssh_host="$deploy_user@$target"

  # metrics_bind_address is a locally validated loopback token.
  # shellcheck disable=SC2029
  ssh "$ssh_host" "sudo env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin bash -s -- '$metrics_bind_address'" < "$(dirname "$0")/configure-metrics-bind.sh"

  machine=$(ssh "$ssh_host" uname -m)
  case "$machine" in
    x86_64|amd64) goarch=amd64 ;;
    aarch64|arm64) goarch=arm64 ;;
    *) echo "unsupported architecture $machine on $target" >&2; exit 1 ;;
  esac
  artifact="endlessnet-stun_${version}_linux_${goarch}"
  if [[ ! -f "$work/$artifact" ]]; then
    gh release download "$version" --repo "$repo" --pattern "$artifact" --dir "$work"
  fi
  expected=$(awk -v file="$artifact" '$2 == file || $2 == "*" file { print $1 }' "$work/checksums.txt")
  [[ "$expected" =~ ^[0-9a-fA-F]{64}$ ]] || { echo "checksum for $artifact is missing" >&2; exit 1; }
  actual=$(sha256sum "$work/$artifact" | awk '{print $1}')
  [[ "$actual" == "$expected" ]] || { echo "checksum mismatch for $artifact" >&2; exit 1; }

  remote_tmp=$(ssh "$ssh_host" 'mktemp /tmp/endlessnet-stun.XXXXXX')
  [[ "$remote_tmp" =~ ^/[A-Za-z0-9_./-]+$ ]] || { echo "unsafe remote temporary path" >&2; exit 1; }
  scp "$work/$artifact" "$ssh_host:$remote_tmp"
  # The version, checksum, and mktemp path are locally validated safe tokens.
  # shellcheck disable=SC2029
  if ! ssh "$ssh_host" "sudo env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin bash -s -- '$version' '$remote_tmp' '$expected'" < "$(dirname "$0")/install-release.sh"
  then
    echo "deployment failed on $target; verifying rollback before stopping rollout" >&2
    if ! "$(dirname "$0")/rollback.sh" --target "$target" --failed-version "$version" --stun-addr "$endpoint"; then
      echo "verified rollback failed on $target" >&2
    fi
    echo "rollout stopped after failure on $target" >&2
    exit 1
  fi

  if ! "$(dirname "$0")/smoke-test.sh" --stun-addr "$endpoint"; then
    echo "external smoke test failed on $target; rolling back and stopping rollout" >&2
    if ! "$(dirname "$0")/rollback.sh" --target "$target" --failed-version "$version" --stun-addr "$endpoint"; then
      echo "verified rollback failed on $target" >&2
    fi
    exit 1
  fi
  echo "$target successfully runs $version"
done
