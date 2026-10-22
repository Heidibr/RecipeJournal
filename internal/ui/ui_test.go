package ui

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/heidi/recipejournal/internal/i18n"
	"github.com/heidi/recipejournal/internal/models"
)

// TestNewParsesEveryLanguage is the cheap guard that every template parses
// against every language's func map -- a typo'd func name or an unclosed
// action fails here rather than at request time in production.
func TestNewParsesEveryLanguage(t *testing.T) {
	r, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, l := range i18n.Langs {
		set, ok := r.sets[l]
		if !ok {
			t.Fatalf("no template set for %s", l)
		}
		for _, want := range []string{"login", "recipes_list", "categories", "recipe_form", "partials/category_row"} {
			if _, ok := set[want]; !ok {
				t.Errorf("%s: template %q missing", l, want)
			}
		}
	}
}

// TestRenderTranslatesChrome renders a real page in both languages and checks
// the chrome changes while the user's own text does not.
func TestRenderTranslatesChrome(t *testing.T) {
	r, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Mirrors the fields of handlers.pageData that these templates touch.
	type pd struct {
		Lang           i18n.Lang
		CurrentUser    *models.User
		CSRFToken      string
		Flash          string
		Error          string
		Categories     []models.Category
		ActiveCategory *models.Category
		Recipes        []models.Recipe
	}
	data := pd{
		CurrentUser: &models.User{Name: "Heidi"},
		Categories: []models.Category{
			{ID: 1, Name: "Main Dishes", I18nKey: "cat.main_dishes"}, // seeded
			{ID: 2, Name: "Mormors kaker"},                           // user's own
		},
		Recipes: []models.Recipe{{ID: 1, Name: "Fiskegrateng"}},
	}

	for _, tc := range []struct {
		lang        i18n.Lang
		wantChrome  string
		wantMissing string
	}{
		{i18n.En, "Main Dishes", "Hovedretter"},
		{i18n.No, "Hovedretter", "Main Dishes"},
	} {
		w := httptest.NewRecorder()
		data.Lang = tc.lang
		r.Render(w, 200, tc.lang, "recipes_list", data)
		body := w.Body.String()

		if !strings.Contains(body, tc.wantChrome) {
			t.Errorf("%s: body missing translated category %q", tc.lang, tc.wantChrome)
		}
		if strings.Contains(body, tc.wantMissing) {
			t.Errorf("%s: body still contains %q from the other language", tc.lang, tc.wantMissing)
		}
		// The user's own category and recipe names must survive untouched.
		if !strings.Contains(body, "Mormors kaker") {
			t.Errorf("%s: user-created category name was lost", tc.lang)
		}
		if !strings.Contains(body, "Fiskegrateng") {
			t.Errorf("%s: recipe name was lost", tc.lang)
		}
		if !strings.Contains(body, `<html lang="`+tc.lang.String()+`"`) {
			t.Errorf("%s: <html lang> attribute not set", tc.lang)
		}
	}
}
