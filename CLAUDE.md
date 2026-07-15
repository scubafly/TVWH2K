# TradeBridge — Claude Code Context

## Project
TradingView webhook → Kraken order execution + DCA bot (SaaS in ontwikkeling)
Repo: https://github.com/scubafly/TVWH2K

## Stack
- Golang (deze repo: webhook bridge + Kraken API)
- Supabase (auth + Postgres, nog te integreren)
- Frontend: Next.js of SvelteKit (aparte repo, nog te bouwen)

## Wat er al staat
- POST /webhooks — ontvangt TradingView webhook, voert Kraken order uit
- Kraken AddOrder (market, limit, stop-loss, stop-loss-limit)
- Telegram notificaties
- KRAKEN_TEST_MODE
- GET /api/signals + /api/trades (in-memory, laatste 50)
- Docker Compose

## Nog te bouwen (huidige sprint)
- GET /api/positions — Kraken OpenPositions uitlezen
- Position-based DCA logica (P&L% check → bijkooporder)
- DCA guard tegen dubbele orders

## Architectuur beslissingen
- MVP: single-tenant (.env keys), later multi-tenant via Supabase
- Geen GraphQL in MVP, security gaat voor
- RLS op alle Supabase tabellen verplicht
- Tests verplicht vóór live deployment (Playwright E2E + Golang testify)

## Code stijl
- Senior developer, geen basics uitleggen
- Directe antwoorden
- Bestaande patterns in codebase volgen

## Werkwijze & Review

### Na elke taak automatisch uitvoeren:
1. `go build ./...` — moet slagen zonder errors
2. `go vet ./...` — geen warnings
3. `go test ./...` — alle tests groen
4. Als tests falen: fix de **code**, niet de test
   - Uitzondering: als de test aantoonbaar incorrect is, leg uit waarom en vraag bevestiging

### Codereview checklist (vraag dit na elke sprint):
- Geen hardcoded credentials of tokens
- Errors altijd afgehandeld (niet `_` tenzij bewust)
- Nieuwe functies hebben een unit test
- Bestaande tests nog steeds groen
- Geen nieuwe dependencies zonder expliciete reden

### Tests schrijven:
- Elke nieuwe functie krijgt een unit test
- Gebruik table-driven tests (Go standaard patroon)
- Mock Kraken API calls in tests (geen echte calls in CI)
- Nooit een test aanpassen om hem te laten slagen zonder uitleg

## Git hooks
Pre-commit hook in `.githooks/pre-commit` — activeer na clone:
`git config core.hooksPath .githooks`

## code style
Code is geschreven in engels, functie namen etc hebben een omschrijvende naam waardoor het gebruik van comments overbodig wordt.
Clean code principes worden gebruikt ( Drive left, DRY, geen else statements als het even kan )
En de output van logs / debug etc is ook altijd engels.

