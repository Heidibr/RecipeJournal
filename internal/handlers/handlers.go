// Package handlers wires HTTP routes to business logic.
package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/heidi/recipejournal/internal/auth"
	"github.com/heidi/recipejournal/internal/i18n"
	"github.com/heidi/recipejournal/internal/models"
	"github.com/heidi/recipejournal/internal/store"
	"github.com/heidi/recipejournal/internal/ui"
	"github.com/justinas/nosurf"
)

type Handlers struct {
	Store *store.Store
	Auth  *auth.Auth
	UI    *ui.Renderer
}

func New(s *store.Store, a *auth.Auth, r *ui.Renderer) *Handlers {
	return &Handlers{Store: s, Auth: a, UI: r}
}

// pageData is the envelope every page template expects.
type pageData struct {
	Lang           i18n.Lang
	CurrentUser    *models.User
	CSRFToken      string
	Flash          string
	Error          string
	Email          string
	Name           string
	Categories     []models.Category
	ActiveCategory *models.Category
	Recipes        []models.Recipe
	Recipe         *models.Recipe
}

// baseData seeds a pageData with the per-request bits every page needs: the
// signed-in user (if any) and the CSRF token (always present — nosurf issues
// one even for anonymous sessions so the signup/login forms can submit).
func (h *Handlers) baseData(r *http.Request) pageData {
	return pageData{
		Lang:        h.lang(r),
		CurrentUser: auth.UserFromContext(r.Context()),
		CSRFToken:   nosurf.Token(r),
	}
}

// langCookie carries the UI language for visitors who are not signed in (the
// login, signup and public share pages). For a signed-in user the column on
// their row is authoritative, so the choice follows them between devices.
const langCookie = "lang"

// lang resolves the language for a request: the signed-in user's stored
// preference first, then the cookie, then English.
func (h *Handlers) lang(r *http.Request) i18n.Lang {
	if u := auth.UserFromContext(r.Context()); u != nil {
		if l, ok := i18n.Parse(u.Language); ok {
			return l
		}
	}
	if c, err := r.Cookie(langCookie); err == nil {
		if l, ok := i18n.Parse(c.Value); ok {
			return l
		}
	}
	return i18n.Default
}

// render is the single place language reaches the renderer, so individual
// handlers never have to thread it through.
func (h *Handlers) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	h.UI.Render(w, status, h.lang(r), name, data)
}

// SetLanguage switches the UI language, persisting it to the user row when
// signed in and always to the cookie. It returns the visitor to the page they
// came from so switching language never loses their place.
func (h *Handlers) SetLanguage(w http.ResponseWriter, r *http.Request) {
	lang, ok := i18n.Parse(chi.URLParam(r, "lang"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	setLangCookie(w, r, lang)
	// This route is public (anonymous visitors on the login page can switch
	// too), so RequireUser has not populated the context -- read the session
	// directly to find a signed-in user whose row we should update.
	if uid := h.Auth.CurrentUserID(r); uid != 0 {
		if err := h.Store.SetUserLanguage(uid, lang.String()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, safeReturnPath(r, "/recipes"), http.StatusSeeOther)
}

// setLangCookie writes the language cookie. It is the carrier for anonymous
// visitors, and is also refreshed at login so a user's stored preference
// reaches the public pages (login, signup, shared recipes) on a new device.
func setLangCookie(w http.ResponseWriter, r *http.Request, lang i18n.Lang) {
	http.SetCookie(w, &http.Cookie{
		Name:     langCookie,
		Value:    lang.String(),
		Path:     "/",
		MaxAge:   int((365 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

// safeReturnPath extracts a same-site path from the Referer header. Only the
// path and query are used and the result must start with a single "/", so a
// crafted Referer cannot turn this into an open redirect.
func safeReturnPath(r *http.Request, fallback string) string {
	ref := r.Header.Get("Referer")
	if ref == "" {
		return fallback
	}
	u, err := url.Parse(ref)
	if err != nil {
		return fallback
	}
	p := u.EscapedPath()
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return fallback
	}
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	return p
}

// --- Home ------------------------------------------------------------------

func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	if h.Auth.CurrentUserID(r) != 0 {
		http.Redirect(w, r, "/recipes", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Auth handlers ---------------------------------------------------------

func (h *Handlers) ShowSignUp(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "signup", h.baseData(r))
}

func (h *Handlers) DoSignUp(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := r.FormValue("email")
	name := r.FormValue("name")
	password := r.FormValue("password")
	_, err := h.Auth.SignUp(r.Context(), email, name, password)
	if err != nil {
		L := h.lang(r)
		msg := L.T("err.generic")
		switch {
		case errors.Is(err, auth.ErrInvalidInput):
			msg = L.T("err.fill_fields")
		case errors.Is(err, auth.ErrEmailTaken):
			msg = L.T("err.email_taken")
		}
		data := h.baseData(r)
		data.Error = msg
		data.Email = email
		data.Name = name
		h.render(w, r, http.StatusBadRequest, "signup", data)
		return
	}
	http.Redirect(w, r, "/recipes", http.StatusSeeOther)
}

func (h *Handlers) ShowLogin(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "login", h.baseData(r))
}

func (h *Handlers) DoLogin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := r.FormValue("email")
	password := r.FormValue("password")
	ip := clientIP(r)
	u, err := h.Auth.Login(r.Context(), ip, email, password)
	if err != nil {
		data := h.baseData(r)
		data.Email = email
		switch {
		case errors.Is(err, auth.ErrTooManyAttempts):
			data.Error = h.lang(r).T("err.login_throttled")
		default:
			data.Error = h.lang(r).T("err.bad_login")
		}
		h.render(w, r, http.StatusUnauthorized, "login", data)
		return
	}
	// Carry the account's stored language into the cookie so the public pages
	// speak it too, on this device, from now on.
	if l, ok := i18n.Parse(u.Language); ok {
		setLangCookie(w, r, l)
	}
	http.Redirect(w, r, "/recipes", http.StatusSeeOther)
}

// clientIP picks the request's client IP. middleware.RealIP in main.go has
// already normalised X-Forwarded-For / X-Real-IP into r.RemoteAddr, so this
// is just splitting "ip:port" — kept tiny on purpose so the rate-limit key
// stays predictable in tests.
func clientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

// --- Email verification ----------------------------------------------------

func (h *Handlers) ShowVerifyResult(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	uid, err := h.Auth.VerifyEmailToken(token)
	data := h.baseData(r)
	if err != nil {
		data.Error = h.lang(r).T("err.verify_link")
		h.render(w, r, http.StatusBadRequest, "verify_result", data)
		return
	}
	// If the viewer happens to already be logged in as a different user, just
	// land them on /recipes — the verification still applied to the correct
	// account (uid), we just don't bounce them through their own session.
	if h.Auth.CurrentUserID(r) == uid {
		data.Flash = h.lang(r).T("flash.email_verified")
	}
	h.render(w, r, http.StatusOK, "verify_result", data)
}

// ResendVerification re-issues a verification token for the current user.
// Idempotent from the user's perspective; rate-limited implicitly by the
// session (you must be logged in) and by the link's 7-day TTL.
func (h *Handlers) ResendVerification(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u.EmailVerified() {
		http.Redirect(w, r, "/recipes", http.StatusSeeOther)
		return
	}
	if err := h.Auth.SendVerificationEmail(u); err != nil {
		http.Error(w, h.lang(r).T("err.send_verify"), http.StatusInternalServerError)
		return
	}
	data := h.baseData(r)
	data.Flash = h.lang(r).T("flash.verify_sent")
	h.render(w, r, http.StatusOK, "verify_sent", data)
}

// --- Password reset --------------------------------------------------------

func (h *Handlers) ShowForgot(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "forgot", h.baseData(r))
}

func (h *Handlers) DoForgot(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	emailAddr := r.FormValue("email")
	// Errors here are intentionally swallowed for unknown-email cases inside
	// SendPasswordResetEmail. A genuine infra error still bubbles up.
	if err := h.Auth.SendPasswordResetEmail(emailAddr); err != nil {
		http.Error(w, h.lang(r).T("err.send_reset"), http.StatusInternalServerError)
		return
	}
	// Always render the same confirmation regardless of whether the address
	// exists — that's the whole point of the no-enumeration design.
	h.render(w, r, http.StatusOK, "forgot_sent", h.baseData(r))
}

func (h *Handlers) ShowReset(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if _, err := h.Store.LookupPasswordReset(token); err != nil {
		data := h.baseData(r)
		data.Error = h.lang(r).T("err.reset_link")
		h.render(w, r, http.StatusBadRequest, "reset_invalid", data)
		return
	}
	data := h.baseData(r)
	data.Recipe = &models.Recipe{} // unused, just keeps the page-data envelope happy
	// Stash the token in the rendered form via Email field — repurposed for
	// the hidden input. (Avoiding adding a new field to pageData for this
	// one-off; keeps the envelope from sprouting reset-only state.)
	data.Email = token
	h.render(w, r, http.StatusOK, "reset", data)
}

func (h *Handlers) DoReset(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	_ = r.ParseForm()
	pw := r.FormValue("password")
	if err := h.Auth.ResetPassword(r.Context(), token, pw); err != nil {
		data := h.baseData(r)
		data.Email = token
		switch {
		case errors.Is(err, auth.ErrInvalidInput):
			data.Error = h.lang(r).T("err.password_short")
			h.render(w, r, http.StatusBadRequest, "reset", data)
		case errors.Is(err, auth.ErrInvalidToken):
			data.Error = h.lang(r).T("err.reset_link")
			h.render(w, r, http.StatusBadRequest, "reset_invalid", data)
		default:
			http.Error(w, h.lang(r).T("err.generic"), http.StatusInternalServerError)
		}
		return
	}
	h.render(w, r, http.StatusOK, "reset_done", h.baseData(r))
}

func (h *Handlers) DoLogout(w http.ResponseWriter, r *http.Request) {
	_ = h.Auth.Logout(r.Context())
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Categories ------------------------------------------------------------

func (h *Handlers) ListCategories(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	cats, err := h.Store.ListCategories(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := h.baseData(r)
	data.Categories = cats
	h.render(w, r, http.StatusOK, "categories", data)
}

func (h *Handlers) CreateCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	_ = r.ParseForm()
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	cat, err := h.Store.CreateCategory(user.ID, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, r, http.StatusOK, "partials/category_row", cat)
}

func (h *Handlers) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	_ = r.ParseForm()
	cat, err := h.Store.UpdateCategoryName(user.ID, id, r.FormValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, r, http.StatusOK, "partials/category_row", cat)
}

func (h *Handlers) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := h.Store.DeleteCategory(user.ID, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Empty body + hx-swap="outerHTML" on the row removes the <li>.
	w.WriteHeader(http.StatusOK)
}

// --- Recipes ---------------------------------------------------------------

func (h *Handlers) ListRecipes(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	cats, err := h.Store.ListCategories(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filter := store.RecipeFilter{}
	var active *models.Category
	if catStr := r.URL.Query().Get("category"); catStr != "" {
		if id, err := strconv.ParseInt(catStr, 10, 64); err == nil {
			filter.CategoryID = &id
			for i, c := range cats {
				if c.ID == id {
					active = &cats[i]
					break
				}
			}
		}
	}
	recipes, err := h.Store.ListRecipes(user.ID, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := h.baseData(r)
	data.Categories = cats
	data.Recipes = recipes
	data.ActiveCategory = active
	h.render(w, r, http.StatusOK, "recipes_list", data)
}

func (h *Handlers) NewRecipe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	cats, _ := h.Store.ListCategories(user.ID)
	data := h.baseData(r)
	data.Categories = cats
	data.Recipe = &models.Recipe{}
	h.render(w, r, http.StatusOK, "recipe_form", data)
}

func (h *Handlers) ShowRecipe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rec, err := h.Store.RecipeByID(user.ID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rec.ShareURL = absoluteShareURL(r, rec.ShareToken)
	data := h.baseData(r)
	data.Recipe = rec
	h.render(w, r, http.StatusOK, "recipe_view", data)
}

func (h *Handlers) EditRecipe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rec, err := h.Store.RecipeByID(user.ID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cats, _ := h.Store.ListCategories(user.ID)
	data := h.baseData(r)
	data.Recipe = rec
	data.Categories = cats
	h.render(w, r, http.StatusOK, "recipe_form", data)
}

func (h *Handlers) CreateRecipe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	_ = r.ParseForm()
	in := readRecipeForm(r)
	if in.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	id, err := h.Store.CreateRecipe(user.ID, in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/recipes/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (h *Handlers) UpdateRecipe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	in := readRecipeForm(r)
	if err := h.Store.UpdateRecipe(user.ID, id, in); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/recipes/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (h *Handlers) ShareRecipe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rec, err := h.Store.ShareRecipe(user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rec.ShareURL = absoluteShareURL(r, rec.ShareToken)
	h.render(w, r, http.StatusOK, "partials/share_state", rec)
}

func (h *Handlers) RevokeShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rec, err := h.Store.RevokeShare(user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// rec.ShareToken is now "", so the partial renders the "not shared" state.
	h.render(w, r, http.StatusOK, "partials/share_state", rec)
}

// absoluteShareURL builds the full public URL for a share token using the
// incoming request's host. Honors X-Forwarded-Proto so Fly.io's TLS terminator
// produces https links.
func absoluteShareURL(r *http.Request, token string) string {
	if token == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/share/" + token
}

// ShowSharedRecipe is the public share view. Signed-out viewers see the
// recipe with no nav and a sign-up CTA; signed-in viewers see the nav and an
// "Add to my journal" save button (unless they're the recipe's owner).
func (h *Handlers) ShowSharedRecipe(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	rec, err := h.Store.RecipeByShareToken(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data := h.baseData(r)
	data.Recipe = rec
	// baseData already populates CurrentUser from the context when set by
	// RequireUser, but this route is open to anonymous viewers — read the
	// session directly so the nav header still appears for signed-in viewers.
	if data.CurrentUser == nil {
		if uid := h.Auth.CurrentUserID(r); uid != 0 {
			if u, err := h.Store.UserByID(uid); err == nil {
				data.CurrentUser = u
			}
		}
	}
	h.render(w, r, http.StatusOK, "shared_recipe", data)
}

// savePickerData is the view model for the inline category-picker partial that
// appears when a viewer wants to save a shared recipe and there's no matching
// category in their journal.
type savePickerData struct {
	Token        string
	RecipeName   string
	Categories   []models.Category
	SuggestedNew string // pre-fills the "new category" input with the source category name
}

// SaveSharedRecipe handles the first click on "Add to my journal" from the
// share page. If the source recipe has no category, or its category name
// matches one of the viewer's existing categories (case-insensitive), the
// copy happens immediately and the response tells HTMX to navigate to the
// new recipe. Otherwise the inline category picker is swapped in.
func (h *Handlers) SaveSharedRecipe(w http.ResponseWriter, r *http.Request) {
	viewer := auth.UserFromContext(r.Context())
	token := chi.URLParam(r, "token")
	src, err := h.Store.RecipeByShareToken(token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if src.UserID == viewer.ID {
		http.Error(w, "you already own this recipe", http.StatusBadRequest)
		return
	}

	var chosen *int64
	if src.CategoryName != "" {
		if cat, err := h.Store.CategoryByNameCI(viewer.ID, src.CategoryName); err == nil {
			chosen = &cat.ID
		} else if !errors.Is(err, store.ErrNotFound) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else {
			// Source has a category but the viewer has no match — show picker.
			cats, err := h.Store.ListCategories(viewer.ID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			h.render(w, r, http.StatusOK, "partials/save_category_picker", savePickerData{
				Token:        token,
				RecipeName:   src.Name,
				Categories:   cats,
				SuggestedNew: src.CategoryName,
			})
			return
		}
	}

	newID, err := h.Store.CopyRecipeFromShare(viewer.ID, token, chosen)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Redirect", "/recipes/"+strconv.FormatInt(newID, 10))
	w.WriteHeader(http.StatusOK)
}

// ConfirmSaveSharedRecipe is the second step of the save flow, invoked from
// the picker form. The form sends either an existing category_id, a
// new_category_name (which we create on the fly), or neither (uncategorized).
func (h *Handlers) ConfirmSaveSharedRecipe(w http.ResponseWriter, r *http.Request) {
	viewer := auth.UserFromContext(r.Context())
	token := chi.URLParam(r, "token")

	_ = r.ParseForm()
	var chosen *int64
	if v := strings.TrimSpace(r.FormValue("new_category_name")); v != "" {
		// New category takes precedence over the dropdown selection — the
		// viewer typed something, that's the clearer signal of intent.
		cat, err := h.Store.CreateCategory(viewer.ID, v)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		chosen = &cat.ID
	} else if v := r.FormValue("category_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "bad category_id", http.StatusBadRequest)
			return
		}
		// Sanity check: the id must belong to the viewer.
		if _, err := h.Store.CategoryByID(viewer.ID, id); err != nil {
			http.Error(w, "unknown category", http.StatusBadRequest)
			return
		}
		chosen = &id
	}

	newID, err := h.Store.CopyRecipeFromShare(viewer.ID, token, chosen)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Redirect", "/recipes/"+strconv.FormatInt(newID, 10))
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) DeleteRecipe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.Store.DeleteRecipe(user.ID, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/recipes", http.StatusSeeOther)
}

func readRecipeForm(r *http.Request) store.RecipeInput {
	in := store.RecipeInput{
		Name:         r.FormValue("name"),
		Ingredients:  r.FormValue("ingredients"),
		Process:      r.FormValue("process"),
		WinePairings: r.FormValue("wine_pairings"),
		Notes:        r.FormValue("notes"),
	}
	if v := r.FormValue("category_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.CategoryID = &id
		}
	}
	in.Difficulty = atoi(r.FormValue("difficulty"))
	in.Serves = atoi(r.FormValue("serves"))
	in.PrepMinutes = atoi(r.FormValue("prep_minutes"))
	in.CookMinutes = atoi(r.FormValue("cook_minutes"))
	return in
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
