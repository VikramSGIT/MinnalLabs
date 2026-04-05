ALTER TABLE users ADD COLUMN IF NOT EXISTS kratos_identity_id UUID;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_kratos_identity_id
  ON users(kratos_identity_id) WHERE kratos_identity_id IS NOT NULL;
