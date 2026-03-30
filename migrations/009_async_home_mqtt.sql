ALTER TABLE homes
    ADD COLUMN IF NOT EXISTS mqtt_provision_state VARCHAR(32) NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS mqtt_provision_error TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS mqtt_provisioned_at TIMESTAMPTZ;

UPDATE homes
SET
    mqtt_provision_state = CASE
        WHEN deleted_at IS NOT NULL THEN 'deleting'
        WHEN COALESCE(TRIM(mqtt_username), '') <> '' AND COALESCE(TRIM(mqtt_password), '') <> '' THEN 'ready'
        ELSE 'failed'
    END,
    mqtt_provision_error = CASE
        WHEN COALESCE(TRIM(mqtt_username), '') = '' OR COALESCE(TRIM(mqtt_password), '') = '' THEN 'mqtt credentials missing'
        ELSE ''
    END,
    mqtt_provisioned_at = CASE
        WHEN COALESCE(TRIM(mqtt_username), '') <> '' AND COALESCE(TRIM(mqtt_password), '') <> '' AND mqtt_provisioned_at IS NULL
            THEN COALESCE(updated_at, created_at, NOW())
        ELSE mqtt_provisioned_at
    END;

CREATE TABLE IF NOT EXISTS home_mqtt_jobs (
    id SERIAL PRIMARY KEY,
    home_id INTEGER NOT NULL REFERENCES homes(id) ON DELETE CASCADE,
    operation VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_run_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_home_mqtt_jobs_home_operation
    ON home_mqtt_jobs(home_id, operation);

CREATE INDEX IF NOT EXISTS idx_home_mqtt_jobs_status_next_run_at
    ON home_mqtt_jobs(status, next_run_at);
