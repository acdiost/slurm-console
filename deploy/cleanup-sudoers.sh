#!/usr/bin/env bash
# Remove only the exact rules installed by previous application releases.
set -euo pipefail
[[ $EUID -eq 0 && $# -eq 0 ]] || exit 1
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
cat > "$work/pam" <<'RULE'
# Only this root-owned helper, without command-line arguments.
slurm-console ALL=(root) NOPASSWD: /usr/local/libexec/slurm-console-pam-helper ""
RULE
cat > "$work/linux-users" <<'RULE'
# One fixed subcommand. It independently enforces a root-owned UID policy.
slurm-console ALL=(root) NOPASSWD: /usr/bin/nsenter --mount=/proc/1/ns/mnt -- /opt/slurm-console/slurm-console linux-users-admin
RULE
for name in pam linux-users; do
 target=/etc/sudoers.d/slurm-console-$name
 [[ ! -L "$target" && (! -e "$target" || -f "$target") ]] || exit 1
 if cmp -s "$work/$name" "$target"; then rm -- "$target"; fi
done
