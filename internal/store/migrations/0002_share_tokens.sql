ALTER TABLE recipes ADD COLUMN share_token TEXT;
CREATE UNIQUE INDEX idx_recipes_share_token ON recipes(share_token) WHERE share_token IS NOT NULL;
