# NAS deploy (Synology Container Manager)

Run TVWH2K on a Synology NAS the same way as tribot: pull a GHCR image and start it with Docker Compose. Pushes to `main` build and publish `ghcr.io/scubafly/tvwh2k:latest` via GitHub Actions.

## Directory layout

Create a folder on the NAS (for example `/volume1/docker/tvwh2k`) with:

- `compose.nas.yml` (from this repo)
- `.env` (copy from `.env.example` and fill in real values)

Lock down secrets on the NAS:

```bash
chmod 600 .env
```

Required variables in `.env`:

- `DATABASE_URL`
- `SUPABASE_JWT_SECRET`
- `ENCRYPTION_KEY`
- `API_BASE_URL`
- `FRONTEND_ORIGIN`
- `KRAKEN_TEST_MODE` (leave `true` until you intentionally go live)
- `PORT` (`8081`)

## Pull and start

From the NAS directory:

```bash
docker compose -f compose.nas.yml pull
docker compose -f compose.nas.yml up -d
```

Check logs:

```bash
docker compose -f compose.nas.yml logs -f
```

## Updates

After a new image is pushed to GHCR, pull and recreate the container:

```bash
docker compose -f compose.nas.yml pull
docker compose -f compose.nas.yml up -d
```

If you use Watchtower (as with tribot), point it at the `latest` tag so the NAS picks up new builds automatically.

## Test mode

`KRAKEN_TEST_MODE` defaults to validate-only Kraken orders. Keep it `true` on the NAS until you are ready for live trading and set `API_BASE_URL` / `FRONTEND_ORIGIN` to the URLs your frontend and webhooks will use.
