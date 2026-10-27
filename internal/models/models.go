package models

import "time"

type User struct {
	ID              int64
	Email           string
	PasswordHash    string
	Name            string
	CreatedAt       time.Time
	EmailVerifiedAt *time.Time // nil means the email is not yet verified
	Language        string     // UI language code: "en" or "no"
}

// EmailVerified is true once the user clicked the link in their verification
// email. Gates participation in sharing (creating share links, saving shared
// recipes) without blocking basic journal use.
func (u *User) EmailVerified() bool { return u != nil && u.EmailVerifiedAt != nil }

type Category struct {
	ID        int64
	UserID    int64
	Name      string
	SortOrder int
	CreatedAt time.Time

	// I18nKey is set only for the categories seeded at signup, and is the
	// catalogue key used to show a translated name. It is empty for any
	// category the user created or renamed, whose name is then shown
	// verbatim -- user-written text is never translated.
	I18nKey string
}

// Serves runs 1..ServesPlus, with 0 meaning "not set". ServesPlus is shown
// as "8+", for dishes that feed a crowd (a traybake, a party). The CHECK in
// migration 0006 allows exactly this range.
const ServesPlus = 9

// MaxDifficulty is the top of the 0..5 difficulty scale.
const MaxDifficulty = 5

type Recipe struct {
	ID              int64
	UserID          int64
	CategoryID      *int64
	CategoryName    string
	CategoryI18nKey string // see Category.I18nKey; empty for user-named categories
	Name            string
	Difficulty      int
	Serves          int
	PrepMinutes     int
	CookMinutes     int
	Ingredients     string
	Process         string
	WinePairings    string
	Notes           string
	ShareToken      string // empty means not shared
	ShareURL        string // populated by handlers from the request host; never persisted
	OwnerName       string // populated only when fetched via share token
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
