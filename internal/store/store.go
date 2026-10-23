package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/heidi/recipejournal/internal/models"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	DB *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{DB: db}
}

// Migrate runs every .sql file under the embedded migrations dir in lexical
// order, recording each applied name in a schema_migrations table so they only
// run once per database.
func (s *Store) Migrate() error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var dummy int
		err := s.DB.QueryRow(`SELECT 1 FROM schema_migrations WHERE name = ?`, name).Scan(&dummy)
		if err == nil {
			continue // already applied
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check %s applied: %w", name, err)
		}
		bs, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(bs)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}

// --- Users -----------------------------------------------------------------

var ErrNotFound = errors.New("not found")

func (s *Store) CreateUser(email, passwordHash, name string) (int64, error) {
	res, err := s.DB.Exec(
		`INSERT INTO users (email, password_hash, name) VALUES (?, ?, ?)`,
		email, passwordHash, name,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// userSelectCols + scanUser keep the user-column list in one place so adding
// future columns (e.g. timezone, locale) only requires touching one spot.
const userSelectCols = `id, email, password_hash, name, created_at, email_verified_at, language`

func scanUser(row interface{ Scan(...any) error }) (*models.User, error) {
	var u models.User
	var verified sql.NullTime
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt, &verified, &u.Language); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if verified.Valid {
		t := verified.Time
		u.EmailVerifiedAt = &t
	}
	return &u, nil
}

func (s *Store) UserByEmail(email string) (*models.User, error) {
	return scanUser(s.DB.QueryRow(
		`SELECT `+userSelectCols+` FROM users WHERE email = ?`, email,
	))
}

func (s *Store) UserByID(id int64) (*models.User, error) {
	return scanUser(s.DB.QueryRow(
		`SELECT `+userSelectCols+` FROM users WHERE id = ?`, id,
	))
}

// DeleteUser removes a user and, through ON DELETE CASCADE, their recipes,
// categories and outstanding verification/reset tokens. Recipes other people
// saved from this user's share links are their own copies and stay.
func (s *Store) DeleteUser(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// SetUserLanguage persists the UI language for a signed-in user, so the
// choice follows them to any device. Anonymous visitors get the cookie only.
func (s *Store) SetUserLanguage(userID int64, lang string) error {
	_, err := s.DB.Exec(`UPDATE users SET language = ? WHERE id = ?`, lang, userID)
	return err
}

// --- Email verification ----------------------------------------------------

// CreateEmailVerification issues a new verification token and clears any
// outstanding ones for the user. The token is the caller-visible identifier
// (long random) — return it so the caller can email a link. Only its hash is
// stored.
func (s *Store) CreateEmailVerification(userID int64, ttl time.Duration) (string, error) {
	token, err := newToken(32)
	if err != nil {
		return "", err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM email_verifications WHERE user_id = ?`, userID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(
		`INSERT INTO email_verifications (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), userID, time.Now().Add(ttl).UTC(),
	); err != nil {
		return "", err
	}
	return token, tx.Commit()
}

// ConsumeEmailVerification looks up an unexpired token, marks the user's
// email verified, deletes the token, and returns the user id. ErrNotFound
// means the token is unknown, already used, or expired — all surfaced to the
// caller the same way to avoid leaking which.
func (s *Store) ConsumeEmailVerification(token string) (int64, error) {
	h := hashToken(token)
	var userID int64
	var expiresAt time.Time
	err := s.DB.QueryRow(
		`SELECT user_id, expires_at FROM email_verifications WHERE token_hash = ?`,
		h,
	).Scan(&userID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if time.Now().After(expiresAt) {
		_, _ = s.DB.Exec(`DELETE FROM email_verifications WHERE token_hash = ?`, h)
		return 0, ErrNotFound
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`UPDATE users SET email_verified_at = CURRENT_TIMESTAMP WHERE id = ?`, userID,
	); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM email_verifications WHERE token_hash = ?`, h); err != nil {
		return 0, err
	}
	return userID, tx.Commit()
}

// --- Password resets -------------------------------------------------------

// CreatePasswordReset issues a new reset token. Multiple outstanding tokens
// per user are fine — the most recent one a user clicks is what counts —
// but we clear old ones to keep the table small.
func (s *Store) CreatePasswordReset(userID int64, ttl time.Duration) (string, error) {
	token, err := newToken(32)
	if err != nil {
		return "", err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM password_resets WHERE user_id = ? AND used_at IS NULL`, userID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(
		`INSERT INTO password_resets (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), userID, time.Now().Add(ttl).UTC(),
	); err != nil {
		return "", err
	}
	return token, tx.Commit()
}

// LookupPasswordReset returns the user id behind an unused, unexpired token.
// Does NOT mark the token consumed — call ConsumePasswordReset for that, so
// the GET-form-render path can validate without burning the token.
func (s *Store) LookupPasswordReset(token string) (int64, error) {
	var userID int64
	var expiresAt time.Time
	var usedAt sql.NullTime
	err := s.DB.QueryRow(
		`SELECT user_id, expires_at, used_at FROM password_resets WHERE token_hash = ?`,
		hashToken(token),
	).Scan(&userID, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if usedAt.Valid || time.Now().After(expiresAt) {
		return 0, ErrNotFound
	}
	return userID, nil
}

// ConsumePasswordReset atomically validates the token, updates the password
// hash, and marks the token used. Returns ErrNotFound for any failed lookup
// (unknown / used / expired) so the caller can show a single generic message.
func (s *Store) ConsumePasswordReset(token, newHash string) (int64, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	h := hashToken(token)
	var userID int64
	var expiresAt time.Time
	var usedAt sql.NullTime
	err = tx.QueryRow(
		`SELECT user_id, expires_at, used_at FROM password_resets WHERE token_hash = ?`, h,
	).Scan(&userID, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if usedAt.Valid || time.Now().After(expiresAt) {
		return 0, ErrNotFound
	}
	if _, err := tx.Exec(
		`UPDATE users SET password_hash = ? WHERE id = ?`, newHash, userID,
	); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(
		`UPDATE password_resets SET used_at = CURRENT_TIMESTAMP WHERE token_hash = ?`, h,
	); err != nil {
		return 0, err
	}
	return userID, tx.Commit()
}

// newToken returns a URL-safe random token of n random bytes. Used for share
// links, verification, and password reset.
func newToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken is what verification and reset tokens are stored and looked up
// as. A plain SHA-256 is enough here: unlike a password, the token is 32
// random bytes, so there is nothing to brute-force and no need for a slow,
// salted hash like bcrypt. Share tokens are not hashed -- the link itself is
// what the owner publishes.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// --- Categories ------------------------------------------------------------

// DefaultCategory is a seeded category: a canonical English Name stored in
// the row, plus the i18n catalogue Key that lets it render translated. Key
// must exist in the i18n catalogue for every supported language -- there is a
// test asserting that.
type DefaultCategory struct {
	Key  string
	Name string
}

var DefaultCategories = []DefaultCategory{
	{"cat.breakfast_brunch", "Breakfast & Brunch"},
	{"cat.appetizers_snacks", "Appetizers & Snacks"},
	{"cat.main_dishes", "Main Dishes"},
	{"cat.side_dishes", "Side Dishes"},
	{"cat.soups_stews", "Soups & Stews"},
	{"cat.desserts_baking", "Desserts & Baking"},
	{"cat.beverages", "Beverages"},
	{"cat.family_favorites", "Family Favorites"},
	{"cat.quick_easy", "Quick & Easy"},
	{"cat.recipes_to_try", "Recipes to Try"},
	{"cat.vegetarian", "Vegetarian"},
	{"cat.holidays_special_events", "Holidays & Special Events"},
}

func (s *Store) SeedDefaultCategories(userID int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO categories (user_id, name, sort_order, i18n_key) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, dc := range DefaultCategories {
		if _, err := stmt.Exec(userID, dc.Name, i, dc.Key); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// categorySelectCols + scanCategory keep the category column list in one
// place, the same pattern userSelectCols / recipeSelectCols follow.
const categorySelectCols = `id, user_id, name, sort_order, created_at, COALESCE(i18n_key, '')`

func scanCategory(scanner interface{ Scan(...any) error }, c *models.Category) error {
	return scanner.Scan(&c.ID, &c.UserID, &c.Name, &c.SortOrder, &c.CreatedAt, &c.I18nKey)
}

func (s *Store) ListCategories(userID int64) ([]models.Category, error) {
	rows, err := s.DB.Query(
		`SELECT `+categorySelectCols+`
		 FROM categories WHERE user_id = ?
		 ORDER BY sort_order, name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Category
	for rows.Next() {
		var c models.Category
		if err := scanCategory(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CategoryByNameCI looks up a category for a user by case-insensitive name
// match. Used when copying a shared recipe into the viewer's journal: if the
// source recipe's category name maps to one of the viewer's own categories
// (regardless of case), we auto-assign the copy to it.
func (s *Store) CategoryByNameCI(userID int64, name string) (*models.Category, error) {
	row := s.DB.QueryRow(
		`SELECT `+categorySelectCols+`
		   FROM categories
		  WHERE user_id = ? AND LOWER(name) = LOWER(?)`,
		userID, name,
	)
	var c models.Category
	if err := scanCategory(row, &c); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (s *Store) CategoryByID(userID, id int64) (*models.Category, error) {
	row := s.DB.QueryRow(
		`SELECT `+categorySelectCols+` FROM categories WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	var c models.Category
	if err := scanCategory(row, &c); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (s *Store) CreateCategory(userID int64, name string) (*models.Category, error) {
	var nextOrder int
	_ = s.DB.QueryRow(
		`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM categories WHERE user_id = ?`, userID,
	).Scan(&nextOrder)
	res, err := s.DB.Exec(
		`INSERT INTO categories (user_id, name, sort_order) VALUES (?, ?, ?)`,
		userID, name, nextOrder,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.CategoryByID(userID, id)
}

// UpdateCategoryName renames a category and clears its i18n_key. Once the
// user has typed their own name for a category, that name is their text and
// is shown verbatim in every language -- the same rule recipes follow. This
// is why renaming a seeded category makes it stop translating.
func (s *Store) UpdateCategoryName(userID, id int64, name string) (*models.Category, error) {
	res, err := s.DB.Exec(
		`UPDATE categories SET name = ?, i18n_key = NULL WHERE id = ? AND user_id = ?`,
		name, id, userID,
	)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.CategoryByID(userID, id)
}

func (s *Store) DeleteCategory(userID, id int64) error {
	res, err := s.DB.Exec(
		`DELETE FROM categories WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Recipes ---------------------------------------------------------------

type RecipeFilter struct {
	CategoryID *int64
}

// recipeSelectCols is the column list shared by ListRecipes / RecipeByID /
// RecipeByShareToken so a new column only has to be added in one place (and
// the matching scan target list).
const recipeSelectCols = `r.id, r.user_id, r.category_id, COALESCE(c.name, ''), COALESCE(c.i18n_key, ''),
	r.name, r.difficulty, r.serves, r.prep_minutes, r.cook_minutes,
	r.ingredients, r.process, r.wine_pairings, r.notes,
	COALESCE(r.share_token, ''),
	r.created_at, r.updated_at`

func scanRecipe(scanner interface{ Scan(...any) error }, r *models.Recipe) error {
	var catID sql.NullInt64
	if err := scanner.Scan(
		&r.ID, &r.UserID, &catID, &r.CategoryName, &r.CategoryI18nKey,
		&r.Name, &r.Difficulty, &r.Serves, &r.PrepMinutes, &r.CookMinutes,
		&r.Ingredients, &r.Process, &r.WinePairings, &r.Notes,
		&r.ShareToken,
		&r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return err
	}
	if catID.Valid {
		id := catID.Int64
		r.CategoryID = &id
	}
	return nil
}

func (s *Store) ListRecipes(userID int64, f RecipeFilter) ([]models.Recipe, error) {
	q := `SELECT ` + recipeSelectCols + `
		 FROM recipes r
		 LEFT JOIN categories c ON c.id = r.category_id
		 WHERE r.user_id = ?`
	args := []any{userID}
	if f.CategoryID != nil {
		q += ` AND r.category_id = ?`
		args = append(args, *f.CategoryID)
	}
	q += ` ORDER BY r.updated_at DESC`
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Recipe
	for rows.Next() {
		var r models.Recipe
		if err := scanRecipe(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RecipeByID(userID, id int64) (*models.Recipe, error) {
	row := s.DB.QueryRow(
		`SELECT `+recipeSelectCols+`
		   FROM recipes r LEFT JOIN categories c ON c.id = r.category_id
		   WHERE r.id = ? AND r.user_id = ?`,
		id, userID,
	)
	var r models.Recipe
	if err := scanRecipe(row, &r); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

// RecipeByShareToken returns the recipe behind a public share link, including
// the owner's display name so the share page can credit them.
func (s *Store) RecipeByShareToken(token string) (*models.Recipe, error) {
	row := s.DB.QueryRow(
		`SELECT `+recipeSelectCols+`, u.name
		   FROM recipes r
		   LEFT JOIN categories c ON c.id = r.category_id
		   JOIN users u ON u.id = r.user_id
		   WHERE r.share_token = ?`,
		token,
	)
	var r models.Recipe
	var catID sql.NullInt64
	if err := row.Scan(
		&r.ID, &r.UserID, &catID, &r.CategoryName, &r.CategoryI18nKey,
		&r.Name, &r.Difficulty, &r.Serves, &r.PrepMinutes, &r.CookMinutes,
		&r.Ingredients, &r.Process, &r.WinePairings, &r.Notes,
		&r.ShareToken,
		&r.CreatedAt, &r.UpdatedAt,
		&r.OwnerName,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if catID.Valid {
		id := catID.Int64
		r.CategoryID = &id
	}
	return &r, nil
}

// ShareRecipe generates a URL-safe share token for the recipe (if it doesn't
// already have one) and returns the updated recipe.
func (s *Store) ShareRecipe(userID, id int64) (*models.Recipe, error) {
	rec, err := s.RecipeByID(userID, id)
	if err != nil {
		return nil, err
	}
	if rec.ShareToken != "" {
		return rec, nil
	}
	token, err := newShareToken()
	if err != nil {
		return nil, err
	}
	if _, err := s.DB.Exec(
		`UPDATE recipes SET share_token = ? WHERE id = ? AND user_id = ?`,
		token, id, userID,
	); err != nil {
		return nil, err
	}
	return s.RecipeByID(userID, id)
}

// RevokeShare clears the share token, making the public link 404.
func (s *Store) RevokeShare(userID, id int64) (*models.Recipe, error) {
	if _, err := s.DB.Exec(
		`UPDATE recipes SET share_token = NULL WHERE id = ? AND user_id = ?`,
		id, userID,
	); err != nil {
		return nil, err
	}
	return s.RecipeByID(userID, id)
}

func newShareToken() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

type RecipeInput struct {
	CategoryID   *int64
	Name         string
	Difficulty   int
	Serves       int
	PrepMinutes  int
	CookMinutes  int
	Ingredients  string
	Process      string
	WinePairings string
	Notes        string
}

// CopyRecipeFromShare looks up a recipe by its share token and inserts a copy
// owned by viewerUserID. The copy never carries the source's share_token (so
// each viewer's copy is independent and not auto-shared). categoryID is the
// caller's choice — nil means uncategorized.
func (s *Store) CopyRecipeFromShare(viewerUserID int64, token string, categoryID *int64) (int64, error) {
	src, err := s.RecipeByShareToken(token)
	if err != nil {
		return 0, err
	}
	if src.UserID == viewerUserID {
		// Owner shouldn't reach this path — the UI hides the button — but
		// reject defensively so a direct POST can't silently duplicate.
		return 0, errors.New("cannot copy your own recipe")
	}
	res, err := s.DB.Exec(
		`INSERT INTO recipes
		 (user_id, category_id, name, difficulty, serves, prep_minutes, cook_minutes,
		  ingredients, process, wine_pairings, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		viewerUserID, categoryID, src.Name, src.Difficulty, src.Serves,
		src.PrepMinutes, src.CookMinutes,
		src.Ingredients, src.Process, src.WinePairings, src.Notes,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) CreateRecipe(userID int64, in RecipeInput) (int64, error) {
	res, err := s.DB.Exec(
		`INSERT INTO recipes
		 (user_id, category_id, name, difficulty, serves, prep_minutes, cook_minutes,
		  ingredients, process, wine_pairings, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, in.CategoryID, in.Name, in.Difficulty, in.Serves,
		in.PrepMinutes, in.CookMinutes,
		in.Ingredients, in.Process, in.WinePairings, in.Notes,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateRecipe(userID, id int64, in RecipeInput) error {
	res, err := s.DB.Exec(
		`UPDATE recipes
		   SET category_id = ?, name = ?, difficulty = ?, serves = ?,
		       prep_minutes = ?, cook_minutes = ?,
		       ingredients = ?, process = ?, wine_pairings = ?, notes = ?,
		       updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND user_id = ?`,
		in.CategoryID, in.Name, in.Difficulty, in.Serves,
		in.PrepMinutes, in.CookMinutes,
		in.Ingredients, in.Process, in.WinePairings, in.Notes,
		id, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteRecipe(userID, id int64) error {
	res, err := s.DB.Exec(
		`DELETE FROM recipes WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
