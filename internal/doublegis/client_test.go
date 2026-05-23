package doublegis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// minimalSuccessBody returns a 2GIS routing response that satisfies the parser
// in client.go: at least one Result with two Waypoints and one Maneuver
// whose OutcomingPath has a parseable LINESTRING.
func minimalSuccessBody(t *testing.T) []byte {
	t.Helper()
	resp := RoutingResponse{
		Result: []Route{{
			TotalDistance:  100,
			TotalDuration:  60,
			UITotalDuration: "1 min",
			Waypoints: []Waypoint{
				{ProjectedPoint: WaypointPoint{Lon: 71.0, Lat: 51.0}},
				{ProjectedPoint: WaypointPoint{Lon: 71.1, Lat: 51.1}},
			},
			Maneuvers: []Maneuver{{
				Comment: "go",
				OutcomingPath: &OutcomingPath{
					Distance: 100,
					Duration: 60,
					Names:    []string{"Main St"},
					Geometry: []GeometrySegment{{
						Selection: "LINESTRING(71.0 51.0, 71.1 51.1)",
						Color:     "normal",
					}},
				},
			}},
		}},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func newTestClient(t *testing.T, keys []string, baseURL string) *Client {
	t.Helper()
	t.Setenv("DOUBLEGIS_BASE_URL", baseURL)
	return NewClient(keys, func() RouteParams {
		return RouteParams{
			RouteMode:   "fastest",
			TrafficMode: "jam",
			Locale:      "en",
			Timeout:     5 * time.Second,
		}
	})
}

func TestClient_SuccessFirstKey(t *testing.T) {
	body := minimalSuccessBody(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer srv.Close()

	c := newTestClient(t, []string{"k1", "k2"}, srv.URL)
	_, _, _, _, err := c.GetRoute(context.Background(), 51.0, 71.0, 51.1, 71.1)
	if err != nil {
		t.Fatalf("GetRoute error: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

func TestClient_RetriesAfter429WithNextKey(t *testing.T) {
	body := minimalSuccessBody(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		key := r.URL.Query().Get("key")
		if n == 1 {
			if key != "k1" {
				t.Errorf("first call key = %q, want \"k1\"", key)
			}
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if key != "k2" {
			t.Errorf("retry call key = %q, want \"k2\"", key)
		}
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer srv.Close()

	c := newTestClient(t, []string{"k1", "k2"}, srv.URL)
	_, _, _, _, err := c.GetRoute(context.Background(), 51.0, 71.0, 51.1, 71.1)
	if err != nil {
		t.Fatalf("GetRoute error after rotation: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("upstream calls = %d, want 2", got)
	}
}

func TestClient_AllKeysExhaustedReturnsError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestClient(t, []string{"k1", "k2", "k3"}, srv.URL)
	_, _, _, _, err := c.GetRoute(context.Background(), 51.0, 71.0, 51.1, 71.1)
	if err == nil {
		t.Fatal("expected error when all keys are 429")
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("upstream calls = %d, want 3 (one per key)", got)
	}
}

func TestClient_NonRateLimitErrorReturnedImmediately(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, []string{"k1", "k2"}, srv.URL)
	_, _, _, _, err := c.GetRoute(context.Background(), 51.0, 71.0, 51.1, 71.1)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 (no rotation on non-429)", got)
	}
}

func TestClient_RespectsContextCancellation(t *testing.T) {
	// Server always returns 429 so the client wants to retry; ctx cancel must abort.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestClient(t, []string{"k1", "k2", "k3"}, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, _, _, _, err := c.GetRoute(ctx, 51.0, 71.0, 51.1, 71.1)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestClient_RetryAfterHeaderControlsCooldown(t *testing.T) {
	// One key, server returns 429 with a huge Retry-After. On the next request
	// the retry loop's first acquire() returns ok=false (key still cooling
	// down), so doRoutingRequest errors out without making any HTTP call.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", strconv.Itoa(int((10 * time.Minute).Seconds())))
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestClient(t, []string{"only"}, srv.URL)
	_, _, _, _, err := c.GetRoute(context.Background(), 51.0, 71.0, 51.1, 71.1)
	if err == nil {
		t.Fatal("first call expected to error after exhausting single key")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("after first call: upstream calls = %d, want 1", got)
	}

	_, _, _, _, err = c.GetRoute(context.Background(), 51.0, 71.0, 51.1, 71.1)
	if err == nil {
		t.Fatal("second call expected to error while key is cooling down")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("after second call: upstream calls = %d, want 1 (no request while cooling)", got)
	}
}

func TestClient_NetworkErrorReturnedImmediatelyNoRotation(t *testing.T) {
	// Close the server before calling so httpClient.Do fails on the first key.
	// A transient transport error must NOT trigger key rotation — it's not a
	// rate-limit signal.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := newTestClient(t, []string{"k1", "k2"}, url)
	_, _, _, _, err := c.GetRoute(context.Background(), 51.0, 71.0, 51.1, 71.1)
	if err == nil {
		t.Fatal("expected error when transport fails")
	}

	// Verify the keyring's "current" pointer hasn't advanced: a fresh request
	// against a server that returns 200 should use k1, not k2.
	var keyUsed string
	body := minimalSuccessBody(t)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keyUsed = r.URL.Query().Get("key")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer srv2.Close()

	// Redirect the existing client at the second server without rebuilding it,
	// so we observe the same keyring state.
	c.baseURL = srv2.URL

	if _, _, _, _, err := c.GetRoute(context.Background(), 51.0, 71.0, 51.1, 71.1); err != nil {
		t.Fatalf("follow-up GetRoute error: %v", err)
	}
	if keyUsed != "k1" {
		t.Fatalf("network error rotated key: next request used %q, want \"k1\"", keyUsed)
	}
}
