package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestStore opens a migrated database in a temp dir, with the same
// pragmas the server uses (foreign keys matter for the cascade test).
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db")+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	s := New(db)
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func newTestUser(t *testing.T, s *Store) int64 {
	t.Helper()
	id, err := s.CreateUser("a@example.com", "hash", "A")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAuthTokensAreStoredHashed(t *testing.T) {
	s := newTestStore(t)
	uid := newTestUser(t, s)

	verify, err := s.CreateEmailVerification(uid, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reset, err := s.CreatePasswordReset(uid, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// The database must not contain the token a link is built from.
	for _, q := range []struct{ table, token string }{
		{"email_verifications", verify},
		{"password_resets", reset},
	} {
		var stored string
		if err := s.DB.QueryRow(`SELECT token_hash FROM ` + q.table).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored == q.token || stored != hashToken(q.token) {
			t.Errorf("%s stores %q, want sha256 of the emailed token", q.table, stored)
		}
	}

	// And the plaintext token from the email still works.
	if got, err := s.LookupPasswordReset(reset); err != nil || got != uid {
		t.Errorf("LookupPasswordReset = %d, %v; want %d", got, err, uid)
	}
	if _, err := s.ConsumePasswordReset(reset, "newhash"); err != nil {
		t.Errorf("ConsumePasswordReset: %v", err)
	}
	if got, err := s.ConsumeEmailVerification(verify); err != nil || got != uid {
		t.Errorf("ConsumeEmailVerification = %d, %v; want %d", got, err, uid)
	}

	// Looking up by the stored hash itself must not work.
	if _, err := s.ConsumeEmailVerification(hashToken(verify)); !errors.Is(err, ErrNotFound) {
		t.Errorf("lookup by hash: err = %v, want ErrNotFound", err)
	}
}

func TestDeleteUserCascades(t *testing.T) {
	s := newTestStore(t)
	uid := newTestUser(t, s)
	if err := s.SeedDefaultCategories(uid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRecipe(uid, RecipeInput{Name: "Pancakes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePasswordReset(uid, time.Hour); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteUser(uid); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"users", "recipes", "categories", "password_resets"} {
		var n int
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s has %d rows after DeleteUser, want 0", table, n)
		}
	}
}
