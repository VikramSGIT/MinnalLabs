ALTER TABLE oauth_tokens
    ADD COLUMN IF NOT EXISTS code TEXT NOT NULL DEFAULT '';

ALTER TABLE oauth_tokens
    ADD COLUMN IF NOT EXISTS data TEXT NOT NULL DEFAULT '';

ALTER TABLE oauth_tokens
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

ALTER TABLE oauth_tokens
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS idx_oauth_tokens_code_lookup
    ON oauth_tokens(code)
    WHERE code <> '';

CREATE INDEX IF NOT EXISTS idx_oauth_tokens_access_lookup
    ON oauth_tokens(access)
    WHERE access <> '';

CREATE INDEX IF NOT EXISTS idx_oauth_tokens_refresh_lookup
    ON oauth_tokens(refresh)
    WHERE refresh <> '';

CREATE INDEX IF NOT EXISTS idx_oauth_tokens_expires_at
    ON oauth_tokens(expires_at);
