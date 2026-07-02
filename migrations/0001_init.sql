-- Multi-tenant schema for TVWH2K, run against the Supabase Postgres project.
-- Supabase already manages auth.users; these tables reference it directly.

CREATE TABLE IF NOT EXISTS connections (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    exchange TEXT NOT NULL DEFAULT 'kraken',
    kraken_api_key_encrypted BYTEA NOT NULL,
    kraken_api_secret_encrypted BYTEA NOT NULL,
    webhook_token TEXT NOT NULL UNIQUE,
    test_mode BOOLEAN NOT NULL DEFAULT true,
    telegram_bot_token_encrypted BYTEA,
    telegram_chat_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_connections_user_id ON connections(user_id);

CREATE TABLE IF NOT EXISTS signals (
    id BIGSERIAL PRIMARY KEY,
    connection_id BIGINT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    pair TEXT,
    type TEXT,
    payload JSONB
);

CREATE INDEX IF NOT EXISTS idx_signals_connection_id ON signals(connection_id, received_at DESC);

CREATE TABLE IF NOT EXISTS trades (
    id BIGSERIAL PRIMARY KEY,
    signal_id BIGINT REFERENCES signals(id) ON DELETE SET NULL,
    connection_id BIGINT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    pair TEXT,
    type TEXT,
    ordertype TEXT,
    volume TEXT,
    price TEXT,
    txid TEXT,
    status TEXT NOT NULL DEFAULT 'open',
    pnl NUMERIC NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_trades_connection_id ON trades(connection_id, created_at DESC);

-- Row-level security: the Go backend connects as a role that bypasses RLS
-- (or via the postgres/service role), but these policies are defense in depth
-- in case anything ever queries through Supabase's PostgREST/anon key directly.
ALTER TABLE connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE signals ENABLE ROW LEVEL SECURITY;
ALTER TABLE trades ENABLE ROW LEVEL SECURITY;

CREATE POLICY connections_owner_only ON connections
    USING (user_id = auth.uid());

CREATE POLICY signals_owner_only ON signals
    USING (connection_id IN (SELECT id FROM connections WHERE user_id = auth.uid()));

CREATE POLICY trades_owner_only ON trades
    USING (connection_id IN (SELECT id FROM connections WHERE user_id = auth.uid()));

-- Reserved for future billing (Stripe) integration -- not used by any code yet.
CREATE TABLE IF NOT EXISTS user_profiles (
    user_id UUID PRIMARY KEY REFERENCES auth.users(id) ON DELETE CASCADE,
    stripe_customer_id TEXT,
    subscription_status TEXT NOT NULL DEFAULT 'free',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
