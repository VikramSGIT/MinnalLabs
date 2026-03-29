ALTER TABLE products
    ADD COLUMN IF NOT EXISTS firmware_url VARCHAR(512) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS firmware_md5_url VARCHAR(512) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS rollout_percentage INTEGER NOT NULL DEFAULT 20,
    ADD COLUMN IF NOT EXISTS rollout_interval_minutes INTEGER NOT NULL DEFAULT 60;

UPDATE products
SET rollout_interval_minutes = CASE
    WHEN rollout_interval_minutes = 60 AND rollout_delay_days > 0 THEN rollout_delay_days * 24 * 60
    ELSE rollout_interval_minutes
END;

ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS firmware_version VARCHAR(64) NOT NULL DEFAULT '';
