# Self-hosted NAS runbook (Postgres + GoTrue + API + Caddy + cloudflared)

Files: `compose.selfhost.yml`, `Caddyfile`, `.env.selfhost.example`, `initdb/`, `migrations/0003_selfhost_auth_shim.sql`, `backup.sh`.
Existing `../../compose.nas.yml` / `../../NAS-DEPLOY.md` (API only, external DB) are untouched. The repo-level migrations `../../migrations/0001`, `0002` are reused.

## Architecture
Browser -> Cloudflare -> tunnel -> cloudflared -> Caddy (:80, internal) -> gotrue:9999 / tvwh2k:8081.
No ports are published; the only outbound connection is cloudflared to Cloudflare. Postgres is only on the internal docker network.

The browser talks to GoTrue directly, so GoTrue must be publicly reachable (`auth.<domain>`) and the API too (`api.<domain>`, also used by TradingView webhooks).
supabase-js requests `<VITE_SUPABASE_URL>/auth/v1/...`, but GoTrue serves at root (`/signup`, `/token`, `/user`). Caddy strips `/auth/v1` for `auth.<domain>`.
Frontend: `VITE_SUPABASE_URL=https://auth.<domain>` (no suffix). The "anon key" supabase-js requires can be any JWT signed with `SUPABASE_JWT_SECRET` (GoTrue does not check it); generate one, e.g. with jwt.io HS256 payload `{"role":"anon"}`.

## Steps
1. Copy `deploy/nas/` to the NAS (e.g. `/volume1/docker/tvwh2k`). `cp .env.selfhost.example .env.selfhost; chmod 600 .env.selfhost`.
2. Generate secrets: `openssl rand -hex 24` (POSTGRES_PASSWORD), `openssl rand -hex 32` (SUPABASE_JWT_SECRET, shared by GoTrue and API), `openssl rand -base64 32` (ENCRYPTION_KEY). Fill hostnames. Keep `KRAKEN_TEST_MODE=true`.
3. Cloudflare Zero Trust -> Networks -> Tunnels -> create tunnel (Docker), copy the token into `CLOUDFLARE_TUNNEL_TOKEN`. Add public hostnames, both with service `HTTP` `caddy:80`:
   - `auth.<domain>` and `api.<domain>`.
   Caddy routes by Host header (`AUTH_HOST` / `API_HOST`). Don't open any router port-forward.
4. Start DB + auth first (creates `auth.users`):
   `docker compose --env-file .env.selfhost -f compose.selfhost.yml up -d postgres gotrue`
   Wait ~20s until `docker exec tvwh2k-pg psql -U postgres -d tvwh2k -c '\dt auth.users'` shows the table.
5. Migrations in this order (0001 FKs need `auth.users`; the shim needs nothing but is last):
   ```
   for f in ../../migrations/0001_init.sql ../../migrations/0002_live_enabled.sql migrations/0003_selfhost_auth_shim.sql; do
     docker exec -i tvwh2k-pg psql -v ON_ERROR_STOP=1 -U postgres -d tvwh2k < "$f"; done
   ```
   (0001 is not idempotent for policies; run once.) The shim defines `auth.uid()` from `request.jwt.claim.sub`; never run it on Supabase.
6. Start everything: `docker compose --env-file .env.selfhost -f compose.selfhost.yml up -d` (pull first: `... pull`).
7. Backups: `chmod +x backup.sh`, run once, add cron (see comments in `backup.sh`; DSM Task Scheduler works too). Test a restore on a scratch DB.

## Restart behaviour
All services use `restart: unless-stopped`: they come back after NAS/docker restarts. The API and GoTrue wait for Postgres health at `up`. Data lives in the `pgdata` volume.

## Uptime monitoring
- Uptime Kuma / healthchecks.io HTTP check on `https://auth.<domain>/health` (GoTrue, expects 200).
- API has no /health route: check `GET https://api.<domain>/webhooks/healthcheck` expects 405 (route exists, POST-only) via Uptime Kuma "accepted status codes 405".
- Optionally a cron on the NAS that curls both and pings a healthchecks.io URL on success.

## Verify
- `docker compose ... ps`: all Up, postgres healthy; `docker ps` shows no published ports.
- `curl https://auth.<domain>/auth/v1/settings` returns JSON (`disable_signup:false`; with autoconfirm `mailer_autoconfirm:true`).
- Sign up from the frontend; user appears in `auth.users`; API calls with the returned access token succeed (no 401).
- Create a connection; POST to the webhook URL returns dry-run result (KRAKEN_TEST_MODE=true).
- `./backup.sh` produces a dump; `pg_restore --list` reads it.
- Set `GOTRUE_MAILER_AUTOCONFIRM=false` and configure SMTP only if you want email confirmation.

## Notes
- Image tags: GoTrue pinned to `supabase/gotrue:v2.188.1`; cloudflared is `latest` (bump deliberately).
- The API connects as the `postgres` superuser (bypasses RLS, as the code expects).
