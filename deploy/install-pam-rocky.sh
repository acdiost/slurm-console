#!/usr/bin/env bash
set -euo pipefail

# Run from a trusted checkout after building the Linux binaries and frontend.
[[ $EUID -eq 0 ]] || { echo 'Run as root.' >&2; exit 1; }
[[ $# -eq 0 ]] || { echo 'This installer takes no arguments.' >&2; exit 1; }
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
for artifact in backend/slurm-console backend/slurm-console-pam-helper frontend/dist/index.html; do
  [[ -f "$repo_dir/$artifact" ]] || { echo "Missing build artifact: $artifact" >&2; exit 1; }
done
[[ -f /etc/pam.d/password-auth ]] || { echo 'Rocky/RHEL password-auth policy is required.' >&2; exit 1; }

if ! getent passwd slurm-console >/dev/null; then
  useradd --system --home-dir /var/lib/slurm-console --shell /sbin/nologin slurm-console
fi
[[ $(id -u slurm-console) != 0 ]] || { echo 'Service account must not be root.' >&2; exit 1; }
install -d -o root -g root -m 0755 /opt/slurm-console /usr/local/libexec
install -d -o slurm-console -g slurm-console -m 0700 /var/lib/slurm-console /var/lib/slurm-console/pb_data
install -o root -g root -m 0755 "$repo_dir/backend/slurm-console" /opt/slurm-console/slurm-console
install -o root -g root -m 0755 "$repo_dir/backend/slurm-console-pam-helper" /usr/local/libexec/slurm-console-pam-helper
install -d -o root -g root -m 0755 /opt/slurm-console/public
cp -R "$repo_dir/frontend/dist/." /opt/slurm-console/public/
chown -R root:root /opt/slurm-console/public
chmod -R go-w /opt/slurm-console/public
if [[ ! -e /etc/pam.d/slurm-console ]]; then
  install -o root -g root -m 0644 "$repo_dir/deploy/pam/slurm-console" /etc/pam.d/slurm-console
fi
visudo -cf "$repo_dir/deploy/pam/slurm-console-pam.sudoers"
install -o root -g root -m 0440 "$repo_dir/deploy/pam/slurm-console-pam.sudoers" /etc/sudoers.d/slurm-console-pam
visudo -c
install -o root -g root -m 0644 "$repo_dir/deploy/systemd/slurm-console.service" /etc/systemd/system/slurm-console.service
if command -v restorecon >/dev/null; then
  restorecon -RF /opt/slurm-console /usr/local/libexec/slurm-console-pam-helper /var/lib/slurm-console /etc/pam.d/slurm-console /etc/sudoers.d/slurm-console-pam /etc/systemd/system/slurm-console.service
fi
runuser -u slurm-console -- /usr/bin/sudo -n -- /usr/local/libexec/slurm-console-pam-helper <<<'{"mode":"probe"}' | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin).get("ok") else 1)'
systemctl daemon-reload
echo 'Installed. Provision a non-root PAM platform account, then systemctl enable --now slurm-console.'
