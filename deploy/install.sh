#!/usr/bin/env bash
# Install only this application. External services remain administrator-managed.
set -euo pipefail
[[ $EUID -eq 0 && $(uname -s) == Linux ]] || { echo 'Run as root on Linux.' >&2; exit 1; }
[[ $# -eq 0 || ($# -eq 1 && $1 == --pam) ]] || { echo 'Usage: bash deploy/install.sh [--pam]' >&2; exit 1; }
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
for cmd in getent useradd id install systemctl cmp cp mv chmod chown mktemp flock diff find; do
  command -v "$cmd" >/dev/null || { echo "Required command: $cmd" >&2; exit 1; }
done
for artifact in backend/slurm-console frontend/dist/index.html; do
  [[ -f "$repo_dir/$artifact" && ! -L "$repo_dir/$artifact" ]] || { echo "Missing/unsafe build artifact: $artifact" >&2; exit 1; }
done
"$repo_dir/backend/slurm-console" backup --help >/dev/null
if [[ ${1:-} == --pam ]]; then
  for cmd in awk; do
    command -v "$cmd" >/dev/null || { echo "Required command: $cmd" >&2; exit 1; }
  done
  [[ -f "$repo_dir/backend/slurm-console-pam-helper" || -x /usr/local/libexec/slurm-console-pam-helper ]] || { echo 'Build the Linux PAM helper: make pam-helper' >&2; exit 1; }
  [[ -f /etc/pam.d/password-auth ]] || { echo 'Rocky/RHEL password-auth policy is required.' >&2; exit 1; }
fi
lock=/run/lock/slurm-console.lock
[[ ! -L "$lock" && (! -e "$lock" || -f "$lock") ]] || exit 1
(umask 022; : >> "$lock")
chown root:root "$lock"
chmod 0644 "$lock"
exec 9<>"$lock"
flock -n 9 || { echo 'Maintenance in progress; retry later.' >&2; exit 1; }
export SLURM_CONSOLE_LOCK_FD=9
if ! getent passwd slurm-console >/dev/null; then
  useradd --system --user-group --home-dir /var/lib/slurm-console --shell /sbin/nologin slurm-console
fi
[[ $(id -u slurm-console) != 0 ]] || exit 1
for target in /opt/slurm-console /var/lib/slurm-console /var/lib/slurm-console/pb_data /etc/slurm-console; do
  [[ ! -L "$target" && (! -e "$target" || -d "$target") ]] || { echo "Unsafe target: $target" >&2; exit 1; }
done
install -d -o root -g root -m 0755 /opt
# Preserve existing ownership until the stopped-service snapshot is verified.
[[ -d /etc/slurm-console ]] || install -d -o root -g root -m 0700 /etc/slurm-console
for target in /var/lib/slurm-console /var/lib/slurm-console/pb_data; do
  [[ -d "$target" ]] || install -d -o root -g root -m 0700 "$target"
done
"$repo_dir/backend/slurm-console" backup init-key
was_active=false
systemctl is-active --quiet slurm-console && was_active=true
new_timer=true
[[ ! -f /etc/systemd/system/slurm-console-backup.timer ]] || new_timer=false
changed=false
[[ -z $(find /var/lib/slurm-console /etc/slurm-console \( ! -user root -o ! -group root \) -print -quit) ]] || changed=true
for pair in 'backend/slurm-console /opt/slurm-console/slurm-console' 'deploy/slurm-console.service /etc/systemd/system/slurm-console.service' 'deploy/slurm-console-backup.service /etc/systemd/system/slurm-console-backup.service' 'deploy/slurm-console-backup.timer /etc/systemd/system/slurm-console-backup.timer'; do
  read -r source target <<< "$pair"
  cmp -s "$repo_dir/$source" "$target" || changed=true
done
diff -qr "$repo_dir/frontend/dist" /opt/slurm-console/public >/dev/null 2>&1 || changed=true
if [[ ${1:-} == --pam && -f "$repo_dir/backend/slurm-console-pam-helper" ]]; then
  cmp -s "$repo_dir/backend/slurm-console-pam-helper" /usr/local/libexec/slurm-console-pam-helper || changed=true
fi
release=$(mktemp -d /opt/.slurm-console-release-XXXXXXXX)
previous="$release.previous"
backup=""
mutated=false
stopped=false
cleanup() {
  result=$?
  trap - EXIT
  if (( result != 0 )); then
    if "$mutated" && [[ -n "$backup" ]]; then
      echo 'Install failed; restoring the verified pre-upgrade snapshot.' >&2
      systemctl stop slurm-console || true
      if "$repo_dir/backend/slurm-console" backup restore --file "$backup" --apply --confirm RESTORE; then
        rm -rf -- "$previous"
        if "$was_active"; then systemctl start slurm-console && /opt/slurm-console/slurm-console healthcheck || true; fi
      else
        echo "Automatic rollback failed; preserved release: $previous; encrypted snapshot: $backup" >&2
      fi
    elif "$stopped" && "$was_active"; then
      systemctl start slurm-console || true
    fi
  else
    rm -rf -- "$previous"
  fi
  rm -rf -- "$release"
  exit "$result"
}
trap cleanup EXIT
install -o root -g root -m 0755 "$repo_dir/backend/slurm-console" "$release/slurm-console"
install -d -o root -g root -m 0755 "$release/public"
cp -R "$repo_dir/frontend/dist/." "$release/public/"
[[ -z $(find "$release/public" ! -type f ! -type d -print -quit) ]] || { echo 'Frontend contains unsupported file types.' >&2; exit 1; }
find "$release/public" -type d -exec chmod 0755 {} +
find "$release/public" -type f -exec chmod 0644 {} +
chown -R root:root "$release"
chmod -R go-w "$release"
chmod 0755 "$release"
if "$changed"; then
  if "$was_active"; then systemctl stop slurm-console; stopped=true; fi
  if [[ -f /var/lib/slurm-console/pb_data/data.db ]]; then
    backup="/var/backups/slurm-console/upgrade-$(date -u +%Y%m%dT%H%M%S)-$$.tar.gz.age"
    "$repo_dir/backend/slurm-console" backup create --kind upgrade --file "$backup"
  fi
  mutated=true
  [[ ! -e /opt/slurm-console ]] || mv -- /opt/slurm-console "$previous"
  mv -- "$release" /opt/slurm-console
fi
# The web process and all application data now belong to root. The dedicated
# slurm-console identity remains solely for Slurm auth/local socket requests.
chown -hR root:root /var/lib/slurm-console /etc/slurm-console
chmod 0711 /var/lib/slurm-console
chmod 0700 /var/lib/slurm-console/pb_data /etc/slurm-console
bash "$repo_dir/deploy/cleanup-sudoers.sh"
[[ -e /etc/slurm-console/slurm-console.env ]] || install -o root -g root -m 0640 "$repo_dir/deploy/slurm-console.env.example" /etc/slurm-console/slurm-console.env
[[ -e /etc/slurm-console/backup.env ]] || install -o root -g root -m 0600 "$repo_dir/deploy/backup.env.example" /etc/slurm-console/backup.env
for unit in slurm-console.service slurm-console-backup.service slurm-console-backup.timer; do
  [[ ! -L /etc/systemd/system/$unit ]] || exit 1
  install -o root -g root -m 0644 "$repo_dir/deploy/$unit" "/etc/systemd/system/$unit"
done
systemctl daemon-reload
if [[ ${1:-} == --pam ]]; then bash "$repo_dir/deploy/setup-pam.sh"; fi
if command -v restorecon >/dev/null; then
  restorecon -RF /opt/slurm-console /var/lib/slurm-console /etc/slurm-console /var/lib/slurm-console-backup /var/backups/slurm-console /etc/systemd/system/slurm-console*.service /etc/systemd/system/slurm-console-backup.timer
fi
systemctl enable slurm-console
if "$changed" || ! "$was_active"; then systemctl restart slurm-console; fi
systemctl is-active --quiet slurm-console
/opt/slurm-console/slurm-console healthcheck
if "$new_timer"; then systemctl enable --now slurm-console-backup.timer; else systemctl try-restart slurm-console-backup.timer; fi
echo 'Installed and ready: http://127.0.0.1:18090. Backup key: /var/lib/slurm-console-backup/identity.txt (keep an offline copy).'
