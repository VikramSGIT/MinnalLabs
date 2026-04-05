-- Identity data (username, password, email) now lives in Kratos.
-- The users table is a pure ID mapping: id <-> kratos_identity_id.
DROP INDEX IF EXISTS users_username_key;
DROP INDEX IF EXISTS idx_users_google_sub;
ALTER TABLE users DROP COLUMN IF EXISTS username;
ALTER TABLE users DROP COLUMN IF EXISTS password;
ALTER TABLE users DROP COLUMN IF EXISTS google_sub;
