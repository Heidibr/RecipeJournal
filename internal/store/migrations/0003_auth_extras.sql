-- Email verification: users get a nullable timestamp; tokens live in their
-- own table with an expiry. Existing users are backfilled to "verified" so
-- nobody currently registered gets locked out of sharing the day this ships.

ALTER TABLE users ADD COLUMN email_verified_at DATETIME;

UPDATE users SET email_verified_at = created_at WHERE email_verified_at IS NULL;

CREATE TABLE IF NOT EXISTS email_verifications (
  token      TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at DATETIME NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_email_verifications_user ON email_verifications(user_id);

-- Password reset: tokens are single-use, time-limited. used_at lets us mark
-- a token consumed without immediately deleting it (helpful for diagnostics
-- if a user reports a "token already used" error).
CREATE TABLE IF NOT EXISTS password_resets (
  token      TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at DATETIME NOT NULL,
  used_at    DATETIME,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_password_resets_user ON password_resets(user_id);
