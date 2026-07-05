# TVWH2K

## Multi-tenant Go backend that turns TradingView webhooks into Kraken orders, one connection per signed-up user

## Status
**Multi-tenant MVP**
- [x] Each user gets their own encrypted Kraken connection + unique webhook URL (`/webhooks/{token}`)
- [x] Signup/login handled by Supabase Auth (frontend); this backend verifies the resulting JWT
- [x] Kraken API keys (and optional Telegram bot token) encrypted at rest (AES-256-GCM)
- [x] Async signal processing via an in-process worker queue, so the webhook responds immediately
- [x] Per-connection test mode (dry-run orders via Kraken's `validate` flag)
- [x] Optional per-connection Telegram notifications

## Architecture

- **Auth + user table**: Supabase Auth. The frontend signs users up/in and gets a JWT; this backend verifies that JWT (`SUPABASE_JWT_SECRET`) on every authenticated API route.
- **Database**: Postgres (Supabase-hosted). Run `migrations/0001_init.sql` against your Supabase project before starting the app.
- **Encryption**: `security/crypto.go` — AES-256-GCM, key from `ENCRYPTION_KEY` (`openssl rand -base64 32`).
- **Webhook**: `POST /webhooks/{token}` — no shared secret anymore, the unguessable per-connection token in the URL is the auth. Get your own token via `POST /api/connections`.

## Configuration

Create a `.env` file based on `.env.example`:
```env
DATABASE_URL=postgres://postgres:password@db.your-project-ref.supabase.co:5432/postgres?sslmode=require
SUPABASE_JWT_SECRET=your-supabase-jwt-secret
ENCRYPTION_KEY=replace-with-openssl-rand--base64-32-output
API_BASE_URL=https://api.tradingbridge.online
FRONTEND_ORIGIN=https://tradingbridge.online
# Anything other than "false" keeps all Kraken orders validate-only (dry-run).
KRAKEN_TEST_MODE=true
PORT=8081
```

## API

### Public
- `POST /webhooks/{token}` — TradingView alert payload:
```json
{
  "text": "BUY Signal on BTCUSDT",
  "pair": "XBT/USD",
  "type": "buy",
  "ordertype": "limit",
  "volume": "0.001",
  "price": "95000",
  "close_ordertype": "stop-loss-limit",
  "close_price": "90000",
  "close_price2": "89500"
}
```

### Authenticated (`Authorization: Bearer <supabase JWT>`)
- `POST /api/connections` — body `{kraken_api_key, kraken_api_secret}` plus optional `{telegram_bot_token, telegram_chat_id}`; tests the credentials live, stores them encrypted, returns `{id, webhook_url, test_mode, telegram_enabled, status, created_at}`.
- `GET /api/connections` — list your connection(s): `[{id, webhook_url, test_mode, telegram_enabled, created_at}]` (no live Kraken check, to avoid rate-limiting — use the endpoint below for that).
- `PATCH /api/connections/{id}` — update Telegram notification settings: `{telegram_bot_token, telegram_chat_id}` (omit a field to leave it unchanged; empty string / 0 clears it).
- `POST /api/connections/{id}/test` — on-demand live credential check: `{status: "ok"|"invalid_credentials"}`.
- `GET /api/signals?connection_id=` — last 50 signals for that connection.
- `GET /api/trades?connection_id=` — last 50 trades for that connection; `status` is `executed`, `validated` (test mode dry-run) or `failed`.

## Test mode / going live
All orders are sent to Kraken with the `validate` flag (dry-run) unless the server is started with `KRAKEN_TEST_MODE=false`. This is a server-wide kill-switch; the per-connection `test_mode` column also forces dry-run when set.

## Docker
- `docker compose up --build`

## Local Development
1. Create a Supabase project, run `migrations/0001_init.sql` against it (SQL editor or `psql "$DATABASE_URL" -f migrations/0001_init.sql`).
2. Copy `.env.example` to `.env` and fill in real values.
3. `go build && go run .`
