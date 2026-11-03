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
- **Database**: Postgres (Supabase-hosted). Run `migrations/0001_init.sql` and `migrations/0002_live_enabled.sql` against your Supabase project before starting the app.
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

## DCA runner (optional, own account)
Off by default; does not touch the webhook. Averages down losing **spot** holdings (no margin) by buying `DCA_AMOUNT_EUR` of the EUR pair when unrealized PnL is below the threshold. Positions come from account balances; cost basis is the volume-weighted average entry from trade history (average-cost: sells reduce volume at the average cost, buy fees count as cost). EUR, stablecoins, staked assets and dust below `DCA_MIN_VALUE_EUR` are skipped, as is any asset whose balance exceeds the trade-history volume (deposits/transfers: unknown basis). Orders are validate-only while `KRAKEN_TEST_MODE` is not `false`.

Known limitations: the cooldown is in-memory (lost on restart, so a restart can trigger another buy); overlaps with tribot `cmd/portfolio`, which already does spot DCA, so do not run both on the same account.

| Env var | Default | Effect |
|---|---|---|
| `DCA_ENABLED` | `false` | set `true` to start the runner (needs `KRAKEN_API_KEY`, `KRAKEN_API_SECRET`) |
| `DCA_THRESHOLD` | `-10.0` | PnL % below which a buy triggers |
| `DCA_AMOUNT_EUR` | required | quote amount per DCA buy (EUR pairs only) |
| `DCA_COOLDOWN_HOURS` | `24` | min hours between DCA buys per position |
| `DCA_MIN_VALUE_EUR` | `5` | holdings worth less are ignored as dust |
| `DCA_INTERVAL_MINUTES` | `5` | check interval |

## Webhook hardening
`POST /webhooks/{token}` is protected by (all env vars optional):

| Env var | Default | Effect |
|---|---|---|
| `WEBHOOK_TOKEN_RATE_PER_MIN` / `WEBHOOK_TOKEN_BURST` | `30` / `10` | per-token token bucket, 429 + `Retry-After` when empty |
| `WEBHOOK_IP_RATE_PER_MIN` / `WEBHOOK_IP_BURST` | `120` / `30` | per-client-IP token bucket |
| `WEBHOOK_DEDUP_WINDOW` | `60s` | identical payload (or same optional `idempotency_id`) for the same token inside the window returns 200 `{"status":"duplicate"}` and is not enqueued; `0` disables |
| `WEBHOOK_MAX_BODY_BYTES` | `16384` | larger bodies get 413 |
| `WEBHOOK_MAX_VOLUME` | `1000000` | volume must be a plain decimal, > 0 and <= this, else 400 |
| `WEBHOOK_TRUST_PROXY` | `false` | when `true`, client IP is the last `X-Forwarded-For` hop (only behind a trusted proxy) |

Signals are validated: `type` must be buy/sell, `pair` alphanumeric (optionally `BASE/QUOTE`), `ordertype` a known Kraken type, price fields numeric; payloads without pair/type/volume are accepted as text-only notifications. Limits are in-memory and per process.

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
All orders are sent to Kraken with the `validate` flag (dry-run) unless ALL of these hold: the server runs with `KRAKEN_TEST_MODE=false`, the connection's `test_mode` is false, and the connection has opted in with `live_enabled` (default false; existing connections stay dry-run). Set it via `POST /api/connections` or `PATCH /api/connections/{id}` with `{"live_enabled": true, "confirm_live": true}`; `live_enabled: true` without `confirm_live: true` is rejected with 400. Apply `migrations/0002_live_enabled.sql` before deploying.

## Docker
- `docker compose up --build`

## Local Development
1. Create a Supabase project, run `migrations/0001_init.sql` and then `migrations/0002_live_enabled.sql` against it (SQL editor or `psql "$DATABASE_URL" -f migrations/0001_init.sql -f migrations/0002_live_enabled.sql`).
2. Copy `.env.example` to `.env` and fill in real values.
3. `go build && go run .`
