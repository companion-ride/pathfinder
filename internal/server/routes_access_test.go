package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// accessTestServer builds the real router from the repo root so ./web resolves.
func accessTestServer(t *testing.T) http.Handler {
	t.Chdir("../..")
	s := &Server{jwtSecretKey: "test", mapAPIKey: "map-key"}
	return s.RegisterRoutes()
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr
}

func TestDebugRoutesDisabledByDefault(t *testing.T) {
	t.Setenv("ENABLE_DEBUG_PAGES", "")
	h := accessTestServer(t)
	for _, path := range []string{"/config", "/app"} {
		rr := get(h, path)
		if rr.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want %d", path, rr.Code, http.StatusNotFound)
		}
		if strings.Contains(rr.Body.String(), "map-key") {
			t.Errorf("GET %s leaked the map key", path)
		}
	}
}

func TestDebugRoutesEnabled(t *testing.T) {
	t.Setenv("ENABLE_DEBUG_PAGES", "true")
	h := accessTestServer(t)
	if rr := get(h, "/app"); rr.Code != http.StatusOK {
		t.Errorf("GET /app = %d, want %d", rr.Code, http.StatusOK)
	}
	rr := get(h, "/config")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "map-key") {
		t.Errorf("GET /config = %d %q, want 200 with map key", rr.Code, rr.Body.String())
	}
}
