CREATE TABLE IF NOT EXISTS oauth_clients (
    id VARCHAR(255) PRIMARY KEY,
    secret VARCHAR(255) NOT NULL,
    domain VARCHAR(255) NOT NULL,
    user_id VARCHAR(255) NOT NULL
);

INSERT INTO oauth_clients (id, secret, domain, user_id)
VALUES (
    'google-client',
    'stress-oauth-secret',
    'http://127.0.0.1/oauth/callback',
    '1'
)
ON CONFLICT (id) DO UPDATE SET
    secret = EXCLUDED.secret,
    domain = EXCLUDED.domain,
    user_id = EXCLUDED.user_id;
