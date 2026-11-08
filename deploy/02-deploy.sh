#!/usr/bin/env bash
# Deploy (or redeploy) OpsFlow. Idempotent: re-run it after every `git pull`.
#
#   interim, no DNS:
#     MODE=ip SERVER_IP=187.77.145.198 bash deploy/02-deploy.sh
#   once DNS points here:
#     MODE=domain APP_HOST=portal.growvence.com \
#       FILES_HOST=files.portal.growvence.com bash deploy/02-deploy.sh
#   (to switch modes on a live box, edit the APP_SITE / FILES_SITE / APP_ORIGINS
#    / COOKIE_SECURE / S3_PUBLIC_* lines in .env and re-run — this script never
#    overwrites an existing .env, so the secrets survive)
#
# Secrets are generated once, on the first run, into .env at the repo root.
set -euo pipefail

REPO="${REPO:-/srv/opsflow}"
cd "$REPO"

COMPOSE=(docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --profile app)

log() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }

# ------------------------------------------------------------------ 1. the env
# MODE=ip      serve on the bare IP over http (no DNS needed, NOT secure:
#              sessions and uploads cross the network in clear text)
# MODE=domain  serve on real hostnames over https, certificates automatic
MODE="${MODE:-ip}"

if [[ -f .env ]]; then
    log ".env already exists — keeping it (and its secrets)"
    # shellcheck disable=SC1091
    set -a; . ./.env; set +a
else
    log "Writing .env with fresh secrets (MODE=$MODE)"
    case "$MODE" in
      ip)
        : "${SERVER_IP:?SERVER_IP is required when MODE=ip}"
        APP_SITE="http://$SERVER_IP"
        FILES_SITE="http://$SERVER_IP:9443"
        ORIGINS="http://$SERVER_IP"
        SECURE=false
        PUBLIC_ENDPOINT="$SERVER_IP:9443"
        PUBLIC_SSL=false
        ;;
      domain)
        : "${APP_HOST:?APP_HOST is required when MODE=domain}"
        : "${FILES_HOST:?FILES_HOST is required when MODE=domain}"
        APP_SITE="https://$APP_HOST"
        FILES_SITE="https://$FILES_HOST"
        ORIGINS="https://$APP_HOST"
        SECURE=true
        PUBLIC_ENDPOINT="$FILES_HOST"
        PUBLIC_SSL=true
        ;;
      *) echo "MODE must be 'ip' or 'domain'" >&2; exit 1 ;;
    esac

    gen() { openssl rand -base64 36 | tr -d '/+=\n' | cut -c1-32; }
    sed \
        -e "s|^APP_SITE=.*|APP_SITE=$APP_SITE|" \
        -e "s|^FILES_SITE=.*|FILES_SITE=$FILES_SITE|" \
        -e "s|^APP_ORIGINS=.*|APP_ORIGINS=$ORIGINS|" \
        -e "s|^COOKIE_SECURE=.*|COOKIE_SECURE=$SECURE|" \
        -e "s|^S3_PUBLIC_ENDPOINT=.*|S3_PUBLIC_ENDPOINT=$PUBLIC_ENDPOINT|" \
        -e "s|^S3_PUBLIC_SSL=.*|S3_PUBLIC_SSL=$PUBLIC_SSL|" \
        -e "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=$(gen)|" \
        -e "s|^S3_ACCESS_KEY=.*|S3_ACCESS_KEY=opsflow-$(gen | cut -c1-12)|" \
        -e "s|^S3_SECRET_KEY=.*|S3_SECRET_KEY=$(gen)|" \
        deploy/env.prod.example > .env
    chmod 600 .env
    set -a; . ./.env; set +a
fi
printf '    APP_SITE=%s\n    FILES_SITE=%s\n' "$APP_SITE" "$FILES_SITE"

# --------------------------------------------------------------- 2. build & up
log "Building images (this takes a few minutes the first time)"
"${COMPOSE[@]}" build

log "Starting the stack"
"${COMPOSE[@]}" up -d
"${COMPOSE[@]}" ps --format 'table {{.Service}}\t{{.Status}}'

# The Caddyfile is bind-mounted, so `up -d` doesn't restart Caddy when only the file
# changed. Validate it, then reload in place (no dropped connections).
log "Reloading Caddy's config"
"${COMPOSE[@]}" exec -T caddy caddy validate --config /etc/caddy/Caddyfile >/dev/null
"${COMPOSE[@]}" exec -T caddy caddy reload --config /etc/caddy/Caddyfile

# -------------------------------------------------------------- 3. wait for it
log "Waiting for the API to answer /healthz"
for i in $(seq 1 60); do
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:8080/healthz" || true)
    if [[ "$code" == "200" ]]; then printf '    healthy after %ss\n' "$((i*2))"; break; fi
    [[ $i -eq 60 ]] && { echo "    API never became healthy. Logs:"; "${COMPOSE[@]}" logs --tail=60 api; exit 1; }
    sleep 2
done

log "Waiting for the web app"
for i in $(seq 1 45); do
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:3000/" || true)
    if [[ "$code" =~ ^(200|307|308)$ ]]; then printf '    web answering (%s) after %ss\n' "$code" "$((i*2))"; break; fi
    [[ $i -eq 45 ]] && { echo "    web never answered. Logs:"; "${COMPOSE[@]}" logs --tail=40 web; exit 1; }
    sleep 2
done

# ------------------------------------------------------------- 4. the backup job
log "Nightly backup timer"
sudo install -m 644 deploy/opsflow-backup.service /etc/systemd/system/opsflow-backup.service
sudo install -m 644 deploy/opsflow-backup.timer   /etc/systemd/system/opsflow-backup.timer
sudo systemctl daemon-reload
sudo systemctl enable --now opsflow-backup.timer
systemctl list-timers opsflow-backup.timer --no-pager | sed 's/^/    /'

log "Deployed."
cat <<EOF
    App:    $APP_SITE
    Files:  $FILES_SITE

    First account (only needed once):
      ${COMPOSE[*]} --profile cli run --rm api-cli create-admin \\
          -email you@growvence.com -name 'Your Name' -role ceo
EOF
