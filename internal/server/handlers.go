package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"pathfinder/internal/cache"
	"pathfinder/internal/database"
	"pathfinder/internal/geo"
)

type CreateRouteRequest struct {
	PointA PointInput `json:"point_a" binding:"required"`
	PointB PointInput `json:"point_b" binding:"required"`
}

type PointInput struct {
	Lat float64 `json:"lat" binding:"required,min=-90,max=90"`
	Lon float64 `json:"lon" binding:"required,min=-180,max=180"`
}

type CreateRouteResponse struct {
	RouteID       string        `json:"route_id"`
	Segments      []geo.Segment `json:"segments"`
	TotalDistance  string        `json:"total_distance"`
	TotalDuration string        `json:"total_duration"`
}

type CustomRouteCoord struct {
	Lat float64 `json:"lat" binding:"min=-90,max=90"`
	Lon float64 `json:"lon" binding:"min=-180,max=180"`
}

type CustomRouteSegment struct {
	Coords []CustomRouteCoord `json:"coords" binding:"required,min=2,dive"`
}

type CreateCustomRouteRequest struct {
	Segments []CustomRouteSegment `json:"segments" binding:"required,min=1,dive"`
}

type FindClosestPointRequest struct {
	Lat float64 `json:"lat" binding:"required,min=-90,max=90"`
	Lon float64 `json:"lon" binding:"required,min=-180,max=180"`
}

type FindClosestPointResponse struct {
	PassengerLat float64 `json:"passenger_lat"`
	PassengerLon float64 `json:"passenger_lon"`
	ClosestLat   float64 `json:"closest_lat"`
	ClosestLon   float64 `json:"closest_lon"`
}

type WalkToClosestResponse struct {
	PassengerLat    float64          `json:"passenger_lat"`
	PassengerLon    float64          `json:"passenger_lon"`
	ClosestLat      float64          `json:"closest_lat"`
	ClosestLon      float64          `json:"closest_lon"`
	Geometry        []geo.Coord      `json:"geometry"`
	WalkDistance    string           `json:"walk_distance"`
	WalkDuration    string           `json:"walk_duration"`
	RawWalkResponse json.RawMessage  `json:"raw_walk_response" swaggertype:"object"`
}

func parseRouteID(c *gin.Context) (string, bool) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid route id"})
		return "", false
	}
	return id, true
}

// ConfigHandler godoc
//
//	@Summary	Debug page configuration
//	@Description	Returns the 2GIS map API key used by the /app debug page. Registered only when ENABLE_DEBUG_PAGES=true.
//	@Tags		system
//	@Produce	json
//	@Success	200	{object}	map[string]string
//	@Router		/config [get]
func (s *Server) ConfigHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"map_api_key": s.mapAPIKey})
}

// CreateRouteHandler godoc
//
//	@Summary		Create a driving route
//	@Description	Calls 2GIS routing API, persists the route in PostGIS and caches it in Redis (3 h TTL).
//	@Tags			routes
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CreateRouteRequest	true	"Origin and destination"
//	@Success		201		{object}	CreateRouteResponse
//	@Failure		400		{object}	map[string]string
//	@Failure		500		{object}	map[string]string
//	@Failure		502		{object}	map[string]string
//	@Router			/routes [post]
func (s *Server) CreateRouteHandler(c *gin.Context) {
	var req CreateRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	routeSegments, mergedWKT, routeInfo, rawJSON, err := s.doublegis.GetRoute(c.Request.Context(),
		req.PointA.Lat, req.PointA.Lon,
		req.PointB.Lat, req.PointB.Lon,
	)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to get route: " + err.Error()})
		return
	}

	// Build deduplicated maneuvers list (multi-geometry maneuvers share the same comment)
	type maneuverEntry struct {
		Names    []string `json:"names"`
		Distance int      `json:"distance"`
	}
	var maneuvers []maneuverEntry
	seen := make(map[string]bool)
	for _, rs := range routeSegments {
		key := rs.Comment
		if seen[key] {
			continue
		}
		seen[key] = true
		maneuvers = append(maneuvers, maneuverEntry{
			Names:    rs.StreetNames,
			Distance: rs.Distance,
		})
	}

	maneuversJSON, err := json.Marshal(maneuvers)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to serialize maneuvers"})
		return
	}

	meta := database.RouteMetadata{
		TotalDistance:  routeInfo.TotalDistance,
		TotalDuration: routeInfo.TotalDuration,
		StartLon:      routeInfo.Waypoints[0].ProjectedPoint.Lon,
		StartLat:      routeInfo.Waypoints[0].ProjectedPoint.Lat,
		EndLon:        routeInfo.Waypoints[1].ProjectedPoint.Lon,
		EndLat:        routeInfo.Waypoints[1].ProjectedPoint.Lat,
		Maneuvers:     maneuversJSON,
	}

	slog.Info("saving route",
		"wkt", mergedWKT,
		"start_lon", meta.StartLon,
		"start_lat", meta.StartLat,
		"end_lon", meta.EndLon,
		"end_lat", meta.EndLat,
		"maneuvers_len", len(meta.Maneuvers))
	routeID, err := s.db.SaveRoute(c.Request.Context(), mergedWKT, meta)
	if err != nil {
		slog.Error("save route failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save route"})
		return
	}

	segments := make([]geo.Segment, 0, len(routeSegments))
	for _, rs := range routeSegments {
		coords, err := geo.ParseWKTLinestring(rs.WKT)
		if err != nil {
			slog.Warn("skipping segment with unparseable geometry", "error", err)
			continue
		}
		segments = append(segments, geo.Segment{
			Coords:      coords,
			Color:       rs.Color,
			Comment:     rs.Comment,
			StreetNames: rs.StreetNames,
			Distance:    rs.Distance,
			Duration:    rs.Duration,
		})
	}

	resp := CreateRouteResponse{
		RouteID:       routeID,
		Segments:      segments,
		TotalDistance:  routeInfo.UITotalDistance,
		TotalDuration: routeInfo.UITotalDuration,
	}

	// Best-effort cache — don't fail the request if Redis is down
	if err := s.cache.SetRoute(c.Request.Context(), routeID, resp); err != nil {
		slog.Warn("cache SetRoute failed", "error", err)
	}
	if err := s.cache.SetRawResponse(c.Request.Context(), routeID, rawJSON); err != nil {
		slog.Warn("cache SetRawResponse failed", "error", err)
	}

	c.JSON(http.StatusCreated, resp)
}

// CreateCustomRouteHandler godoc
//
//	@Summary		Create a custom (driver-drawn) route
//	@Description	Stores a caller-supplied polyline as a route, computing distance via haversine and duration from a configured average speed. No 2GIS call.
//	@Tags			routes
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CreateCustomRouteRequest	true	"Segments of coordinates"
//	@Success		201		{object}	CreateRouteResponse
//	@Failure		400		{object}	map[string]string
//	@Failure		500		{object}	map[string]string
//	@Router			/routes/custom [post]
func (s *Server) CreateCustomRouteHandler(c *gin.Context) {
	var req CreateCustomRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	total := 0
	for _, seg := range req.Segments {
		total += len(seg.Coords)
	}
	if total > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "too many coordinates (max 500)"})
		return
	}

	avgSpeed := s.pfConfig.Get().CustomRouteAvgSpeedMPS
	if avgSpeed <= 0 {
		avgSpeed = defaultCustomRouteAvgSpeedMPS
	}

	segments := make([]geo.Segment, 0, len(req.Segments))
	totalDistanceM := 0
	totalDurationS := 0
	for _, seg := range req.Segments {
		coords := make([]geo.Coord, len(seg.Coords))
		for i, cc := range seg.Coords {
			coords[i] = geo.Coord{Lat: cc.Lat, Lon: cc.Lon}
		}
		distM := geo.HaversineSum(coords)
		durS := int(math.Round(float64(distM) / avgSpeed))
		segments = append(segments, geo.Segment{
			Coords:      coords,
			Color:       "",
			Comment:     "",
			StreetNames: nil,
			Distance:    distM,
			Duration:    durS,
		})
		totalDistanceM += distM
		totalDurationS += durS
		if totalDistanceM > 200_000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "route exceeds 200 km limit"})
			return
		}
	}

	// Build merged WKT, deduping a coord bitwise-identical to the previous one
	// (segment-boundary endpoints) so PostGIS sees no zero-length sub-segments.
	var points []string
	var prevLon, prevLat float64
	have := false
	var firstLon, firstLat, lastLon, lastLat float64
	for _, seg := range segments {
		for _, co := range seg.Coords {
			if have && co.Lon == prevLon && co.Lat == prevLat {
				continue
			}
			points = append(points, fmt.Sprintf("%f %f", co.Lon, co.Lat))
			if !have {
				firstLon, firstLat = co.Lon, co.Lat
			}
			lastLon, lastLat = co.Lon, co.Lat
			prevLon, prevLat = co.Lon, co.Lat
			have = true
		}
	}
	if len(points) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "polyline collapses to a single point"})
		return
	}
	mergedWKT := "LINESTRING(" + strings.Join(points, ", ") + ")"

	meta := database.RouteMetadata{
		TotalDistance: totalDistanceM,
		TotalDuration: totalDurationS,
		StartLon:      firstLon,
		StartLat:      firstLat,
		EndLon:        lastLon,
		EndLat:        lastLat,
		Maneuvers:     json.RawMessage("[]"),
	}

	routeID, err := s.db.SaveRoute(c.Request.Context(), mergedWKT, meta)
	if err != nil {
		slog.Error("save custom route failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save route"})
		return
	}

	resp := CreateRouteResponse{
		RouteID:       routeID,
		Segments:      segments,
		TotalDistance: formatDistance(totalDistanceM),
		TotalDuration: formatDuration(totalDurationS),
	}

	// Best-effort cache — no raw 2GIS payload to store.
	if s.cache != nil {
		if err := s.cache.SetRoute(c.Request.Context(), routeID, resp); err != nil {
			slog.Warn("cache SetRoute failed", "error", err)
		}
	}

	slog.Info("custom route created", "route_id", routeID, "segment_count", len(req.Segments), "total_distance_m", totalDistanceM)
	c.JSON(http.StatusCreated, resp)
}

// FindClosestPointHandler godoc
//
//	@Summary		Find closest point on route
//	@Description	Projects a passenger coordinate onto the route polyline using s2 geometry. Result is appended to the route's Redis list.
//	@Tags			navigation
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Route UUID"	format(uuid)
//	@Param			body	body		FindClosestPointRequest	true	"Passenger coordinates"
//	@Success		200		{object}	FindClosestPointResponse
//	@Failure		400		{object}	map[string]string
//	@Failure		404		{object}	map[string]string
//	@Failure		500		{object}	map[string]string
//	@Router			/routes/{id}/closest-point [post]
func (s *Server) FindClosestPointHandler(c *gin.Context) {
	routeID, ok := parseRouteID(c)
	if !ok {
		return
	}

	var req FindClosestPointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	wkt, err := s.db.GetRouteGeometry(c.Request.Context(), routeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "route not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get route"})
		}
		return
	}

	closest, err := geo.FindClosestPoint(wkt, geo.Coord{Lat: req.Lat, Lon: req.Lon})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to compute closest point"})
		return
	}

	resp := FindClosestPointResponse{
		PassengerLat: req.Lat,
		PassengerLon: req.Lon,
		ClosestLat:   closest.Lat,
		ClosestLon:   closest.Lon,
	}

	if err := s.cache.AddClosestPoint(c.Request.Context(), routeID, resp); err != nil {
		slog.Warn("cache AddClosestPoint failed", "error", err)
	}

	c.JSON(http.StatusOK, resp)
}

// WalkToClosestHandler godoc
//
//	@Summary		Walking path to nearest point on route
//	@Description	Projects passenger onto route, calls 2GIS pedestrian API, caches result in Redis (3 h TTL).
//	@Tags			navigation
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Route UUID"	format(uuid)
//	@Param			body	body		FindClosestPointRequest	true	"Passenger coordinates"
//	@Success		200		{object}	WalkToClosestResponse
//	@Failure		400		{object}	map[string]string
//	@Failure		404		{object}	map[string]string
//	@Failure		500		{object}	map[string]string
//	@Failure		502		{object}	map[string]string
//	@Router			/routes/{id}/walk-to-closest [post]
func (s *Server) WalkToClosestHandler(c *gin.Context) {
	routeID, ok := parseRouteID(c)
	if !ok {
		return
	}

	var req FindClosestPointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	wkt, err := s.db.GetRouteGeometry(c.Request.Context(), routeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "route not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get route"})
		}
		return
	}

	closest, err := geo.FindClosestPoint(wkt, geo.Coord{Lat: req.Lat, Lon: req.Lon})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to compute closest point"})
		return
	}

	walkWKT, walkInfo, rawJSON, err := s.doublegis.GetWalkingRoute(c.Request.Context(),
		req.Lat, req.Lon, closest.Lat, closest.Lon,
	)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to get walking route: " + err.Error()})
		return
	}

	walkCoords, err := geo.ParseWKTLinestring(walkWKT)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse walking geometry"})
		return
	}

	resp := WalkToClosestResponse{
		PassengerLat:    req.Lat,
		PassengerLon:    req.Lon,
		ClosestLat:      closest.Lat,
		ClosestLon:      closest.Lon,
		Geometry:        walkCoords,
		WalkDistance:    walkInfo.UITotalDistance,
		WalkDuration:   walkInfo.UITotalDuration,
		RawWalkResponse: json.RawMessage(rawJSON),
	}

	if err := s.cache.AddWalk(c.Request.Context(), routeID, resp); err != nil {
		slog.Warn("cache AddWalk failed", "error", err)
	}

	c.JSON(http.StatusOK, resp)
}

// GetRouteHandler godoc
//
//	@Summary		Get a route
//	@Description	Returns route from Redis cache; falls back to PostGIS on cache miss.
//	@Tags			routes
//	@Produce		json
//	@Param			id	path		string	true	"Route UUID"	format(uuid)
//	@Success		200	{object}	CreateRouteResponse
//	@Failure		400	{object}	map[string]string
//	@Failure		404	{object}	map[string]string
//	@Failure		500	{object}	map[string]string
//	@Router			/routes/{id} [get]
// GET /routes/:id — return cached route, fall back to DB on cache miss.
func (s *Server) GetRouteHandler(c *gin.Context) {
	routeID, ok := parseRouteID(c)
	if !ok {
		return
	}

	// Try cache first — returns raw JSON, avoids re-marshal.
	raw, err := s.cache.GetRoute(c.Request.Context(), routeID)
	if err == nil {
		c.Data(http.StatusOK, "application/json", []byte(raw))
		return
	}

	// DB fallback — reconstruct a degraded response (no per-segment colors).
	row, err := s.db.GetRouteByID(c.Request.Context(), routeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "route not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get route"})
		}
		return
	}

	coords, err := geo.ParseWKTLinestring(row.Geometry)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse route geometry"})
		return
	}
	resp := CreateRouteResponse{
		RouteID:       row.ID,
		Segments:      []geo.Segment{{Coords: coords}},
		TotalDistance:  formatDistance(row.TotalDistance),
		TotalDuration: formatDuration(row.TotalDuration),
	}
	c.JSON(http.StatusOK, resp)
}

// GetClosestPointsHandler godoc
//
//	@Summary		Get cached closest-point results
//	@Description	Returns all closest-point entries from Redis list. 404 when empty or expired.
//	@Tags			navigation
//	@Produce		json
//	@Param			id	path		string					true	"Route UUID"	format(uuid)
//	@Success		200	{array}		FindClosestPointResponse
//	@Failure		400	{object}	map[string]string
//	@Failure		404	{object}	map[string]string
//	@Failure		500	{object}	map[string]string
//	@Router			/routes/{id}/closest-points [get]
// GET /routes/:id/closest-points — return cached list, 404 on miss.
func (s *Server) GetClosestPointsHandler(c *gin.Context) {
	routeID, ok := parseRouteID(c)
	if !ok {
		return
	}

	items, err := s.cache.GetClosestPoints(c.Request.Context(), routeID)
	if err != nil {
		if errors.Is(err, cache.ErrCacheMiss) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no cached closest points for this route"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "cache error"})
		}
		return
	}

	result := make([]json.RawMessage, len(items))
	for i, raw := range items {
		result[i] = json.RawMessage(raw)
	}
	c.JSON(http.StatusOK, result)
}

// GetWalksHandler godoc
//
//	@Summary		Get cached walk results
//	@Description	Returns all walk-to-closest entries from Redis list. 404 when empty or expired.
//	@Tags			navigation
//	@Produce		json
//	@Param			id	path		string					true	"Route UUID"	format(uuid)
//	@Success		200	{array}		WalkToClosestResponse
//	@Failure		400	{object}	map[string]string
//	@Failure		404	{object}	map[string]string
//	@Failure		500	{object}	map[string]string
//	@Router			/routes/{id}/walks [get]
// GET /routes/:id/walks — return cached list, 404 on miss.
func (s *Server) GetWalksHandler(c *gin.Context) {
	routeID, ok := parseRouteID(c)
	if !ok {
		return
	}

	items, err := s.cache.GetWalks(c.Request.Context(), routeID)
	if err != nil {
		if errors.Is(err, cache.ErrCacheMiss) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no cached walks for this route"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "cache error"})
		}
		return
	}

	result := make([]json.RawMessage, len(items))
	for i, raw := range items {
		result[i] = json.RawMessage(raw)
	}
	c.JSON(http.StatusOK, result)
}

// GetRawRouteHandler godoc
//
//	@Summary		Raw 2GIS API response
//	@Description	Returns the verbatim 2GIS routing response from Redis (available for 3 h TTL).
//	@Tags			routes
//	@Produce		json
//	@Param			id	path		string	true	"Route UUID"	format(uuid)
//	@Success		200	{object}	map[string]interface{}
//	@Failure		400	{object}	map[string]string
//	@Failure		404	{object}	map[string]string
//	@Failure		500	{object}	map[string]string
//	@Router			/routes/{id}/raw [get]
// GET /routes/:id/raw — return the raw 2GIS API response from cache.
func (s *Server) GetRawRouteHandler(c *gin.Context) {
	routeID, ok := parseRouteID(c)
	if !ok {
		return
	}

	raw, err := s.cache.GetRawResponse(c.Request.Context(), routeID)
	if err != nil {
		if errors.Is(err, cache.ErrCacheMiss) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no cached raw response for this route"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "cache error"})
		}
		return
	}

	c.Data(http.StatusOK, "application/json", []byte(raw))
}

func formatDistance(meters int) string {
	if meters >= 1000 {
		return fmt.Sprintf("%.1f km", float64(meters)/1000)
	}
	return fmt.Sprintf("%d m", meters)
}

func formatDuration(seconds int) string {
	if seconds >= 3600 {
		return fmt.Sprintf("%d h %d min", seconds/3600, (seconds%3600)/60)
	}
	if seconds < 60 {
		return "1 min"
	}
	return fmt.Sprintf("%d min", seconds/60)
}

// SwaggerJSONHandler serves swagger.json with host/basePath overridden by env vars.
// Set SWAGGER_HOST and SWAGGER_BASE_PATH in production (e.g. companion.kopir.uk and /api/pathfinder).
func (s *Server) SwaggerJSONHandler(c *gin.Context) {
	data, err := os.ReadFile("./docs/swagger.json")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "swagger.json not found"})
		return
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse swagger.json"})
		return
	}
	if h := os.Getenv("SWAGGER_HOST"); h != "" {
		spec["host"] = h
	}
	if bp := os.Getenv("SWAGGER_BASE_PATH"); bp != "" {
		spec["basePath"] = bp
	}
	c.JSON(http.StatusOK, spec)
}
