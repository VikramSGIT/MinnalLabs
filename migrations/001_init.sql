CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    username VARCHAR(255) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users(deleted_at);

CREATE TABLE IF NOT EXISTS homes (
    id SERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    user_id INTEGER NOT NULL REFERENCES users(id),
    name VARCHAR(255) NOT NULL,
    wifi_ssid VARCHAR(255) DEFAULT '',
    wifi_password VARCHAR(255) DEFAULT '',
    mqtt_username VARCHAR(255) DEFAULT '',
    mqtt_password VARCHAR(255) DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_homes_deleted_at ON homes(deleted_at);
CREATE INDEX IF NOT EXISTS idx_homes_user_id ON homes(user_id);

CREATE TABLE IF NOT EXISTS capabilities (
    id SERIAL PRIMARY KEY,
    component VARCHAR(64) NOT NULL,
    trait_type VARCHAR(64) NOT NULL,
    writable BOOLEAN NOT NULL DEFAULT false,
    google_device_type VARCHAR(128) NOT NULL,
    UNIQUE(component, trait_type)
);

CREATE TABLE IF NOT EXISTS products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL
);

CREATE TABLE IF NOT EXISTS product_capabilities (
    product_id INTEGER NOT NULL REFERENCES products(id),
    capability_id INTEGER NOT NULL REFERENCES capabilities(id),
    esphome_key VARCHAR(128) NOT NULL,
    PRIMARY KEY (product_id, capability_id, esphome_key)
);

CREATE TABLE IF NOT EXISTS devices (
    id SERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    user_id INTEGER NOT NULL REFERENCES users(id),
    home_id INTEGER NOT NULL REFERENCES homes(id),
    product_id INTEGER NOT NULL REFERENCES products(id),
    name VARCHAR(255) NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_devices_deleted_at ON devices(deleted_at);
CREATE INDEX IF NOT EXISTS idx_devices_user_id ON devices(user_id);
CREATE INDEX IF NOT EXISTS idx_devices_home_id ON devices(home_id);

CREATE TABLE IF NOT EXISTS oauth_clients (
    id VARCHAR(255) PRIMARY KEY,
    secret VARCHAR(255) NOT NULL,
    domain VARCHAR(255) NOT NULL,
    user_id VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS oauth_tokens (
    id SERIAL PRIMARY KEY,
    client_id VARCHAR(255) NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    access TEXT NOT NULL,
    refresh TEXT NOT NULL,
    expires_in BIGINT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Seed capabilities
INSERT INTO capabilities (component, trait_type, writable, google_device_type) VALUES
    ('switch', 'OnOff', true, 'action.devices.types.SWITCH'),
    ('binary_sensor', 'MotionDetection', false, 'action.devices.types.SENSOR')
ON CONFLICT (component, trait_type) DO NOTHING;

-- Seed products
INSERT INTO products (name) VALUES ('ml-smart-sensor-v1')
ON CONFLICT (name) DO NOTHING;

-- Seed product capabilities
INSERT INTO product_capabilities (product_id, capability_id, esphome_key)
SELECT p.id, c.id, 'power'
FROM products p, capabilities c
WHERE p.name = 'ml-smart-sensor-v1' AND c.component = 'switch' AND c.trait_type = 'OnOff'
ON CONFLICT DO NOTHING;

INSERT INTO product_capabilities (product_id, capability_id, esphome_key)
SELECT p.id, c.id, 'presence'
FROM products p, capabilities c
WHERE p.name = 'ml-smart-sensor-v1' AND c.component = 'binary_sensor' AND c.trait_type = 'MotionDetection'
ON CONFLICT DO NOTHING;

-- Seed default OAuth client
INSERT INTO oauth_clients (id, secret, domain, user_id) VALUES
    ('google-client', 'my-secret-key', 'https://oauth-redirect.googleusercontent.com/', '1')
ON CONFLICT (id) DO NOTHING;
