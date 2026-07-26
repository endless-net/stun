#!/usr/bin/env sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "install.sh must run as root" >&2
  exit 1
fi

root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
if ! getent group endlessnet-stun >/dev/null 2>&1; then
  groupadd --system endlessnet-stun
fi
if ! id endlessnet-stun >/dev/null 2>&1; then
  useradd --system --gid endlessnet-stun --home-dir /nonexistent --shell /usr/sbin/nologin endlessnet-stun
fi
install -d -o root -g root -m 0755 /opt/endlessnet-stun/releases
install -d -o root -g endlessnet-stun -m 0750 /etc/endlessnet-stun
if [ ! -e /etc/endlessnet-stun/stun.env ]; then
  install -o root -g endlessnet-stun -m 0640 "$root/configs/stun.example.env" /etc/endlessnet-stun/stun.env
fi
install -o root -g root -m 0644 "$root/deploy/systemd/endlessnet-stun.service" /etc/systemd/system/endlessnet-stun.service
systemctl daemon-reload
systemctl enable endlessnet-stun.service
echo "bootstrap complete; deploy a versioned release before starting endlessnet-stun.service"
