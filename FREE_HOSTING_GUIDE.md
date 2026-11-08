# Free Hosting Guide — Growvence Ops for $0/month

Run the full stack on free tiers: **Vercel** (web) + **Fly.io** (API) + **Neon** (Postgres) +
**Upstash** (Redis) + **Cloudflare R2** (file storage). No credit card needed for any of them
at the free tier (verify current terms when you sign up — free tiers change).

> The app is already rebranded to **Growvence Tech / Growvence Ops**. Nothing below changes the brand.

## How it fits together

```
Browser ──HTTPS──▶ Vercel (Next.js web)
                      └─ /api/* rewritten server-side ─▶ Fly.io (Go API :8080)
                                                            ├─ Neon (Postgres)
                                                            ├─ Upstash (Redis: sessions, live alerts, jobs)
                                                            └─ Cloudflare R2 (S3-compatible file storage)
```

The browser only ever talks to your Vercel URL, so the session cookie stays first-party
(`SameSite=Lax`, no CORS headaches) — the same trick the Docker/Caddy setup uses.

## Step 1 — Postgres on Neon (free)

1. Sign up at neon.tech → New Project → name `growvence-ops`, Postgres 16, region closest to you (Singapore for BD).
2. Copy the **pooled** connection string. It looks like:
   `postgres://user:pass@ep-xxx.ap-southeast-1.aws.neon.tech/growvence?sslmode=require`
3. Save it as `DATABASE_URL`. (The API runs all migrations itself on boot via `AUTO_MIGRATE=true`.)

Free tier: 512 MB storage, 1 project — plenty for a team portal starting out.

## Step 2 — Redis on Upstash (free)

1. Sign up at upstash.com → Create Database → Regional, closest region.
2. Copy the **Redis URL** (`rediss://default:…@…upstash.io:6379`). Save as `REDIS_URL`.

Free tier: 10k commands/day. Fine for a small team; watch the dashboard — if you hit the cap,
sessions/live alerts stop working until the next day.

## Step 3 — File storage on Cloudflare R2 (free)

1. Sign up at cloudflare.com → R2 → Create bucket `growvence-ops`.
2. R2 → Manage R2 API tokens → Create token with Object Read & Write on that bucket.
   Save: `ACCOUNT_ID`, `ACCESS_KEY_ID`, `SECRET_ACCESS_KEY`.
3. Bucket → Settings → **Public access**: allow public access via the `r2.dev` subdomain
   (or attach your own domain). Save the public host, e.g. `pub-abc123.r2.dev`.

Free tier: 10 GB storage + generous egress — the most generous free object storage available.

## Step 4 — Go API on Fly.io (free allowance)

1. Install `flyctl`, `fly auth signup` / `fly auth login`.
2. In the repo root (or `backend/`), create `fly.toml`:
   ```toml
   app = "growvence-ops-api"
   primary_region = "sin"   # Singapore, closest to Dhaka

   [http_service]
     internal_port = 8080
     force_https = true
     auto_stop_machines = true    # scale to zero when idle (free)
     auto_start_machines = true
     min_machines_running = 0
     [http_service.checks]
       grace_period = "30s"
       path = "/healthz"
   ```
3. Set secrets (never commit these):
   ```bash
   fly secrets set \
     DATABASE_URL="postgres://..." \
     REDIS_URL="rediss://..." \
     APP_ORIGINS="https://growvence-ops.vercel.app" \
     COOKIE_SECURE="true" \
     AUTO_MIGRATE="true" \
     S3_ENDPOINT="https://<ACCOUNT_ID>.r2.cloudflarestorage.com" \
     S3_PUBLIC_ENDPOINT="pub-abc123.r2.dev" \
     S3_PUBLIC_SSL="true" \
     S3_USE_SSL="true" \
     S3_REGION="auto" \
     S3_BUCKET="growvence-ops" \
     S3_ACCESS_KEY="<R2_ACCESS_KEY_ID>" \
     S3_SECRET_KEY="<R2_SECRET_ACCESS_KEY>" \
     MAX_UPLOAD_MB="100"
   ```
   > `APP_ORIGINS` must exactly match your Vercel URL (step 5) — update it after you know the URL,
   > then `fly deploy` again.
4. `fly deploy` (uses `backend/Dockerfile`). First boot runs DB migrations automatically.
5. Note your API URL: `https://growvence-ops-api.fly.dev`. Check `…/healthz` → 200.

**Why Fly.io and not Render free?** The API listens on `API_ADDR` (default `:8080`) and its Docker
image is distroless (no shell), so it can't read Render's dynamic `$PORT`. Fly.io lets you fix the
internal port in `fly.toml` — no code change. (If you insist on Render: patch
`backend/internal/config/config.go` line 39 to `Addr: env("API_ADDR", ":"+env("PORT", "8080"))`,
and override the Docker command to `sh -c` — not recommended, the image has no shell.)

## Step 5 — Web frontend on Vercel (free)

1. Push this repo to GitHub (private is fine).
2. vercel.com → Add New Project → import the repo → **Root Directory: `frontend`** → Deploy.
3. In the project's Settings → Environment Variables, add:
   - `API_INTERNAL_URL` = `https://growvence-ops-api.fly.dev` (your Fly URL from step 4)
4. Redeploy so the variable takes effect.

That's it — Vercel builds Next.js natively. `/api/*` is rewritten server-side to your Fly API,
so login cookies work exactly like the VPS setup.

## Step 6 — Create your admin account

```bash
fly ssh console -C "/usr/local/bin/opsctl create-admin -email ceo@growvence.com -name 'Your Name' -role ceo"
```
(Or `seed-dev` instead of `create-admin` for throwaway demo users: `admin@opsflow.local / password123!` etc. — see README.)

## Step 7 — Verify

- [ ] `https://<your-app>.vercel.app/login` shows the Growvence logo and green brand color.
- [ ] Sign in as the admin; create a test project; upload a file (tests R2 presigned URLs end-to-end).
- [ ] `https://<your-app>.vercel.app/mcp` → 401 without token (correct).
- [ ] Neon dashboard shows tables created (migrations ran).

## Honest limits of the free setup

| Limit | What it means for you |
|---|---|
| Fly.io machines sleep when idle | First request after idle takes ~10–30 s to wake. Fine for a team portal; annoying for demos. Set `min_machines_running = 1` to avoid it (uses free allowance faster). |
| Upstash 10k commands/day | Sessions + live alerts + background jobs share this. A team of ~10 is fine; watch usage. |
| Neon 512 MB | Fine for months of projects; files live in R2, not the DB. |
| No TURN server | Voice calls work browser-to-browser on normal networks; calls behind strict mobile/office NAT may not connect (the VPS guide's coturn solves this). |
| No automatic backups | Neon has point-in-time recovery on paid tiers; on free, run `pg_dump` yourself weekly. R2 files: enable versioning or copy the bucket periodically. |
| Cold GitHub → Vercel builds | Each push rebuilds; ~2–4 min. |

## When to graduate to the VPS guide
Switch to `GO_LIVE_GUIDE.md` when: the team grows past ~10 active users, Upstash/Neon caps get hit,
you want voice calls to always work (TURN), or you want nightly backups and a custom domain with
zero cold starts. The `.env` values map 1:1 — nothing in the app changes.
