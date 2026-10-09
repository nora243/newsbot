CREATE TABLE IF NOT EXISTS designed_voices (
    cache_key text PRIMARY KEY,
    voice_id text NOT NULL,
    category text NOT NULL,
    expires_at timestamptz NOT NULL
);
