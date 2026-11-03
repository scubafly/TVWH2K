-- Per-connection live-trading opt-in. Existing connections stay dry-run.
ALTER TABLE connections ADD COLUMN IF NOT EXISTS live_enabled BOOLEAN NOT NULL DEFAULT false;
