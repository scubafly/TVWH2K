-- Self-hosted only. Do NOT run on Supabase (it ships its own auth.uid()).
-- Lets the RLS policies from 0001 work on plain Postgres + GoTrue.
-- Run order: start postgres+gotrue (creates auth.users) -> 0001 -> 0002 -> this file.
CREATE SCHEMA IF NOT EXISTS auth;

CREATE OR REPLACE FUNCTION auth.uid() RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT nullif(current_setting('request.jwt.claim.sub', true), '')::uuid
$$;
