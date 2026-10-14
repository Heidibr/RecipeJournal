// Package ui owns template parsing and rendering for the recipe journal.
// Templates are embedded into the binary so deploys ship as a single artifact.
package ui

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/heidi/recipejournal/internal/i18n"
)

//go:embed templates
var templatesFS embed.FS

// Renderer holds one fully parsed template set per supported language. The
// translation funcs (T, Tf, catName) are baked into each set at parse time
// rather than passed in via request data -- that way a standalone HTMX
// partial rendered against a models.Category or models.Recipe gets the right
// language without every data struct having to carry it.
type Renderer struct {
	sets map[i18n.Lang]map[string]*template.Template
}

// Funcs are exposed to every template — kept tiny on purpose.
var funcs = template.FuncMap{
	"seq": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i + 1
		}
		return out
	},
	"dotsFilled": func(level, dot int) bool { return dot <= level },
	"minutes":    formatMinutes,
	"lines": func(s string) []string {
		out := make([]string, 0)
		for _, l := range strings.Split(s, "\n") {
			if strings.TrimSpace(l) != "" {
				out = append(out, l)
			}
		}
		return out
	},
	"defaultStr": func(s, d string) string {
		if s == "" {
			return d
		}
		return s
	},
	"isSelected":  func(a, b int64) bool { return a == b },
	"isSelectedI": func(a, b int) bool { return a == b },
	"derefI64": func(p *int64) int64 {
		if p == nil {
			return 0
		}
		return *p
	},
}

// langFuncs returns the funcs whose behaviour depends on the language, to be
// merged over the shared set when parsing that language's templates.
func langFuncs(l i18n.Lang) template.FuncMap {
	return template.FuncMap{
		"T":  l.T,
		"Tf": l.Tf,
		// catName renders a category's display name: translated when it is a
		// seeded default, verbatim when the user named it themselves.
		"catName":        l.CategoryName,
		"lang":           func() string { return l.String() },
		"langLabel":      func() string { return l.Label() },
		"otherLang":      func() string { return l.Other().String() },
		"otherLangLabel": func() string { return l.Other().Label() },
	}
}

func formatMinutes(m int) string {
	if m <= 0 {
		return ""
	}
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	h, rest := m/60, m%60
	if rest == 0 {
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, rest)
}

// New parses the layout + every page template into a per-page set. Each page is
// rendered against the same base layout, so the layout always wins. Every page
// set also includes all partials, so {{template "foo.html"}} works inside pages.
func New() (*Renderer, error) {
	layout, err := fs.ReadFile(templatesFS, "templates/layouts/base.html")
	if err != nil {
		return nil, fmt.Errorf("read base layout: %w", err)
	}

	partials, err := loadPartials()
	if err != nil {
		return nil, err
	}

	pageEntries, err := fs.ReadDir(templatesFS, "templates/pages")
	if err != nil {
		return nil, fmt.Errorf("read pages dir: %w", err)
	}

	r := &Renderer{sets: make(map[i18n.Lang]map[string]*template.Template, len(i18n.Langs))}
	for _, lang := range i18n.Langs {
		// Shared funcs first, then the language-bound ones.
		fm := template.FuncMap{}
		for k, v := range funcs {
			fm[k] = v
		}
		for k, v := range langFuncs(lang) {
			fm[k] = v
		}

		set := make(map[string]*template.Template)
		for _, e := range pageEntries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
				continue
			}
			bs, err := fs.ReadFile(templatesFS, "templates/pages/"+e.Name())
			if err != nil {
				return nil, err
			}
			t := template.New("base").Funcs(fm)
			if _, err := t.Parse(string(layout)); err != nil {
				return nil, fmt.Errorf("parse layout for %s (%s): %w", e.Name(), lang, err)
			}
			for name, body := range partials {
				if _, err := t.New(name).Parse(body); err != nil {
					return nil, fmt.Errorf("parse partial %s into %s (%s): %w", name, e.Name(), lang, err)
				}
			}
			if _, err := t.Parse(string(bs)); err != nil {
				return nil, fmt.Errorf("parse page %s (%s): %w", e.Name(), lang, err)
			}
			set[strings.TrimSuffix(e.Name(), ".html")] = t
		}

		// Standalone partial sets — used for HTMX fragment responses.
		for name, body := range partials {
			t := template.New(name).Funcs(fm)
			if _, err := t.Parse(body); err != nil {
				return nil, fmt.Errorf("parse standalone partial %s (%s): %w", name, lang, err)
			}
			set["partials/"+strings.TrimSuffix(name, ".html")] = t
		}

		r.sets[lang] = set
	}

	return r, nil
}

// loadPartials reads every templates/partials/*.html file into a name→body map.
func loadPartials() (map[string]string, error) {
	out := make(map[string]string)
	entries, err := fs.ReadDir(templatesFS, "templates/partials")
	if err != nil {
		return nil, fmt.Errorf("read partials dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		bs, err := fs.ReadFile(templatesFS, "templates/partials/"+e.Name())
		if err != nil {
			return nil, err
		}
		out[e.Name()] = string(bs)
	}
	return out, nil
}

// Render writes the named page to w in the given language. Data is whatever
// the page expects; all translated strings come from the template set rather
// than from data.
func (r *Renderer) Render(w http.ResponseWriter, status int, lang i18n.Lang, name string, data any) {
	set, ok := r.sets[lang]
	if !ok {
		set = r.sets[i18n.Default]
	}
	t, ok := set[name]
	if !ok {
		http.Error(w, "template "+name+" not found", http.StatusInternalServerError)
		return
	}
	// Buffer first so a render error doesn't leave a half-written response.
	var buf bytes.Buffer
	exec := "base"
	if strings.HasPrefix(name, "partials/") {
		exec = strings.TrimPrefix(name, "partials/") + ".html"
	}
	if err := t.ExecuteTemplate(&buf, exec, data); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
