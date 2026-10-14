-- Language support. Two independent pieces:
--
-- 1. users.language holds the chosen UI language ('en' or 'no'). Existing
--    users are backfilled to 'en' so nobody's journal changes language the
--    day this ships.
--
-- 2. categories.i18n_key marks a category as one of the seeded defaults, so
--    it can be shown translated. User-created categories have a NULL key and
--    always display their literal name -- the same rule recipes follow: text
--    the user typed is never translated. Renaming a seeded category clears
--    its key (see store.UpdateCategoryName), because from then on the name
--    is the user's own words.
--
-- The English name stays the canonical value in `name` either way, so
-- dropping translation later needs no data migration.

ALTER TABLE users ADD COLUMN language TEXT NOT NULL DEFAULT 'en';

ALTER TABLE categories ADD COLUMN i18n_key TEXT;

-- Backfill keys for existing accounts whose categories still carry the seeded
-- English names. A category the user already renamed won't match, and so
-- correctly stays untranslated.
UPDATE categories SET i18n_key = 'cat.breakfast_brunch'        WHERE i18n_key IS NULL AND name = 'Breakfast & Brunch';
UPDATE categories SET i18n_key = 'cat.appetizers_snacks'       WHERE i18n_key IS NULL AND name = 'Appetizers & Snacks';
UPDATE categories SET i18n_key = 'cat.main_dishes'             WHERE i18n_key IS NULL AND name = 'Main Dishes';
UPDATE categories SET i18n_key = 'cat.side_dishes'             WHERE i18n_key IS NULL AND name = 'Side Dishes';
UPDATE categories SET i18n_key = 'cat.soups_stews'             WHERE i18n_key IS NULL AND name = 'Soups & Stews';
UPDATE categories SET i18n_key = 'cat.desserts_baking'         WHERE i18n_key IS NULL AND name = 'Desserts & Baking';
UPDATE categories SET i18n_key = 'cat.beverages'               WHERE i18n_key IS NULL AND name = 'Beverages';
UPDATE categories SET i18n_key = 'cat.family_favorites'        WHERE i18n_key IS NULL AND name = 'Family Favorites';
UPDATE categories SET i18n_key = 'cat.quick_easy'              WHERE i18n_key IS NULL AND name = 'Quick & Easy';
UPDATE categories SET i18n_key = 'cat.recipes_to_try'          WHERE i18n_key IS NULL AND name = 'Recipes to Try';
UPDATE categories SET i18n_key = 'cat.vegetarian'              WHERE i18n_key IS NULL AND name = 'Vegetarian';
UPDATE categories SET i18n_key = 'cat.holidays_special_events' WHERE i18n_key IS NULL AND name = 'Holidays & Special Events';

CREATE INDEX IF NOT EXISTS idx_categories_i18n_key ON categories(i18n_key) WHERE i18n_key IS NOT NULL;
