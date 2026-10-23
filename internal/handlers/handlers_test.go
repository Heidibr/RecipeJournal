package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	tests := []struct{ remoteAddr, want string }{
		{"203.0.113.7:51234", "203.0.113.7"},
		{"203.0.113.7", "203.0.113.7"},                   // Fly-Client-IP, no port
		{"[2a01:79c:cebf::1]:51234", "2a01:79c:cebf::1"}, // IPv6 with port
		{"2a01:79c:cebf::1", "2a01:79c:cebf::1"},         // IPv6 without port: must not be truncated
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tt.remoteAddr
		if got := clientIP(r); got != tt.want {
			t.Errorf("clientIP(%q) = %q, want %q", tt.remoteAddr, got, tt.want)
		}
	}
}
