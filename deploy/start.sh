#!/bin/sh
# Growvence Ops — single-container entrypoint (Render free web service).
# Starts the Go API on $API_ADDR (default :8080) in the background, waits
# until it is healthy (it runs DB migrations first), then execs the Next.js
# standalone server in the foreground on $PORT (injected by Render).
set -e

# One-time boot seed (2026-11-09): the live Neon DB starts empty and Render's
# Free plan has no Shell, so seed the login accounts here on first boot.
# seed-dev refuses when users exist, so this is a no-op after the first boot.
# REMOVE this block again once the accounts are verified (it must not stay in
# the production image: a future empty DB would re-seed demo logins).
/usr/local/bin/opsctl seed-dev || echo "growvence-ops: seed skipped (users may already exist)"

# Derive the API port from API_ADDR for the health check (:8080 -> 8080).
API_PORT=${API_ADDR##*:}
[ -n "$API_PORT" ] || API_PORT=8080

/usr/local/bin/api &
API_PID=$!

# Wait up to 60s for the API to become healthy. /healthz is 200 only when
# Postgres, Redis and object storage all answer. If the API process dies
# (bad DATABASE_URL, failed migration, unreachable storage) fail fast so
# Render restarts the container instead of serving a broken frontend.
i=0
while [ "$i" -lt 60 ]; do
  if wget -q -O /dev/null "http://localhost:$API_PORT/healthz" 2>/dev/null; then
    break
  fi
  if ! kill -0 "$API_PID" 2>/dev/null; then
    echo "growvence-ops: api exited during startup" >&2
    exit 1
  fi
  i=$((i + 1))
  sleep 1
done

exec node server.js
