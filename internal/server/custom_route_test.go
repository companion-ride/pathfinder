package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"pathfinder/internal/database"
)

// fakeDB is a minimal database.Service for handler tests.
type fakeDB struct {
	savedWKT  string
	savedMeta database.RouteMetadata
	returnID  string
	saveErr   error
}

func (f *fakeDB) Health() map[string]string { return nil }
func (f *fakeDB) Close() error              { return nil }
func (f *fakeDB) SaveRoute(ctx context.Context, wkt string, meta database.RouteMetadata) (string, error) {
	f.savedWKT = wkt
	f.savedMeta = meta
	if f.saveErr != nil {
		return "", f.saveErr
	}
	if f.returnID == "" {
		return "00000000-0000-0000-0000-000000000001", nil
	}
	return f.returnID, nil
}
func (f *fakeDB) GetRouteGeometry(ctx context.Context, id string) (string, error) { return "", nil }
func (f *fakeDB) GetRouteByID(ctx context.Context, id string) (*database.RouteRow, error) {
	return nil, nil
}

func newTestServer(db database.Service, avgSpeed float64) *Server {
	cfg := DefaultPathfinderConfig()
	cfg.CustomRouteAvgSpeedMPS = avgSpeed
	return &Server{
		db:       db,
		cache:    nil, // handler guards nil cache
		pfConfig: NewPathfinderConfigLoader(cfg),
	}
}

func doCustomRoute(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/routes/custom", s.CreateCustomRouteHandler)
	req := httptest.NewRequest(http.MethodPost, "/routes/custom", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestCustomRoute_EmptySegments(t *testing.T) {
	rr := doCustomRoute(t, newTestServer(&fakeDB{}, 11.11), `{"segments":[]}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomRoute_SegmentTooFewCoords(t *testing.T) {
	body := `{"segments":[{"coords":[{"lat":51.1,"lon":71.4}]}]}`
	rr := doCustomRoute(t, newTestServer(&fakeDB{}, 11.11), body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomRoute_LatOutOfRange(t *testing.T) {
	body := `{"segments":[{"coords":[{"lat":91,"lon":71.4},{"lat":51.1,"lon":71.4}]}]}`
	rr := doCustomRoute(t, newTestServer(&fakeDB{}, 11.11), body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomRoute_TooManyCoords(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"segments":[{"coords":[`)
	for i := 0; i < 501; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"lat":51.1,"lon":71.4}`)
	}
	sb.WriteString(`]}]}`)
	rr := doCustomRoute(t, newTestServer(&fakeDB{}, 11.11), sb.String())
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomRoute_ExceedsDistanceCap(t *testing.T) {
	// Two points ~300 km apart (≈2.7 deg lat) > 200 km cap.
	body := `{"segments":[{"coords":[{"lat":51.0,"lon":71.4},{"lat":53.7,"lon":71.4}]}]}`
	rr := doCustomRoute(t, newTestServer(&fakeDB{}, 11.11), body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "200 km") {
		t.Errorf("expected 200 km error, got %s", rr.Body.String())
	}
}

func TestCustomRoute_LatZeroAccepted(t *testing.T) {
	db := &fakeDB{}
	body := `{"segments":[{"coords":[{"lat":0,"lon":0},{"lat":0.001,"lon":0}]}]}`
	rr := doCustomRoute(t, newTestServer(db, 11.11), body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomRoute_HappyPath(t *testing.T) {
	db := &fakeDB{returnID: "11111111-1111-1111-1111-111111111111"}
	// Two segments sharing the boundary point {51.11,71.40}.
	body := `{"segments":[
		{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.40}]},
		{"coords":[{"lat":51.11,"lon":71.40},{"lat":51.12,"lon":71.40}]}
	]}`
	rr := doCustomRoute(t, newTestServer(db, 11.11), body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var resp CreateRouteResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp.RouteID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("route_id = %q", resp.RouteID)
	}
	if len(resp.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(resp.Segments))
	}
	if resp.Segments[0].Distance < 1100 || resp.Segments[0].Distance > 1125 {
		t.Errorf("seg0 distance = %d, want ~1112", resp.Segments[0].Distance)
	}
	if resp.Segments[0].Duration < 95 || resp.Segments[0].Duration > 105 {
		t.Errorf("seg0 duration = %d, want ~100", resp.Segments[0].Duration)
	}
	if resp.Segments[0].Color != "" || resp.Segments[0].Comment != "" || len(resp.Segments[0].StreetNames) != 0 {
		t.Errorf("expected empty 2GIS metadata, got %+v", resp.Segments[0])
	}
	// Merged WKT dedupes the shared boundary point: 3 distinct points, not 4.
	if got := strings.Count(db.savedWKT, ","); got != 2 {
		t.Errorf("WKT should have 3 points (2 commas), got %d commas: %s", got, db.savedWKT)
	}
	if string(db.savedMeta.Maneuvers) != "[]" {
		t.Errorf("maneuvers = %q, want []", string(db.savedMeta.Maneuvers))
	}
	if db.savedMeta.StartLat != 51.10 || db.savedMeta.EndLat != 51.12 {
		t.Errorf("start/end lat = %v/%v, want 51.10/51.12", db.savedMeta.StartLat, db.savedMeta.EndLat)
	}
}

func TestCustomRoute_AvgSpeedZeroFallsBack(t *testing.T) {
	db := &fakeDB{}
	body := `{"segments":[{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.40}]}]}`
	rr := doCustomRoute(t, newTestServer(db, 0), body) // avg speed 0 → fallback
	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	var resp CreateRouteResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Segments[0].Duration < 95 || resp.Segments[0].Duration > 105 {
		t.Errorf("duration with fallback = %d, want ~100", resp.Segments[0].Duration)
	}
}

func TestCustomRoute_DBError(t *testing.T) {
	db := &fakeDB{saveErr: errors.New("connection reset")}
	body := `{"segments":[{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.40}]}]}`
	rr := doCustomRoute(t, newTestServer(db, 11.11), body)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500; body=%s", rr.Code, rr.Body.String())
	}
}
