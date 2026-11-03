# TVWH2K on Synology via Portainer (short guide)

Stack file: `portainer-stack.yml` (paste into Portainer > Stacks > Add stack > Web editor). Variable list is in its header comment. Frontend is NOT in the stack (stays on its current hosting). `KRAKEN_TEST_MODE` is hard-coded `true`.

## 1. API image
`ghcr.io/scubafly/tvwh2k:latest` is public (anonymous pull works, linux/amd64) but was built 2026-08-30 and may predate current source. To use current source, build on the Mac:
```
cd TVWH2K
docker buildx build --platform linux/amd64 -t tvwh2k:local --load .   # arm64 NAS: --platform linux/arm64
docker save tvwh2k:local | gzip > tvwh2k-local.tar.gz
scp tvwh2k-local.tar.gz <nas-user>@<nas>:/volume1/docker/tradingbridge/
```
Portainer > Images > Import > upload the `.tar.gz`. Then set stack variable `API_IMAGE=tvwh2k:local`. (Or push: `docker tag tvwh2k:local ghcr.io/scubafly/tvwh2k:<tag> && docker push ...`, then `API_IMAGE=ghcr.io/scubafly/tvwh2k:<tag>`.)

## 2. Folders on the NAS (SSH as admin, or File Station)
```
B=/volume1/docker/tradingbridge
sudo mkdir -p $B/postgres $B/backups $B/caddy-data $B/caddy-config $B/config
sudo chown 999:999 $B/postgres && sudo chmod 700 $B/postgres   # postgres:16 runs as uid/gid 999
sudo chown 0:0 $B/backups $B/caddy-data $B/caddy-config         # backup + caddy containers run as root
sudo chmod 755 $B/backups $B/caddy-data $B/caddy-config
```
GoTrue (uid 1000) and the API (distroless nonroot) write nothing to disk. If File Station is used, still run the `chown 999:999` for `postgres` over SSH. Different base path: set variable `TB_BASE`.

## 3. Cloudflare tunnel
1. Zero Trust > Networks > Tunnels > Create tunnel (Cloudflared). Copy the token -> `TUNNEL_TOKEN`.
2. Public hostnames (both Service type `HTTP`, URL `caddy:80`): `api.<domain>` and `auth.<domain>`.
3. DNS CNAMEs are created automatically for these; no router port-forwarding.

## 4. Variables and secrets
```
openssl rand -hex 24      # POSTGRES_PASSWORD
openssl rand -hex 32      # JWT_SECRET
openssl rand -base64 32   # ENCRYPTION_KEY (back it up; losing it loses stored Kraken keys)
```
Also `AUTH_HOST`, `API_HOST`, `SITE_URL` (frontend origin), `TUNNEL_TOKEN`, optional `SMTP_*` (Gmail: host `smtp.gmail.com`, port 587, an app password; then `GOTRUE_MAILER_AUTOCONFIRM=false`).

## 5. Deploy order
Deploy the single stack once with "Deploy the stack". Compose orders it: postgres -> gotrue -> migrate (waits for `auth.users`, applies SQL once, tracked in `tb_migrations`) -> api, backup -> caddy -> cloudflared. Check Portainer: `migrate` Exited (0), everything else running. Redeploys are safe (migrations are skipped).

## 6. Frontend build values
`VITE_API_URL=https://api.<domain>`, `VITE_SUPABASE_URL=https://auth.<domain>` (no suffix), and the anon key (HS256 JWT, role anon, signed with the secret; read from env, never stored):
```
export JWT_SECRET=...   # same as the stack
h=$(printf '{"alg":"HS256","typ":"JWT"}' | openssl base64 -A | tr '+/' '-_' | tr -d =)
p=$(printf '{"role":"anon","iss":"tvwh2k","exp":4102444800}' | openssl base64 -A | tr '+/' '-_' | tr -d =)
s=$(printf '%s.%s' $h $p | openssl dgst -sha256 -hmac "$JWT_SECRET" -binary | openssl base64 -A | tr '+/' '-_' | tr -d =)
echo "$h.$p.$s"
```

## 7. Close signup after your first account
Create your account via the frontend (or the curl below). Then in Portainer > Stack > Environment variables set `GOTRUE_DISABLE_SIGNUP=true` > Update the stack (re-deploy). Verify the signup curl now fails (403).

## 8. Smoke tests
```
curl -s https://auth.<domain>/health                      # 200
curl -s https://auth.<domain>/auth/v1/settings            # JSON, disable_signup flag
curl -s -X POST https://auth.<domain>/auth/v1/signup -H 'Content-Type: application/json' -d '{"email":"me@example.com","password":"<long password>"}'
curl -s -o /dev/null -w '%{http_code}\n' https://api.<domain>/api/connections                 # 401
curl -s -o /dev/null -w '%{http_code}\n' https://api.<domain>/webhooks/x                      # 405 on GET
curl -s -H "Authorization: Bearer <access_token>" https://api.<domain>/api/connections       # 200 []
```

## 9. Backups (Portainer only)
The `backup` service writes `<db>_<timestamp>.dump` (pg_dump -Fc) every 24 h (first one at deploy) into `$B/backups`, keeping `BACKUP_RETENTION_DAYS` (14). Files are root-owned; copy them off the NAS too. Manual dump: Portainer > Containers > `tvwh2k-postgres-1` > Console (`/bin/bash`): `pg_dump -U postgres -Fc tvwh2k > /var/lib/postgresql/data/manual.dump` (then move it), or just restart the `backup` container to force a dump. The old `backup.sh` also still works over SSH (`PG_CONTAINER=<postgres container name>`).

## 10. Restore
1. Portainer: stop `api`, `gotrue`, `backup`.
2. Test first into a scratch DB (SSH): 
   `docker exec <pg> psql -U postgres -c 'create database scratch'`
   `docker exec -i <pg> pg_restore -U postgres -d scratch --no-owner < FILE.dump`
3. Real restore: `docker exec -i <pg> pg_restore -U postgres -d tvwh2k --clean --if-exists --no-owner < FILE.dump`
4. Start `gotrue`, `api`, `backup`. (`<pg>` = postgres container name from Portainer, e.g. `tvwh2k-postgres-1`.)
