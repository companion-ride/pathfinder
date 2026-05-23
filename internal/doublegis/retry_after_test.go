package doublegis

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"delta_seconds", "5", 5 * time.Second},
		{"large_delta_seconds", "120", 120 * time.Second},
		{"empty_returns_default", "", 60 * time.Second},
		{"garbage_returns_default", "soonish", 60 * time.Second},
		{"negative_returns_default", "-5", 60 * time.Second},
		{"zero_returns_default", "0", 60 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRetryAfter(tc.header)
			if got != tc.want {
				t.Fatalf("parseRetryAfter(%q) = %v, want %v", tc.header, got, tc.want)
			}
		})
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	future := time.Now().Add(45 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(future)
	// Allow generous slack — the formatting drops sub-second precision.
	if got < 40*time.Second || got > 50*time.Second {
		t.Fatalf("parseRetryAfter(HTTP-date 45s ahead) = %v, want ~45s", got)
	}
}

func TestParseRetryAfter_PastHTTPDateReturnsDefault(t *testing.T) {
	past := time.Now().Add(-30 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(past)
	if got != 60*time.Second {
		t.Fatalf("parseRetryAfter(past date) = %v, want 60s default", got)
	}
}
