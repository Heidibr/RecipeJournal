// Command server boots the Recipe Journal HTTP service.
//
// Wiring order: env → SQLite → migrations → sessions → auth → handlers → chi
// router → http.ListenAndServe. Everything is plain stdlib + a thin set of
// third-party deps (chi, scs, modernc.org/sqlite, bcrypt).
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/heidi/recipejournal/internal/auth"
	"github.com/heidi/recipejournal/internal/email"
	"github.com/heidi/recipejournal/internal/handlers"
	"github.com/heidi/recipejournal/internal/store"
	"github.com/heidi/recipejournal/internal/ui"
	"github.com/justinas/nosurf"
	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	addr := envOr("ADDR", ":8080")
	dbPath := envOr("DB_PATH", "./recipes.db")
	staticDir := envOr("STATIC_DIR", "./static")
	// APP_ENV gates dev-only conveniences (insecure cookies, log-only email).
	// Default to "prod" so a forgotten env var fails closed rather than open.
	appEnv := envOr("APP_ENV", "prod")
	isDev := appEnv == "dev"
	// APP_URL is the public origin used to build absolute links in emails. In
	// dev it defaults to localhost; in prod it MUST be set explicitly.
	appURL := envOr("APP_URL", "http://localhost:8080")

	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // SQLite likes one writer.

	st := store.New(db)
	if err := st.Migrate(); err != nil {
		return err
	}

	// scs's SQLite store wants a separate "sessions" table; create it if not there.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		data BLOB NOT NULL,
		expiry REAL NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expiry)`); err != nil {
		return err
	}

	sm := scs.New()
	sm.Store = sqlite3store.New(db)
	sm.Lifetime = 30 * 24 * time.Hour
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Persist = true
	// Secure flag: ON in prod (cookie only sent over HTTPS), OFF in dev so
	// the browser will accept it on http://localhost.
	sm.Cookie.Secure = !isDev

	mailer := email.FromEnv(appURL)
	authSvc := auth.New(st, sm, mailer)

	// Content hash on the CSS URL. New HTML then asks for a URL no stale
	// cache can answer, which is what stops a service worker from pairing
	// yesterday's stylesheet with today's markup.
	cssVer := assetVersion(filepath.Join(staticDir, "css", "styles.css"))

	renderer, err := ui.New(cssVer)
	if err != nil {
		return err
	}

	h := handlers.New(st, authSvc, renderer)

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(sm.LoadAndSave) // session middleware: loads cookie, saves on response

	// Static files served directly from disk so CSS/JS changes don't need a rebuild.
	fs := http.FileServer(http.Dir(staticDir))
	r.Handle("/static/*", http.StripPrefix("/static/", fs))

	// Service worker MUST be served from root so its default scope is "/",
	// otherwise it can't intercept fetches for /recipes/... pages. Same idea
	// for the PWA manifest — keeping it root-relative simplifies install.
	r.Get("/sw.js", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Service-Worker-Allowed", "/")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, req, staticDir+"/sw.js")
	})
	// Browsers and crawlers request /favicon.ico at the root regardless of
	// the <link> tags, so serve it from there too.
	r.Get("/favicon.ico", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, req, staticDir+"/favicon.ico")
	})
	r.Get("/manifest.webmanifest", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		http.ServeFile(w, req, staticDir+"/manifest.webmanifest")
	})

	r.Get("/", h.Home)
	r.Get("/signup", h.ShowSignUp)
	r.Post("/signup", h.DoSignUp)
	r.Get("/login", h.ShowLogin)
	r.Post("/login", h.DoLogin)
	r.Post("/logout", h.DoLogout)

	// Language switch is public: the login and signup pages need it too.
	// POST (not GET) so it is CSRF-protected and never prefetched.
	r.Post("/lang/{lang}", h.SetLanguage)
	r.Get("/share/{token}", h.ShowSharedRecipe) // public read-only

	// Public verification/reset entry points — no auth required because the
	// user clicks these from an email (no session yet, or different device).
	r.Get("/verify/{token}", h.ShowVerifyResult)
	r.Get("/forgot", h.ShowForgot)
	r.Post("/forgot", h.DoForgot)
	r.Get("/reset/{token}", h.ShowReset)
	r.Post("/reset/{token}", h.DoReset)

	r.Group(func(r chi.Router) {
		r.Use(authSvc.RequireUser)

		// Resend-verification button on the banner. Auth'd so we know whose
		// email to refresh; rate-limited implicitly by the 7-day token TTL.
		r.Post("/verify/resend", h.ResendVerification)

		r.Get("/categories", h.ListCategories)
		r.Post("/categories", h.CreateCategory)
		r.Patch("/categories/{id}", h.UpdateCategory)
		r.Delete("/categories/{id}", h.DeleteCategory)

		r.Get("/recipes", h.ListRecipes)
		r.Get("/recipes/new", h.NewRecipe)
		r.Post("/recipes", h.CreateRecipe)
		r.Get("/recipes/{id}", h.ShowRecipe)
		r.Get("/recipes/{id}/edit", h.EditRecipe)
		// Plain HTML forms only do GET/POST. We use POST + path suffix here
		// instead of fighting browsers with PATCH/DELETE for the form submits.
		r.Post("/recipes/{id}/update", h.UpdateRecipe)
		r.Post("/recipes/{id}/delete", h.DeleteRecipe)

		// Sharing-related actions are gated on email verification — see
		// RequireVerified in internal/auth.
		r.Group(func(r chi.Router) {
			r.Use(authSvc.RequireVerified)
			r.Post("/recipes/{id}/share", h.ShareRecipe)
			r.Post("/recipes/{id}/share/revoke", h.RevokeShare)

			// "Add to my journal" flow for signed-in viewers of a /share/{token} page.
			r.Post("/share/{token}/save", h.SaveSharedRecipe)
			r.Post("/share/{token}/save/confirm", h.ConfirmSaveSharedRecipe)
		})
	})

	// CSRF wraps the whole router. nosurf enforces a double-submit cookie on
	// every state-changing method (POST/PUT/PATCH/DELETE). It accepts the
	// token from a "csrf_token" form field or an X-CSRF-Token header, so
	// both classic form posts and HTMX requests work.
	csrfHandler := nosurf.New(r)
	csrfHandler.SetBaseCookie(http.Cookie{
		HttpOnly: true,
		Path:     "/",
		MaxAge:   nosurf.MaxAge,
		SameSite: http.SameSiteLaxMode,
		Secure:   !isDev,
	})
	csrfHandler.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Printf("csrf failure: %v (path=%s)", nosurf.Reason(req), req.URL.Path)
		http.Error(w, "Your session expired. Please reload the page and try again.", http.StatusForbidden)
	}))

	server := &http.Server{
		Addr:              addr,
		Handler:           canonicalHost(appURL, isDev, csrfHandler),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown — SIGINT/SIGTERM drain in-flight requests for up to 10s.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("listening on %s (db: %s)", addr, dbPath)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// securityHeaders sets defensive headers on every response. Kept conservative
// so we don't break HTMX or the inline Google Fonts <link>: no strict CSP yet,
// just the cheap wins (clickjacking, MIME sniffing, referrer leakage).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
		next.ServeHTTP(w, r)
	})
}

// canonicalHost redirects any request whose Host is not APP_URL's host --
// recipejournal.fly.dev, www. -- to the same path on APP_URL. Cookies and
// the installed PWA are per-origin, so one public address keeps sessions in
// one place. It wraps everything, so a request that is about to be
// redirected never gets a session or CSRF cookie on the wrong host.
//
// Off in dev and when APP_URL is localhost. 308 rather than 301 so a POST
// stays a POST instead of silently becoming a GET.
func canonicalHost(appURL string, isDev bool, next http.Handler) http.Handler {
	u, err := url.Parse(appURL)
	if err != nil || isDev || u.Host == "" || u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == u.Host {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, u.Scheme+"://"+u.Host+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
}

// assetVersion returns a short content hash for a static file, used to
// cache-bust its URL. If the file cannot be read (it is generated by the
// Tailwind build), fall back to the process start time so each boot still
// serves a fresh URL rather than a wrong one.
func assetVersion(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return strconv.FormatInt(time.Now().Unix(), 36)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return strconv.FormatInt(time.Now().Unix(), 36)
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}
