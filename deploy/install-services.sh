#!/bin/sh
# Install local all-in-one systemd services for an already enrolled amd64 node.
set -eu
root=${1:-/opt/seesize-dev}
agent_id=${2:-aliyun-test-18}
case "$root" in /opt/*) ;; *) echo 'Root must be a dedicated directory under /opt' >&2; exit 1;; esac
case "$root" in *[!a-zA-Z0-9/_-]*|*/../*|*/./*|*/) echo 'Unsupported root path' >&2; exit 1;; esac
case "$agent_id" in ''|*[!a-zA-Z0-9_-]*) echo 'Unsupported Agent ID' >&2; exit 1;; esac
[ "$(id -u)" -eq 0 ] || { echo 'Run as root' >&2; exit 1; }
[ "$(realpath "$root")" = "$root" ] || { echo 'Root must not contain symlinks' >&2; exit 1; }
source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
for file in seesize-hub-linux-amd64 seesize-agent-linux-amd64 seesize-backup-linux-amd64 admin.token agent-auth.token; do
 [ -f "$root/$file" ] && [ ! -L "$root/$file" ] || { echo "Missing regular file $file" >&2; exit 1; }
done
for name in seesize-hub.service seesize-agent.service seesize-backup.service seesize-backup.timer; do
 [ ! -e "/etc/systemd/system/$name" ] || { echo "Unit $name already exists; use systemctl edit for changes" >&2; exit 1; }
done
for dir in data backups scan-fixture; do
 [ ! -L "$root/$dir" ] || { echo 'State directories cannot be symlinks' >&2; exit 1; }
 mkdir -p "$root/$dir"
done
if ! id seesize >/dev/null 2>&1; then useradd --system --no-create-home --shell /usr/sbin/nologin seesize; fi
# Restrict ownership changes to SeeSize state. Credential sources stay root-only;
# systemd delivers private copies to the service user through LoadCredential.
chgrp seesize "$root"
chmod 750 "$root"
chown -R seesize:seesize "$root/data" "$root/backups"
chmod 700 "$root/data" "$root/backups"
chmod 600 "$root/admin.token" "$root/agent-auth.token"
chmod 755 "$root/seesize-hub-linux-amd64" "$root/seesize-agent-linux-amd64" "$root/seesize-backup-linux-amd64"
stage=$(mktemp -d /tmp/seesize-units.XXXXXX)
for name in seesize-hub.service seesize-agent.service seesize-backup.service seesize-backup.timer; do
 sed -e "s|@ROOT@|$root|g" -e "s|@AGENT_ID@|$agent_id|g" "$source_dir/systemd/$name" > "$stage/$name"
done
systemd-analyze verify "$stage"/*
# Migrate only PID files whose live executable matches the exact SeeSize target.
for component in hub agent backup; do
 if [ -f "$root/$component.pid" ]; then
  pid=$(cat "$root/$component.pid")
  case "$pid" in ''|*[!0-9]*) echo 'Invalid PID file' >&2; exit 1;; esac
  if [ -e "/proc/$pid/exe" ]; then
   [ "$(readlink "/proc/$pid/exe")" = "$root/seesize-$component-linux-amd64" ] || { echo 'PID belongs to a different executable' >&2; exit 1; }
   kill "$pid"
  fi
 fi
done
sleep 2
for name in seesize-hub.service seesize-agent.service seesize-backup.service seesize-backup.timer; do
 install -m 644 "$stage/$name" "/etc/systemd/system/$name"
done
systemctl daemon-reload
systemctl enable --now seesize-hub.service seesize-agent.service seesize-backup.timer
echo 'Services installed. Use systemctl status seesize-hub seesize-agent seesize-backup.timer'
echo "Validated unit staging directory: $stage"
