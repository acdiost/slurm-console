#!/usr/bin/env bash
# Provision the host policy; select and enable each source through the web UI.
set -euo pipefail
[[ $EUID -eq 0 && $(uname -s) == Linux && $# -eq 0 ]] || { echo 'Usage: sudo bash deploy/setup-linux-users.sh' >&2; exit 1; }
for cmd in python3 install systemctl flock cmp chmod cp env; do command -v "$cmd" >/dev/null || exit 1; done
[[ $(systemctl show slurm-console -p User | cut -d= -f2-) == root ]] || { echo 'Upgrade with deploy/install.sh first: the application unit must run as root.' >&2; exit 1; }
[[ -x /usr/sbin/useradd && -x /usr/sbin/usermod && -x /usr/bin/getent ]] || { echo 'Install Linux shadow-utils and glibc utilities first.' >&2; exit 1; }
[[ -x /opt/slurm-console/slurm-console ]] || { echo 'Install the application first.' >&2; exit 1; }
lock=/run/lock/slurm-console.lock
[[ ! -L "$lock" ]] || exit 1
(umask 022; : >> "$lock")
chmod 0644 "$lock"
exec 9<>"$lock"
flock -n 9 || { echo 'Maintenance is active; retry later.' >&2; exit 1; }
work=$(mktemp -d)
policy=/etc/slurm-console/linux-users-admin.json
[[ ! -L "$policy" ]] || exit 1
if [[ -e "$policy" ]]; then cp -p -- "$policy" "$work/previous"; fi
changed=false
cleanup() {
  result=$?
  trap - EXIT
  if (( result != 0 )) && "$changed"; then
    if [[ -f "$work/previous" ]]; then cp -p -- "$work/previous" "$policy"; else rm -f -- "$policy"; fi
  fi
  rm -rf -- "$work"
  exit "$result"
}
trap cleanup EXIT
python3 - "$work/policy" <<'PY'
import json,pathlib,stat,sys
root=pathlib.Path('/etc/slurm-console')
assert not root.is_symlink(), 'Unsafe application configuration directory'
root.mkdir(mode=0o700,exist_ok=True)
assert root.stat().st_uid == 0 and root.stat().st_mode & 0o022 == 0, 'Unsafe application configuration directory'
policy=root/'linux-users-admin.json'
if policy.exists():
 info=policy.lstat()
 assert stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o077 == 0, 'Existing root policy has unsafe permissions'
else:
 pathlib.Path(sys.argv[1]).write_text(json.dumps({'enabled':True,'callerUid':0,'minUid':10000,'maxUid':59999,'protectedUsers':['root','consoleadmin','slurm-console','slurm','munge']},indent=2)+'\n')
PY
if [[ -f "$work/policy" ]] && ! cmp -s "$work/policy" "$policy"; then
  install -o root -g root -m 0600 "$work/policy" "$policy"
  changed=true
fi
if command -v restorecon >/dev/null; then restorecon "$policy"; fi
result=$(env -u SUDO_UID /opt/slurm-console/slurm-console linux-users-admin <<<'{"mode":"capabilities","spec":{"name":"","uid":0,"comment":"","shell":""}}')
python3 -c 'import json,sys; assert json.loads(sys.stdin.read())["allowed"], "Review the existing host policy; it was not changed"' <<< "$result"
if [[ -f /etc/sudoers.d/slurm-console-linux-users ]]; then
  bash "$(dirname -- "${BASH_SOURCE[0]}")/cleanup-sudoers.sh"
fi
/opt/slurm-console/slurm-console healthcheck
echo 'Host policy ready. In Linux users, configure a local identity source and explicitly enable local account management. No OS accounts were created or changed.'
