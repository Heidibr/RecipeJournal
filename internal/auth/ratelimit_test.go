package auth

import (
	"testing"
	"time"
)

func TestLimiterTake(t *testing.T) {
	l := newLimiter(time.Hour, 3)
	for i := range 3 {
		if !l.Take("ip-a") {
			t.Fatalf("attempt %d refused, want allowed", i+1)
		}
	}
	if l.Take("ip-a") {
		t.Error("4th attempt allowed, want refused")
	}
	if !l.Take("ip-b") {
		t.Error("other key refused, want its own budget")
	}
}

func TestLimiterWindowExpires(t *testing.T) {
	l := newLimiter(time.Hour, 1)
	l.Take("k")
	// Age the recorded attempt past the window instead of sleeping.
	l.hits["k"][0] = time.Now().Add(-2 * time.Hour)
	if !l.Take("k") {
		t.Error("attempt after the window refused, want allowed")
	}
}
