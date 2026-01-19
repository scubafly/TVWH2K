# TVWH2K
## A Simple application written in golang that handles webhook from tradingview and sends the data to kraken using the kraken api

## Status
**MVP Implemented**
- [x] Output signal to telegram
- [x] Fire order to kraken after webhook call
- [x] Make connection to kraken
- [x] Make option to test the connection instead of placing an order (via `KRAKEN_TEST_MODE`)
- [x] Create something that calls AddOrder (Supports `ordertype`, `price`, `price2` for stop loss/take profit)

## Configuration
Create a `.env` file based on `.env.example`:
```env
TELEGRAM_BOT_TOKEN=...
TELEGRAM_CHAT_ID=...
TOKEN=your-webhook-secret
KRAKEN_API_KEY=...
KRAKEN_API_SECRET=...
KRAKEN_TEST_MODE=true  # Set to false to execute real orders
```

## Usage
The application expects a JSON payload at `POST /webhooks`.

### Payload Format
```json
{
  "token": "your-webhook-secret",
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

### Supported Fields
- `token`: Must match `TOKEN` env var.
- `text`: Message sent to Telegram.
- `pair`: Kraken asset pair (e.g. `XBT/USD`).
- `type`: `buy` or `sell`.
- `ordertype`: `market`, `limit`, `stop-loss`, etc.
- `volume`: Amount to trade.
- `price`: Limit price or stop price.
- `price2`: Secondary price (optional).

## API
 The application exposes two read-only endpoints for external dashboards:
- `GET /api/signals`: Returns the last 50 received webhook signals.
- `GET /api/trades`: Returns the last 50 executed trades with their status and PnL.

## Docker
- `docker compose up --build`

## Local Development
- `go build && go run .`
