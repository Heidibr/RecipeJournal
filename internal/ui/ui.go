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
)

//go:embed templates
var templatesFS embed.FS

type Renderer struct {
	pages map[string]*template.Template
}

// Funcs are exposed to every template — kept tiny on purpose.
var funcs = template.FuncMap{
	"seq":         func(n int) []int { out := make([]int, n); for i := range out { out[i] = i + 1 }; return out },
	"dotsFilled":  func(level, dot int) bool { return dot <= level },
	"minutes":     formatMinutes,
	"lines": func(s string) []string {
		out := make([]string, 0)
		for _, l := range strings.Split(s, "\n") {
			if strings.TrimSpace(l) != "" {
				out = append(out, l)
			}
		}
		return out
	},
	"defaultStr":  func(s, d string) string { if s == "" { return d }; return s },
	"isSelected":  func(a, b int64) bool { return a == b },
	"isSelectedI": func(a, b int) bool { return a == b },
	"derefI64":    func(p *int64) int64 { if p == nil { return 0 }; return *p },
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

	r := &Renderer{pages: make(map[string]*template.Template)}

	pageEntries, err := fs.ReadDir(templatesFS, "templates/pages")
	if err != nil {
		return nil, fmt.Errorf("read pages dir: %w", err)
	}
	for _, e := range pageEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		bs, err := fs.ReadFile(templatesFS, "templates/pages/"+e.Name())
		if err != nil {
			return nil, err
		}
		t := template.New("base").Funcs(funcs)
		if _, err := t.Parse(string(layout)); err != nil {
			return nil, fmt.Errorf("parse layout for %s: %w", e.Name(), err)
		}
		for name, body := range partials {
			if _, err := t.New(name).Parse(body); err != nil {
				return nil, fmt.Errorf("parse partial %s into %s: %w", name, e.Name(), err)
			}
		}
		if _, err := t.Parse(string(bs)); err != nil {
			return nil, fmt.Errorf("parse page %s: %w", e.Name(), err)
		}
		r.pages[strings.TrimSuffix(e.Name(), ".html")] = t
	}

	// Standalone partial sets — used for HTMX fragment responses.
	for name, body := range partials {
		t := template.New(name).Funcs(funcs)
		if _, err := t.Parse(body); err != nil {
			return nil, fmt.Errorf("parse standalone partial %s: %w", name, err)
		}
		r.pages["partials/"+strings.TrimSuffix(name, ".html")] = t
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

// Render writes the named page to w. Data is whatever the page expects.
func (r *Renderer) Render(w http.ResponseWriter, status int, name string, data any) {
	t, ok := r.pages[name]
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
