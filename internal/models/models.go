package models

import "time"

type User struct {
	ID              int64
	Email           string
	PasswordHash    string
	Name            string
	CreatedAt       time.Time
	EmailVerifiedAt *time.Time // nil means the email is not yet verified
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
}

type Recipe struct {
	ID           int64
	UserID       int64
	CategoryID   *int64
	CategoryName string
	Name         string
	Difficulty   int
	Serves       int
	PrepMinutes  int
	CookMinutes  int
	Ingredients  string
	Process      string
	WinePairings string
	Notes        string
	ShareToken   string // empty means not shared
	ShareURL     string // populated by handlers from the request host; never persisted
	OwnerName    string // populated only when fetched via share token
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
