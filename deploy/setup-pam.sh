#!/usr/bin/env bash
# Configure the release's bundled PAM component without source or build tools.
set -euo pipefail
[[ $EUID -eq 0 && $# -eq 0 && $(uname -s) == Linux ]] || { echo 'Run as root on Linux, without arguments.' >&2; exit 1; }
for cmd in systemctl flock readlink; do
  command -v "$cmd" >/dev/null || { echo "Required command: $cmd" >&2; exit 1; }
done
binary=/opt/slurm-console/slurm-console
[[ -f "$binary" && ! -L "$binary" && -x "$binary" ]] || { echo 'Install the current application release first.' >&2; exit 1; }
[[ $(systemctl show slurm-console -p User --value) == root ]] || { echo 'The application service must run as root.' >&2; exit 1; }
lock=/run/lock/slurm-console.lock
if [[ ${SLURM_CONSOLE_LOCK_FD:-} != 9 || $(readlink /proc/$$/fd/9 2>/dev/null || true) != "$lock" ]]; then
  [[ ! -L "$lock" && (! -e "$lock" || -f "$lock") ]] || exit 1
  (umask 022; : >> "$lock")
  chown root:root "$lock"
  chmod 0644 "$lock"
  exec 9<>"$lock"
fi
flock -n 9 || { echo 'Maintenance in progress; retry later.' >&2; exit 1; }
export SLURM_CONSOLE_LOCK_FD=9
"$binary" pam configure --dir=/var/lib/slurm-console/pb_data
if systemctl is-active --quiet slurm-console; then
  systemctl restart slurm-console
  "$binary" healthcheck
fi
echo 'PAM configured and verified from the release bundle.'
