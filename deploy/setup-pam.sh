#!/usr/bin/env bash
# Configure only this application's PAM integration. Safe to run repeatedly.
set -euo pipefail
[[ $EUID -eq 0 && $# -eq 0 && $(uname -s) == Linux ]] || { echo 'Run as root on Linux, without arguments.' >&2; exit 1; }
for cmd in getent id install cmp visudo runuser sudo systemctl awk flock; do
  command -v "$cmd" >/dev/null || { echo "Required command: $cmd" >&2; exit 1; }
done
[[ -f /etc/pam.d/password-auth ]] || { echo 'Rocky/RHEL password-auth policy is required.' >&2; exit 1; }
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
id slurm-console >/dev/null 2>&1 || { echo 'Run deploy/install.sh first.' >&2; exit 1; }
[[ $(id -u slurm-console) != 0 ]] || exit 1
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
helper=/usr/local/libexec/slurm-console-pam-helper
policy=/etc/pam.d/slurm-console
sudoers=/etc/sudoers.d/slurm-console-pam
environment=/etc/slurm-console/slurm-console.env
changed=false
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
install -d -o root -g root -m 0755 /usr/local/libexec
install -d -o root -g slurm-console -m 0750 /etc/slurm-console
for target in "$helper" "$policy" "$sudoers" "$environment"; do
  [[ ! -L "$target" && (! -e "$target" || -f "$target") ]] || { echo "Refusing unsafe target: $target" >&2; exit 1; }
done
if [[ -f "$repo_dir/backend/slurm-console-pam-helper" ]] && ! cmp -s "$repo_dir/backend/slurm-console-pam-helper" "$helper"; then
  install -o root -g root -m 0755 "$repo_dir/backend/slurm-console-pam-helper" "$helper.new"
  mv -f -- "$helper.new" "$helper"
  changed=true
fi
[[ -x "$helper" ]] || { echo 'Build the Linux PAM helper: make pam-helper' >&2; exit 1; }
chown root:root "$helper"
chmod 0755 "$helper"
# Preserve administrator-customized application policies and all shared policies.
if [[ ! -e "$policy" ]]; then
  cat > "$work/pam" <<'POLICY'
#%PAM-1.0
auth       include      password-auth
account    required     pam_nologin.so
account    include      password-auth
POLICY
  install -o root -g root -m 0644 "$work/pam" "$policy"
  changed=true
fi
chown root:root "$policy"
chmod 0644 "$policy"
cat > "$work/sudoers" <<'SUDOERS'
# Only this root-owned helper, without command-line arguments.
slurm-console ALL=(root) NOPASSWD: /usr/local/libexec/slurm-console-pam-helper ""
SUDOERS
visudo -cf "$work/sudoers" >/dev/null
if ! cmp -s "$work/sudoers" "$sudoers"; then
  install -o root -g root -m 0440 "$work/sudoers" "$sudoers"
  changed=true
fi
chown root:root "$sudoers"
chmod 0440 "$sudoers"
visudo -c >/dev/null
# The systemd EnvironmentFile is data, never sourced by this root shell.
if [[ -f "$environment" ]]; then
  awk '!/^[[:space:]]*SLURM_CONSOLE_PAM_ENABLED=/' "$environment" > "$work/env"
fi
printf '%s\n' 'SLURM_CONSOLE_PAM_ENABLED=true' >> "$work/env"
if ! cmp -s "$work/env" "$environment"; then
  install -o root -g slurm-console -m 0640 "$work/env" "$environment"
  changed=true
fi
chown root:slurm-console "$environment"
chmod 0640 "$environment"
if command -v restorecon >/dev/null; then
  restorecon "$helper" "$policy" "$sudoers" "$environment"
fi
result=$(runuser -u slurm-console -- /usr/bin/sudo -n -- "$helper" <<<'{"mode":"probe"}')
[[ "$result" == '{"ok":true}' ]] || { echo 'PAM helper probe failed.' >&2; exit 1; }
if "$changed" && systemctl is-active --quiet slurm-console; then
  systemctl restart slurm-console
fi
echo 'PAM configured and verified. Existing application policy and external services preserved.'
