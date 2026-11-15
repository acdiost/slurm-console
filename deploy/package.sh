#!/usr/bin/env bash
# Prepare the frontend and installer payload before compiling the single binary.
set -euo pipefail
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
payload="$repo_dir/backend/internal/distribution/assets"
[[ -f "$repo_dir/frontend/dist/index.html" ]] || { echo 'Build the frontend first.' >&2; exit 1; }
[[ $# -le 1 ]] || { echo 'Usage: bash deploy/package.sh [amd64|arm64]' >&2; exit 1; }
helper_arch="${1:-$(go -C "$repo_dir/backend" env GOARCH)}"
case "$helper_arch" in amd64|arm64) ;; *) echo 'PAM release architecture must be amd64 or arm64.' >&2; exit 1 ;; esac
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT
(cd "$repo_dir/backend" && CGO_ENABLED=0 GOOS=linux GOARCH="$helper_arch" go build -trimpath -ldflags='-s -w' -o "$stage/slurm-console-pam-helper" ./cmd/pam-helper)
rm -rf -- "$payload/public" "$payload/deploy" "$payload/pam"
mkdir -p "$payload/public" "$payload/deploy" "$payload/pam"
cp -R "$repo_dir/frontend/dist/." "$payload/public/"
cp "$stage/slurm-console-pam-helper" "$payload/pam/slurm-console-pam-helper"
for file in install.sh cleanup-sudoers.sh slurm-console.service slurm-console-backup.service slurm-console-backup.timer slurm-console.env.example backup.env.example; do
  cp "$repo_dir/deploy/$file" "$payload/deploy/$file"
done
