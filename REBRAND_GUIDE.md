# Rebrand Guide — Growvence Ops (Growvence Tech)

This project was rebranded from **OpsFlow (Solvenix Tech)** to **Growvence Ops (Growvence Tech)**.
This guide lists **every place** the brand lives, what was already changed for you,
and how to change each piece yourself in the future.

## What the rebrand changed (already done)

| # | What the user sees | File | Old → New |
|---|---|---|---|
| 1 | Browser tab titles | `frontend/src/app/layout.tsx` | `Solvenix Tech Operations` → `Growvence Tech Operations`, `%s · Solvenix Tech` → `%s · Growvence Tech` |
| 2 | Logo image | `frontend/public/logo-mark.png` | His real Growvence lockup from Drive ("Growvence Logo" folder): white `pure-white-no-bg.png` for dark surfaces; `logo-mark-dark.png` (full-color) for light surfaces |
| 3 | Logo component (white/dark variants) | `frontend/src/components/brand.tsx` | `LogoMark` (white lockup, dark surfaces) + `LogoMarkDark` (full-color, light surfaces); sidebar + login use the full lockup, text name removed (logo carries it) |
| 4 | Sidebar brand name | `frontend/src/components/app-shell.tsx` | `Growvence Tech` (+ `Operations` subtitle) |
| 5 | Login page brand (desktop + mobile) | `frontend/src/app/login/page.tsx` | `Growvence Tech`; email placeholder `you@growvence.com` |
| 6 | HR → People page description | `frontend/src/app/(app)/hr/people/page.tsx` | "Everyone at Growvence Tech…" |
| 7 | AI-assistant connector card | `frontend/src/components/account/assistants-card.tsx` | "Growvence Ops"; fallback endpoint `https://portal.growvence.com/mcp` |
| 8 | Brand color (buttons, accents) | `frontend/src/app/globals.css` | `--brand` was Solvinx orange `#FF7300` → Growvence green `#46C34C` |
| 9 | Favicons / app icons | `frontend/src/app/icon.png`, `apple-icon.png` | GX mark cropped from his real logo on a navy rounded square |
| 10 | OAuth consent page brand | `backend/internal/httpapi/mcp_handlers.go` | `Growvence Ops · Growvence Tech` |
| 11 | MCP server title + AI instructions | `backend/internal/mcp/server.go`, `mcp_handlers.go` | `Growvence Ops (Growvence Tech)` |
| 12 | Example domains in docs/deploy | `README.md`, `deploy/Caddyfile`, `deploy/02-deploy.sh`, `assistants-card.tsx` | `portal.solvenixtech.com` → `portal.growvence.com`, `files.` → `files.` |
| 13 | Code comments mentioning the company | `backend/migrations/*.sql`, `backend/internal/db/queries/*.sql`, `frontend/src/lib/types.ts`, `project-form.tsx` | "Solvenix" → "Growvence" |
| 14 | Compose project / server paths | `docker-compose.yml`, `deploy/*.sh`, `deploy/*.service`, `deploy/*.timer` | `opsflow` → `growvence-ops`, `/srv/growvence-ops`, `/var/backups/growvence-ops` |

## How to change each piece yourself

### 1. Company / product name (text)
The visible name lives in exactly 7 UI spots. Search the repo for `Growvence` to find them all:

```bash
grep -rn "Growvence" frontend/src backend/internal --include="*.tsx" --include="*.ts" --include="*.go" | grep -v "_test.go"
```

Key spots: `frontend/src/app/layout.tsx` (tab titles), `frontend/src/components/app-shell.tsx`
(sidebar), `frontend/src/app/login/page.tsx` (login page), `frontend/src/components/brand.tsx`
(logo alt text), `backend/internal/mcp/server.go` (AI assistant title), `backend/internal/httpapi/mcp_handlers.go`
(OAuth consent page + `resource_name`).

### 2. Logo
1. Export your logo as a **square PNG** (transparent background works best on the dark sidebar).
2. Overwrite `frontend/public/logo-mark.png`.
3. Update `width=`/`height=` in `frontend/src/components/brand.tsx` to the real pixel size
   (the `h-7 w-auto` class handles display size; the props only set the aspect ratio).
4. Regenerate favicons from the same file:
   `icon.png` → 512×512, `apple-icon.png` → 180×180, both in `frontend/src/app/`.
5. Rebuild the frontend (`docker compose --profile app up -d --build web`, or `npm run build`).

### 3. Brand color
One variable drives buttons, active states and accents: `--brand` in `frontend/src/app/globals.css`
(currently Growvence green `#46C34C` = `oklch(0.73 0.185 143)`). Change that one line, rebuild the
frontend. The dark theme overrides `--primary` separately, so check both themes after changing.

### 4. Domain / email placeholders
Example domains live in `deploy/Caddyfile` (comments), `deploy/02-deploy.sh` (comments),
`README.md` (MCP endpoint examples) and the login page placeholder (`you@growvence.com` in
`frontend/src/app/login/page.tsx`). Your **real** domain is set at deploy time via `.env`
(`APP_SITE`, `FILES_SITE`) — see `GO_LIVE_GUIDE.md`. The placeholders are only examples.

### 5. Page subtitle under the logo
`frontend/src/components/app-shell.tsx` — the small `Operations` text under the brand name.
Change it to anything (e.g. your tagline).

## What was deliberately NOT changed (safe to leave)

These are invisible to users; renaming them adds risk with zero visible benefit:

| Untouched | Why |
|---|---|
| Go module path `github.com/solvinx/opsflow` | Internal import path only; renaming means touching every `.go` file |
| DB user / database / S3 bucket / passwords (`opsflow`, `opsflow-secret`) | Functional credentials, overridable via `.env` (`POSTGRES_PASSWORD`, `S3_BUCKET`, …) |
| Cookie name `opsflow_session`, localStorage keys `opsflow.ring.*` | Renaming logs everyone out / resets mute settings |
| MCP `serverName = "opsflow"`, CLI snippets (`OPSFLOW_TOKEN`, `[mcp_servers.opsflow]`) | Arbitrary local labels the user types; backend doesn't validate them |
| Demo seed emails `admin@opsflow.local` etc. | Throwaway demo data only (`opsctl seed-dev`) |
| `TASKS.md` history entries | Dated log of what happened; only the H1 title was updated |

If you ever want the full internal rename anyway: `grep -rn "opsflow" --include="*.go" backend | wc -l`
shows ~80 files — do it with `gofmt` + full test suite (`cd backend && go test ./...`) afterwards,
plus `sqlc generate` if you touch `queries/*.sql`.

## Verify the rebrand
1. `grep -rni "solvenix" . --exclude-dir=node_modules --exclude-dir=.git --exclude="package-lock.json" | grep -v "github.com/solvinx"` → should print nothing.
2. Rebuild + open the app: tab title, sidebar, login page, favicon, and an OAuth consent page
   (`/oauth/authorize…`, reachable via the Account → AI assistants card) all show the new brand.
3. `cd backend && go build ./...` and `cd frontend && npm run build` still pass.
