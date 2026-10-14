// Package i18n holds the UI string catalogue and the language type.
//
// Scope is deliberate: this translates the application's own chrome — labels,
// buttons, headings, flash messages — and the names of the *seeded* default
// categories. It never touches anything the user typed. Recipe names,
// ingredients, process, wine pairings, notes and any category the user
// created or renamed are shown verbatim in whatever language they wrote them.
//
// Translation is bound at template-parse time (see package ui), not passed
// through request data, so a standalone HTMX partial rendered against a
// models.Category gets the same translator as a full page.
package i18n

import "fmt"

type Lang string

const (
	En Lang = "en"
	No Lang = "no"
)

// Default is used when nothing else is known about the request.
const Default = En

// Langs is the supported set, in the order the language switcher shows them.
var Langs = []Lang{En, No}

// Parse validates an untrusted language string (cookie value, form field,
// users.language column). The bool reports whether it was recognised, so
// callers can tell "absent" from "explicitly English".
func Parse(s string) (Lang, bool) {
	switch Lang(s) {
	case En:
		return En, true
	case No:
		return No, true
	}
	return Default, false
}

// Valid reports whether l is a language we actually have a catalogue for.
func (l Lang) Valid() bool { _, ok := Parse(string(l)); return ok }

// String makes Lang printable and usable as the <html lang> attribute value.
func (l Lang) String() string { return string(l) }

// Label is the name of the language in that language — what the switcher shows.
func (l Lang) Label() string {
	switch l {
	case No:
		return "norsk"
	default:
		return "english"
	}
}

// Other returns the language to switch to. With two languages this is simply
// the opposite one; it keeps the switcher template free of conditionals.
func (l Lang) Other() Lang {
	if l == No {
		return En
	}
	return No
}

// T looks up a key. Missing Norwegian keys fall back to English rather than
// rendering blank, and a key missing from both is returned as-is so the gap
// is obvious on screen instead of silently disappearing.
func (l Lang) T(key string) string {
	if m, ok := catalog[l]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	if s, ok := catalog[En][key]; ok {
		return s
	}
	return key
}

// Tf is T plus fmt.Sprintf, for strings with interpolated values. Using a
// format string (rather than concatenating a prefix and suffix in the
// template) lets a translation reorder the sentence around the value.
func (l Lang) Tf(key string, a ...any) string { return fmt.Sprintf(l.T(key), a...) }

// CategoryName resolves a category's display name. A non-empty i18nKey marks
// one of the seeded defaults and is translated; an empty key means the user
// named this category themselves, so their text is returned untouched.
func (l Lang) CategoryName(i18nKey, name string) string {
	if i18nKey == "" {
		return name
	}
	if s := l.T(i18nKey); s != i18nKey {
		return s
	}
	return name // unknown key: trust the stored name over showing a raw key
}

// Keys returns the set of keys defined for a language. Exported for the tests
// that assert the catalogues cannot drift apart.
func Keys(l Lang) map[string]bool {
	out := make(map[string]bool, len(catalog[l]))
	for k := range catalog[l] {
		out[k] = true
	}
	return out
}
