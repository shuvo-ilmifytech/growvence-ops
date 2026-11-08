#!/usr/bin/env bash
# Forced command for the CI deploy key (see authorized_keys command="...").
# Whatever the client asks to run is ignored — $SSH_ORIGINAL_COMMAND is never
# consulted — so a leaked CI key can do this and nothing else. No shell, no
# port forwarding, no file copy.
set -euo pipefail

cd /srv/opsflow

echo "== fetching origin/main"
git fetch --prune --quiet origin
git reset --hard --quiet origin/main
git --no-pager log --oneline -1

echo "== deploying"
exec bash deploy/02-deploy.sh
