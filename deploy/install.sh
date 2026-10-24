#!/usr/bin/env bash
# Install this application only. Dependencies are installed by the administrator.
set -euo pipefail
[[ $EUID -eq 0 && $(uname -s) == Linux ]] || { echo 'Run as root on Linux.' >&2; exit 1; }
[[ $# -eq 0 || ($# -eq 1 && $1 == --pam) ]] || { echo 'Usage: bash deploy/install.sh [--pam]' >&2; exit 1; }
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
for cmd in getent useradd id install systemctl tar cmp; do
  command -v "$cmd" >/dev/null || { echo "Required command: $cmd" >&2; exit 1; }
done
for artifact in backend/slurm-console frontend/dist/index.html; do
  [[ -f "$repo_dir/$artifact" ]] || { echo "Missing build artifact: $artifact" >&2; exit 1; }
done
if [[ ${1:-} == --pam ]]; then
  for cmd in visudo sudo runuser awk; do
    command -v "$cmd" >/dev/null || { echo "Required command: $cmd" >&2; exit 1; }
  done
  [[ -f "$repo_dir/backend/slurm-console-pam-helper" || -x /usr/local/libexec/slurm-console-pam-helper ]] || { echo 'Build the Linux PAM helper: make pam-helper' >&2; exit 1; }
  [[ -f /etc/pam.d/password-auth ]] || { echo 'Rocky/RHEL password-auth policy is required.' >&2; exit 1; }
fi
if ! getent passwd slurm-console >/dev/null; then
  useradd --system --user-group --home-dir /var/lib/slurm-console --shell /sbin/nologin slurm-console
fi
[[ $(id -u slurm-console) != 0 ]] || exit 1
install -d -o root -g root -m 0755 /opt/slurm-console
install -d -o root -g slurm-console -m 0750 /etc/slurm-console
install -d -o slurm-console -g slurm-console -m 0700 /var/lib/slurm-console /var/lib/slurm-console/pb_data
if ! cmp -s "$repo_dir/backend/slurm-console" /opt/slurm-console/slurm-console && [[ -e /var/lib/slurm-console/pb_data/data.db ]]; then
  systemctl stop slurm-console
  install -d -o root -g root -m 0700 /var/backups/slurm-console
  backup="/var/backups/slurm-console/upgrade-$(date -u +%Y%m%dT%H%M%S)-$$.tar.gz"
  (umask 077; tar -czf "$backup" -C / var/lib/slurm-console/pb_data opt/slurm-console etc/slurm-console)
  echo "Pre-upgrade backup: $backup"
fi
install -o root -g root -m 0755 "$repo_dir/backend/slurm-console" /opt/slurm-console/slurm-console.new
mv -f -- /opt/slurm-console/slurm-console.new /opt/slurm-console/slurm-console
install -d -o root -g root -m 0755 /opt/slurm-console/public
cp -R "$repo_dir/frontend/dist/." /opt/slurm-console/public/
chown -R root:root /opt/slurm-console/public
chmod -R go-w /opt/slurm-console/public
[[ -e /etc/slurm-console/slurm-console.env ]] || install -o root -g slurm-console -m 0640 "$repo_dir/deploy/slurm-console.env.example" /etc/slurm-console/slurm-console.env
install -o root -g root -m 0644 "$repo_dir/deploy/slurm-console.service" /etc/systemd/system/slurm-console.service
systemctl daemon-reload
if [[ ${1:-} == --pam ]]; then
  bash "$repo_dir/deploy/setup-pam.sh"
fi
if command -v restorecon >/dev/null; then
  restorecon -RF /opt/slurm-console /var/lib/slurm-console /etc/slurm-console /etc/systemd/system/slurm-console.service
fi
systemctl enable slurm-console
systemctl restart slurm-console
systemctl is-active --quiet slurm-console
echo 'Installed: http://127.0.0.1:18090. Provision platform accounts with the users/access CLI.'
