-- Verification and password-reset tokens are now stored as their SHA-256 hex
-- digest, so a leaked database (or backup) holds no working links. SQLite has
-- no built-in SHA-256, so existing plaintext tokens cannot be converted in
-- place: drop them. An unopened link stops working and the user requests a
-- new one; used reset rows were only kept for diagnostics.
DELETE FROM email_verifications;
DELETE FROM password_resets;

ALTER TABLE email_verifications RENAME COLUMN token TO token_hash;
ALTER TABLE password_resets RENAME COLUMN token TO token_hash;
