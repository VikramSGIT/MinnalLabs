CREATE INDEX IF NOT EXISTS idx_homes_user_id_deleted_at
    ON homes(user_id, deleted_at);

CREATE INDEX IF NOT EXISTS idx_devices_home_id_deleted_at
    ON devices(home_id, deleted_at);

CREATE INDEX IF NOT EXISTS idx_oauth_tokens_user_id
    ON oauth_tokens(user_id);
