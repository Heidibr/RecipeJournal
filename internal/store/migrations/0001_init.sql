CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  name          TEXT NOT NULL,
  created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS categories (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (user_id, name)
);

CREATE TABLE IF NOT EXISTS recipes (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  category_id   INTEGER REFERENCES categories(id) ON DELETE SET NULL,
  name          TEXT NOT NULL,
  difficulty    INTEGER CHECK (difficulty BETWEEN 0 AND 5) DEFAULT 0,
  serves        INTEGER CHECK (serves BETWEEN 0 AND 4) DEFAULT 0,
  prep_minutes  INTEGER DEFAULT 0,
  cook_minutes  INTEGER DEFAULT 0,
  ingredients   TEXT DEFAULT '',
  process       TEXT DEFAULT '',
  wine_pairings TEXT DEFAULT '',
  notes         TEXT DEFAULT '',
  created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_recipes_user_category ON recipes (user_id, category_id);
CREATE INDEX IF NOT EXISTS idx_categories_user ON categories (user_id, sort_order);
