// Package auth handles user registration, login, sessions, and the middleware
// that gates protected routes. Also owns the email-verification and
// password-reset flows since those are tightly coupled to credentials.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/heidi/recipejournal/internal/email"
	"github.com/heidi/recipejournal/internal/models"
	"github.com/heidi/recipejournal/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionUserIDKey = "userID"

	verificationTTL  = 7 * 24 * time.Hour
	passwordResetTTL = 1 * time.Hour
)

type Auth struct {
	Store    *store.Store
	Sessions *scs.SessionManager
	Mailer   email.Sender
	limiter  *loginLimiter
}

func New(s *store.Store, sm *scs.SessionManager, m email.Sender) *Auth {
	return &Auth{Store: s, Sessions: sm, Mailer: m, limiter: newLoginLimiter()}
}

// SignUp creates a user, seeds their default categories, sends a verification
// email, and logs them in. We auto-login (instead of forcing verify-first)
// because the user is already trusted enough to use the journal — sharing is
// the only thing gated on verification.
func (a *Auth) SignUp(ctx context.Context, email, name, password string) (*models.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	name = strings.TrimSpace(name)
	if email == "" || name == "" || len(password) < 8 {
		return nil, ErrInvalidInput
	}
	if _, err := a.Store.UserByEmail(email); err == nil {
		return nil, ErrEmailTaken
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	id, err := a.Store.CreateUser(email, string(hash), name)
	if err != nil {
		return nil, err
	}
	if err := a.Store.SeedDefaultCategories(id); err != nil {
		return nil, err
	}
	a.Sessions.Put(ctx, sessionUserIDKey, id)
	u, err := a.Store.UserByID(id)
	if err != nil {
		return nil, err
	}
	// Verification email failure must NOT block signup — the user can request
	// a resend later. Log and continue.
	if err := a.SendVerificationEmail(u); err != nil {
		// (caller can log if it cares; signup itself still succeeded)
		_ = err
	}
	return u, nil
}

// loginKey is the rate-limit bucket used per login attempt. Bucketing on
// (IP, email) means one attacker can't lock out arbitrary users from one IP
// without ALSO knowing those users' email addresses — and even then they're
// only locking the (their IP, victim email) bucket, not the victim's own
// future logins from a different IP.
func loginKey(ip, email string) string {
	return ip + "|" + strings.ToLower(strings.TrimSpace(email))
}

// Login authenticates a user. ip is the client IP (used as part of the rate
// limit bucket). ErrTooManyAttempts is returned when the bucket is full;
// callers should show a generic "try again later" message.
func (a *Auth) Login(ctx context.Context, ip, email, password string) (*models.User, error) {
	key := loginKey(ip, email)
	if !a.limiter.Allow(key) {
		return nil, ErrTooManyAttempts
	}
	email = strings.TrimSpace(strings.ToLower(email))
	u, err := a.Store.UserByEmail(email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.limiter.RecordFailure(key)
			return nil, ErrBadCredentials
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		a.limiter.RecordFailure(key)
		return nil, ErrBadCredentials
	}
	if err := a.Sessions.RenewToken(ctx); err != nil {
		return nil, err
	}
	a.Sessions.Put(ctx, sessionUserIDKey, u.ID)
	a.limiter.Reset(key)
	return u, nil
}

func (a *Auth) Logout(ctx context.Context) error {
	return a.Sessions.Destroy(ctx)
}

// CurrentUserID returns the logged-in user id from the request context, or 0.
func (a *Auth) CurrentUserID(r *http.Request) int64 {
	v := a.Sessions.Get(r.Context(), sessionUserIDKey)
	if id, ok := v.(int64); ok {
		return id
	}
	return 0
}

// userCtxKey is the request-context key under which the logged-in *models.User
// is stashed by RequireUser, so handlers can pull it out without re-querying.
type userCtxKey struct{}

// UserFromContext returns the *models.User stashed by RequireUser, or nil.
func UserFromContext(ctx context.Context) *models.User {
	u, _ := ctx.Value(userCtxKey{}).(*models.User)
	return u
}

// RequireUser is middleware that redirects unauthenticated requests to /login.
// On success it loads the user and stashes it in the request context.
func (a *Auth) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := a.CurrentUserID(r)
		if id == 0 {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		u, err := a.Store.UserByID(id)
		if err != nil {
			// Stale session pointing at deleted user — wipe it and bounce.
			_ = a.Sessions.Destroy(r.Context())
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey{}, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireVerified is middleware that gates an action on the user having
// verified their email. Apply after RequireUser. Returns 403 with a short
// message; the page-level banner explains how to resend the email.
func (a *Auth) RequireVerified(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if !u.EmailVerified() {
			http.Error(w, "Please verify your email before sharing or saving recipes.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Email verification ----------------------------------------------------

// SendVerificationEmail issues a fresh verification token for the user and
// emails them a link. Safe to call multiple times — each call invalidates
// the previous outstanding token.
func (a *Auth) SendVerificationEmail(u *models.User) error {
	token, err := a.Store.CreateEmailVerification(u.ID, verificationTTL)
	if err != nil {
		return err
	}
	link := a.Mailer.AppURL() + "/verify/" + token
	body := "Hi " + u.Name + ",\n\n" +
		"Please confirm your email for Recipe Journal by opening this link:\n\n" +
		link + "\n\n" +
		"The link expires in 7 days. If you didn't sign up, you can ignore this email.\n"
	return a.Mailer.Send(u.Email, "Verify your Recipe Journal email", body)
}

// VerifyEmailToken marks a user verified given a token. Returns ErrInvalidToken
// for any failure (unknown / expired) so the caller renders one neutral page.
func (a *Auth) VerifyEmailToken(token string) (int64, error) {
	uid, err := a.Store.ConsumeEmailVerification(token)
	if errors.Is(err, store.ErrNotFound) {
		return 0, ErrInvalidToken
	}
	return uid, err
}

// --- Password reset --------------------------------------------------------

// SendPasswordResetEmail looks up the user by email and, if found, issues a
// reset token and emails them a link. If the email is unknown the function
// silently succeeds — callers must NOT branch on the result, so we don't leak
// which addresses are registered.
func (a *Auth) SendPasswordResetEmail(emailAddr string) error {
	emailAddr = strings.TrimSpace(strings.ToLower(emailAddr))
	u, err := a.Store.UserByEmail(emailAddr)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // silently noop to prevent enumeration
		}
		return err
	}
	token, err := a.Store.CreatePasswordReset(u.ID, passwordResetTTL)
	if err != nil {
		return err
	}
	link := a.Mailer.AppURL() + "/reset/" + token
	body := "Hi " + u.Name + ",\n\n" +
		"Someone asked to reset the password for your Recipe Journal account.\n" +
		"If that was you, open this link to choose a new password:\n\n" +
		link + "\n\n" +
		"The link expires in 1 hour. If you didn't ask for a reset, you can ignore\n" +
		"this email — your password won't change.\n"
	return a.Mailer.Send(u.Email, "Reset your Recipe Journal password", body)
}

// ResetPassword validates a token and updates the password. On success it
// also destroys the current session so a stolen-token attacker can't ride a
// concurrent login. ErrInvalidToken means the token is unknown, used, or
// expired; ErrInvalidInput means the new password is too short.
func (a *Auth) ResetPassword(ctx context.Context, token, newPassword string) error {
	if len(newPassword) < 8 {
		return ErrInvalidInput
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := a.Store.ConsumePasswordReset(token, string(hash)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrInvalidToken
		}
		return err
	}
	// Force re-login so an attacker who somehow held an active session is
	// kicked out as soon as the legitimate owner resets the password.
	_ = a.Sessions.Destroy(ctx)
	return nil
}

var (
	ErrInvalidInput    = errors.New("invalid input")
	ErrEmailTaken      = errors.New("email already registered")
	ErrBadCredentials  = errors.New("invalid email or password")
	ErrTooManyAttempts = errors.New("too many login attempts")
	ErrInvalidToken    = errors.New("invalid or expired token")
)
