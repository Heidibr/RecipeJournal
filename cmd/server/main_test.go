package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCanonicalHost(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name     string
		appURL   string
		isDev    bool
		method   string
		host     string
		target   string
		wantCode int
		wantLoc  string
	}{
		{"canonical host passes", "https://denlillekokeboken.no", false, "GET", "denlillekokeboken.no", "/recipes", 200, ""},
		{"fly.dev redirects", "https://denlillekokeboken.no", false, "GET", "recipejournal.fly.dev", "/recipes/3?x=1", 308, "https://denlillekokeboken.no/recipes/3?x=1"},
		{"www redirects", "https://denlillekokeboken.no", false, "GET", "www.denlillekokeboken.no", "/", 308, "https://denlillekokeboken.no/"},
		{"POST keeps 308", "https://denlillekokeboken.no", false, "POST", "recipejournal.fly.dev", "/login", 308, "https://denlillekokeboken.no/login"},
		{"localhost is a no-op", "http://localhost:8080", false, "GET", "recipejournal.fly.dev", "/", 200, ""},
		{"dev is a no-op", "https://denlillekokeboken.no", true, "GET", "localhost:8080", "/", 200, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()
			canonicalHost(tt.appURL, tt.isDev, ok).ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Fatalf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			if got := rec.Header().Get("Location"); got != tt.wantLoc {
				t.Errorf("Location = %q, want %q", got, tt.wantLoc)
			}
		})
	}
}
