-- Serves goes from 1-4 to 1-8, plus 9 meaning "8+" (a traybake, a party).
-- SQLite cannot change a CHECK constraint in place, so the table is rebuilt:
-- create the new shape, copy every row, swap the names. Nothing references
-- recipes, so dropping the old table cascades nowhere.

CREATE TABLE recipes_new (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  category_id   INTEGER REFERENCES categories(id) ON DELETE SET NULL,
  name          TEXT NOT NULL,
  difficulty    INTEGER CHECK (difficulty BETWEEN 0 AND 5) DEFAULT 0,
  serves        INTEGER CHECK (serves BETWEEN 0 AND 9) DEFAULT 0,
  prep_minutes  INTEGER DEFAULT 0,
  cook_minutes  INTEGER DEFAULT 0,
  ingredients   TEXT DEFAULT '',
  process       TEXT DEFAULT '',
  wine_pairings TEXT DEFAULT '',
  notes         TEXT DEFAULT '',
  created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
  share_token   TEXT
);

INSERT INTO recipes_new (id, user_id, category_id, name, difficulty, serves,
  prep_minutes, cook_minutes, ingredients, process, wine_pairings, notes,
  created_at, updated_at, share_token)
SELECT id, user_id, category_id, name, difficulty, serves,
  prep_minutes, cook_minutes, ingredients, process, wine_pairings, notes,
  created_at, updated_at, share_token
FROM recipes;

-- Carry the AUTOINCREMENT high-water mark over, so a new recipe never reuses
-- the id of a deleted one (an old link would otherwise open someone's new
-- recipe). The INSERT above only set it to the highest id still present.
UPDATE sqlite_sequence
SET seq = (SELECT seq FROM sqlite_sequence WHERE name = 'recipes')
WHERE name = 'recipes_new'
  AND EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'recipes');

DROP TABLE recipes;
ALTER TABLE recipes_new RENAME TO recipes;

CREATE INDEX idx_recipes_user_category ON recipes (user_id, category_id);
CREATE UNIQUE INDEX idx_recipes_share_token ON recipes(share_token) WHERE share_token IS NOT NULL;
