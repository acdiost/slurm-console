#!/usr/bin/env bash
# Prepare the frontend and installer payload before compiling the single binary.
set -euo pipefail
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
payload="$repo_dir/backend/internal/distribution/assets"
[[ -f "$repo_dir/frontend/dist/index.html" ]] || { echo 'Build the frontend first.' >&2; exit 1; }
rm -rf -- "$payload/public" "$payload/deploy"
mkdir -p "$payload/public" "$payload/deploy"
cp -R "$repo_dir/frontend/dist/." "$payload/public/"
for file in install.sh cleanup-sudoers.sh slurm-console.service slurm-console-backup.service slurm-console-backup.timer slurm-console.env.example backup.env.example; do
  cp "$repo_dir/deploy/$file" "$payload/deploy/$file"
done
