#!/usr/bin/env bash
# Enable the bounded local account broker for one explicitly selected cluster.
set -euo pipefail
[[ $EUID -eq 0 && $(uname -s) == Linux && $# -eq 1 && $1 =~ ^[a-zA-Z0-9_-]{1,64}$ ]] || { echo 'Usage: sudo bash deploy/setup-linux-users.sh <cluster-record-id>' >&2; exit 1; }
for cmd in python3 id visudo install systemctl flock runuser cmp chmod chgrp cp; do command -v "$cmd" >/dev/null || exit 1; done
[[ -x /usr/sbin/useradd && -x /usr/sbin/usermod && -x /usr/bin/getent && -x /usr/bin/nsenter ]] || { echo "Install Linux shadow-utils and glibc utilities first." >&2; exit 1; }
[[ -x /opt/slurm-console/slurm-console ]] || { echo 'Install the application first.' >&2; exit 1; }
app_uid=$(id -u slurm-console)
[[ $app_uid != 0 ]] || exit 1
lock=/run/lock/slurm-console.lock
[[ ! -L "$lock" ]] || exit 1
(umask 022; : >> "$lock")
chmod 0644 "$lock"
exec 9<>"$lock"
flock -n 9 || { echo 'Maintenance is active; retry later.' >&2; exit 1; }
work=$(mktemp -d)
targets=(/etc/slurm-console/linux-users-admin.json /etc/slurm-console/linux-users.json /etc/slurm-console/slurm-console.env /etc/sudoers.d/slurm-console-linux-users)
for i in "${!targets[@]}"; do
  [[ ! -L "${targets[$i]}" ]] || exit 1
  if [[ -e "${targets[$i]}" ]]; then cp -p -- "${targets[$i]}" "$work/previous-$i"; else touch "$work/absent-$i"; fi
done
changed=false
cleanup() {
  result=$?
  trap - EXIT
  if (( result != 0 )) && "$changed"; then
    for i in "${!targets[@]}"; do
      if [[ -f "$work/previous-$i" ]]; then cp -p -- "$work/previous-$i" "${targets[$i]}"; else rm -f -- "${targets[$i]}"; fi
    done
    systemctl try-restart slurm-console || true
  fi
  rm -rf -- "$work"
  exit "$result"
}
trap cleanup EXIT
cat > "$work/sudoers" <<'SUDOERS'
# One fixed subcommand. It independently enforces a root-owned UID policy.
slurm-console ALL=(root) NOPASSWD: /usr/bin/nsenter --mount=/proc/1/ns/mnt -- /opt/slurm-console/slurm-console linux-users-admin
SUDOERS
visudo -cf "$work/sudoers" >/dev/null
python3 - "$1" "$app_uid" "$work" <<'PY'
import json,pathlib,sys,stat
cid,uid,work=sys.argv[1],int(sys.argv[2]),pathlib.Path(sys.argv[3])
root=pathlib.Path('/etc/slurm-console');root.mkdir(mode=0o750,exist_ok=True)
policy=root/'linux-users-admin.json';profiles=root/'linux-users.json';env=root/'slurm-console.env'
for target in [policy,profiles,env,pathlib.Path('/etc/sudoers.d/slurm-console-linux-users')]:
 if target.is_symlink() or target.exists() and not target.is_file():raise SystemExit('Unsafe application configuration target')
if not policy.exists():
 (work/'policy').write_text(json.dumps({'enabled':True,'callerUid':uid,'minUid':10000,'maxUid':59999,'protectedUsers':['root','consoleadmin','slurm-console','slurm','munge']},indent=2)+'\n')
else:
 info=policy.stat();assert info.st_uid==0 and stat.S_IMODE(info.st_mode)&0o077==0,'Existing root policy has unsafe permissions'
 config=json.loads(policy.read_text());assert config['callerUid']==uid,'Existing caller UID policy differs; review manually'
registry=json.loads(profiles.read_text()) if profiles.exists() else {'clusters':{}}
source=registry['clusters'].get(cid,{'label':'本机 NSS 身份','transport':'local'})
assert source['transport']=='local','SSH sources remain read-only'
source['manageLocal']=True;registry['clusters'][cid]=source
(work/'profiles').write_text(json.dumps(registry,ensure_ascii=False,indent=2)+'\n')
lines=env.read_text().splitlines() if env.exists() else []
desired='SLURM_CONSOLE_LINUX_USERS_PROFILES=/etc/slurm-console/linux-users.json'
updated=[];found=False
for line in lines:
 if line.startswith('SLURM_CONSOLE_LINUX_USERS_PROFILES='):
  if not found:updated.append(desired);found=True
 else:updated.append(line)
if not found:updated.append(desired)
(work/'env').write_text('\n'.join(updated)+'\n')
PY
for pair in "policy /etc/slurm-console/linux-users-admin.json" "profiles /etc/slurm-console/linux-users.json" "env /etc/slurm-console/slurm-console.env" "sudoers /etc/sudoers.d/slurm-console-linux-users"; do
 read -r source target <<< "$pair"
 [[ -f "$work/$source" ]] || continue
 mode=0640
 [[ $source != policy ]] || mode=0600
 [[ $source != sudoers ]] || mode=0440
 if ! cmp -s "$work/$source" "$target"; then
  install -o root -g root -m "$mode" "$work/$source" "$target"
  [[ $source == policy || $source == sudoers ]] || chgrp slurm-console "$target"
  changed=true
 fi
 chmod "$mode" "$target"
done
visudo -c >/dev/null
if command -v restorecon >/dev/null; then restorecon /etc/slurm-console/linux-users-admin.json /etc/slurm-console/linux-users.json /etc/slurm-console/slurm-console.env /etc/sudoers.d/slurm-console-linux-users; fi
result=$(runuser -u slurm-console -- /usr/bin/sudo -n -- /usr/bin/nsenter --mount=/proc/1/ns/mnt -- /opt/slurm-console/slurm-console linux-users-admin <<<'{"mode":"capabilities","spec":{"name":"","uid":0,"comment":"","shell":""}}')
python3 -c 'import json,sys; assert json.loads(sys.stdin.read())["allowed"]' <<< "$result"
if "$changed" && systemctl is-active --quiet slurm-console; then systemctl restart slurm-console; fi
/opt/slurm-console/slurm-console healthcheck
echo 'Local account management enabled. No OS accounts were created or changed.'
