CREATE TABLE IF NOT EXISTS firmware_rollouts (
    id SERIAL PRIMARY KEY,
    product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    target_version VARCHAR(64) NOT NULL,
    firmware_filename VARCHAR(255) NOT NULL,
    firmware_md5 VARCHAR(64) NOT NULL,
    batch_percentage INTEGER NOT NULL,
    batch_interval_minutes INTEGER NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    next_batch_at TIMESTAMPTZ,
    created_by_user_id INTEGER NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_firmware_rollouts_status_next_batch_at
    ON firmware_rollouts(status, next_batch_at);
CREATE INDEX IF NOT EXISTS idx_firmware_rollouts_product_id
    ON firmware_rollouts(product_id);

CREATE TABLE IF NOT EXISTS firmware_rollout_devices (
    rollout_id INTEGER NOT NULL REFERENCES firmware_rollouts(id) ON DELETE CASCADE,
    device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    batch_number INTEGER NOT NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'pending',
    sent_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    retained_cleared_at TIMESTAMPTZ,
    last_reported_version VARCHAR(64) NOT NULL DEFAULT '',
    PRIMARY KEY (rollout_id, device_id)
);

CREATE INDEX IF NOT EXISTS idx_firmware_rollout_devices_device_id
    ON firmware_rollout_devices(device_id);
CREATE INDEX IF NOT EXISTS idx_firmware_rollout_devices_state
    ON firmware_rollout_devices(state);
