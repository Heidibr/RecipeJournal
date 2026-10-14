package i18n_test

import (
	"regexp"
	"sort"
	"testing"

	"github.com/heidi/recipejournal/internal/i18n"
	"github.com/heidi/recipejournal/internal/store"
)

// TestSeededCategoriesHaveTranslations guards the one coupling that is easy to
// break: adding a category to store.DefaultCategories without adding its key
// to the catalogue would silently show a raw key (or the English name) in
// Norwegian.
func TestSeededCategoriesHaveTranslations(t *testing.T) {
	for _, dc := range store.DefaultCategories {
		if dc.Key == "" {
			t.Errorf("default category %q has no i18n key", dc.Name)
			continue
		}
		for _, l := range i18n.Langs {
			if got := l.T(dc.Key); got == dc.Key {
				t.Errorf("category key %q has no %s translation", dc.Key, l)
			}
		}
		// English must round-trip to the name stored in the row, otherwise
		// the migration's name-matching backfill and the catalogue disagree.
		if got := i18n.En.T(dc.Key); got != dc.Name {
			t.Errorf("key %q: English is %q, stored name is %q", dc.Key, got, dc.Name)
		}
	}
}

// TestNoMissingTranslations asserts both languages define exactly the same
// keys. A key present in English but absent in Norwegian would silently fall
// back to English on screen, which is hard to spot by clicking around.
func TestNoMissingTranslations(t *testing.T) {
	keys := map[i18n.Lang]map[string]bool{}
	for _, l := range i18n.Langs {
		keys[l] = i18n.Keys(l)
	}
	for _, a := range i18n.Langs {
		for _, b := range i18n.Langs {
			if a == b {
				continue
			}
			var missing []string
			for k := range keys[a] {
				if !keys[b][k] {
					missing = append(missing, k)
				}
			}
			sort.Strings(missing)
			if len(missing) > 0 {
				t.Errorf("%d key(s) in %s missing from %s: %v", len(missing), a, b, missing)
			}
		}
	}
}

// TestFormatVerbsMatch catches a translation that drops or adds a %s / %d.
// Tf would then render a stray "%!s(MISSING)" into the page.
func TestFormatVerbsMatch(t *testing.T) {
	verb := regexp.MustCompile(`%[a-zA-Z]`)
	for k := range i18n.Keys(i18n.En) {
		want := verb.FindAllString(i18n.En.T(k), -1)
		for _, l := range i18n.Langs {
			if l == i18n.En {
				continue
			}
			got := verb.FindAllString(l.T(k), -1)
			if len(got) != len(want) {
				t.Errorf("key %q: English has verbs %v, %s has %v", k, want, l, got)
			}
		}
	}
}

func TestParseRejectsUnknown(t *testing.T) {
	for _, in := range []string{"", "de", "nb", "EN", "../en", "no-NO"} {
		if l, ok := i18n.Parse(in); ok {
			t.Errorf("Parse(%q) accepted as %s, want rejected", in, l)
		} else if l != i18n.Default {
			t.Errorf("Parse(%q) returned %s, want the default %s", in, l, i18n.Default)
		}
	}
	for _, in := range []string{"en", "no"} {
		if _, ok := i18n.Parse(in); !ok {
			t.Errorf("Parse(%q) rejected, want accepted", in)
		}
	}
}

// TestCategoryName is the core rule of this feature: seeded categories
// translate, anything the user typed does not.
func TestCategoryName(t *testing.T) {
	// A seeded category translates.
	if got := i18n.No.CategoryName("cat.main_dishes", "Main Dishes"); got != "Hovedretter" {
		t.Errorf("seeded category: got %q, want %q", got, "Hovedretter")
	}
	// A user-created category (no key) is shown verbatim, in any language.
	for _, l := range i18n.Langs {
		if got := l.CategoryName("", "Mormors kaker"); got != "Mormors kaker" {
			t.Errorf("%s: user category was altered to %q", l, got)
		}
	}
	// An unknown key falls back to the stored name rather than showing the key.
	if got := i18n.No.CategoryName("cat.removed_later", "Leftovers"); got != "Leftovers" {
		t.Errorf("unknown key: got %q, want the stored name", got)
	}
}
