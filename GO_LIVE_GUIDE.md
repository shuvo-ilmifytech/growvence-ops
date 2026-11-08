# Go-Live Guide — Growvence Ops on your own VPS + domain

The recommended production setup. The project ships its own deploy scripts for exactly this:
Docker + Caddy (automatic Let's Encrypt HTTPS) + nightly backups. Budget: roughly
**$5–6/month** for the VPS + ~$10–15/year for the domain.

> Already rebranded: the app shows **Growvence Tech / Growvence Ops** everywhere.
> The example domain below is `portal.growvence.com` — replace with your real domain.

## Architecture (what runs where)

```
Browser ──HTTPS──▶ Caddy (:80/:443, auto TLS)
                      ├─ /api/*, /mcp, /oauth/* ─▶ Go API (:8080)
                      ├─ everything else ────────▶ Next.js web (:3000)
                      └─ files.portal… ─────────▶ MinIO (:9000, presigned URLs)
Postgres :5432 · Redis :6379 · (optional) coturn for voice calls
```

One origin serves app + API, so the session cookie stays first-party (`SameSite=Lax`, no CORS).

## Step 0 — What you need

- [ ] A VPS with **2 vCPU / 4 GB RAM minimum** (4 GB strongly recommended: Next.js build is hungry),
      40 GB+ disk. Hetzner CX32 (~€4.15/mo), DigitalOcean Basic ($6/mo), or any Docker-capable host.
- [ ] A domain you control (e.g. `growvene.com` — the app already uses `portal.growvence.com` as its example).
- [ ] Your SSH key.

## Step 1 — DNS (5 min + propagation)

At your domain registrar/DNS provider, add two **A records** pointing at the VPS IP:

| Host | Type | Value |
|---|---|---|
| `portal.growvence.com` | A | `YOUR_VPS_IP` |
| `files.portal.growvence.com` | A | `YOUR_VPS_IP` |

Verify: `nslookup portal.growvence.com` returns your VPS IP before continuing.

## Step 2 — Put the code on the server

```bash
ssh your-user@YOUR_VPS_IP
sudo mkdir -p /srv && sudo chown $USER:$USER /srv
git clone <your-repo-url> /srv/growvence-ops   # or: scp the project folder up
cd /srv/growvence-ops
```

> The deploy scripts default to `/srv/growvence-ops`. Override with `REPO=/somewhere/else`.

## Step 3 — Harden the server (as root)

`deploy/01-harden.sh` is written for **Arch Linux** (`pacman`). On Arch, run as root:

```bash
sudo bash deploy/01-harden.sh
```

On **Ubuntu/Debian**, do the equivalent by hand (the script is a good checklist):
1. `apt update && apt install -y docker.io docker-compose-plugin ufw fail2ban unattended-upgrades`
2. `systemctl enable --now docker`
3. Create a non-root admin user, add to `docker` group, copy your SSH key.
4. `ufw default deny incoming && ufw default allow outgoing && ufw allow 22/tcp && ufw allow 80/tcp && ufw allow 443/tcp && ufw --force enable`
5. Disable password SSH logins (`PasswordAuthentication no` in `/etc/ssh/sshd_config`), keep key-only.

## Step 4 — Deploy (one command)

```bash
cd /srv/growvence-ops
MODE=domain APP_HOST=portal.growvence.com FILES_HOST=files.portal.growvence.com \
  bash deploy/02-deploy.sh
```

What it does, in order:
1. Writes `.env` with **fresh random secrets** (never overwrites an existing `.env` — secrets survive redeploys).
2. `docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --profile app build && up -d`
   (Postgres, Redis, MinIO, API, web, Caddy; the API runs DB migrations on start via `AUTO_MIGRATE=true`).
3. Validates + reloads the Caddyfile. Caddy fetches **Let's Encrypt certificates automatically** on first HTTPS hit and redirects http → https.
4. Waits for `http://127.0.0.1:8080/healthz` → 200 and the web app to answer.
5. Installs the **nightly backup timer** (`growvence-ops-backup.timer`, 02:30 Asia/Dhaka → `/var/backups/growvence-ops`).

First build takes several minutes. If anything fails, the script prints which service and its logs.

## Step 5 — Create the first account (CEO)

```bash
cd /srv/growvence-ops
docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --profile cli run --rm api-cli \
  create-admin -email ceo@growvence.com -name "Your Name" -role ceo
```

Then open `https://portal.growvence.com`, sign in, and create the team under **HR → People**
(HR role, Team Leaders, members). The CEO account is read-only on project data by design.

## Step 6 — Verify go-live (checklist)

- [ ] `https://portal.growvence.com` loads with a valid certificate, shows the Growvence logo + tab title.
- [ ] Sign in as CEO works; sign out/in as another user works.
- [ ] Create a test project, upload a file (tests MinIO presigned URLs via `files.portal.growvence.com`).
- [ ] `https://portal.growvence.com/mcp` responds (401 without a token = correct).
- [ ] `systemctl list-timers growvence-ops-backup.timer` shows the backup scheduled; run
      `sudo /srv/growvence-ops/deploy/backup.sh` once by hand and confirm `/var/backups/growvence-ops/` fills.
- [ ] Copy one backup off the server (`scp`) — a backup that never leaves the box isn't a backup.

## Step 7 — Ongoing operations

| Task | How |
|---|---|
| Redeploy after `git pull` | Re-run the same `02-deploy.sh` command — it's idempotent |
| Auto-deploy on push to `main` | Add the repo's CI deploy key as in `deploy/ci-deploy.sh` (forced command, can only deploy) |
| Logs | `docker compose --profile app logs -f api` (or `web`, `caddy`) |
| Change a secret/setting | Edit `/srv/growvence-ops/.env`, re-run `02-deploy.sh` |
| Restore DB from backup | `pg_restore` into a fresh stack — practice once before you need it |
| Voice calls behind strict NAT | Add the `turn` service from `deploy/docker-compose.prod.yml` + `TURN_*` env (see TASKS.md 13.8) |
| Google Sheet project backup | Set `SHEETS_SPREADSHEET_ID` + service-account key in `.env` (see `.env.example`) |

## Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| Caddy never gets a certificate | DNS not pointing at the VPS yet, or port 80 blocked by firewall |
| `files.portal…` uploads fail | `S3_PUBLIC_ENDPOINT` in `.env` must exactly match `FILES_HOST`; re-run deploy after fixing |
| Page loads but login loops | `APP_ORIGINS` must be exactly `https://portal.growvence.com`; `COOKIE_SECURE=true` needs HTTPS |
| API unhealthy after deploy | `docker compose --profile app logs api` — usually Postgres still starting; the script waits 2 min |
| Out of memory during build | Next.js build wants ~2 GB free; add swap or build on a bigger box once |

## Honest limits of this setup
- Single server = single point of failure. Fine for a team portal; not HA.
- The backup timer covers DB + uploaded files. It does **not** copy backups off-site by itself — add that.
- `MODE=ip` exists for testing without DNS, but sessions cross the network in clear text — never use it with real data.
