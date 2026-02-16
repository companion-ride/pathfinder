package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

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
	Lat float64 `json:"lat" binding:"required"`
	Lon float64 `json:"lon" binding:"required"`
}

type CreateRouteResponse struct {
	RouteID       string        `json:"route_id"`
	Segments      []geo.Segment `json:"segments"`
	TotalDistance  string        `json:"total_distance"`
	TotalDuration string        `json:"total_duration"`
}

type FindClosestPointRequest struct {
	Lat float64 `json:"lat" binding:"required"`
	Lon float64 `json:"lon" binding:"required"`
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
	RawWalkResponse json.RawMessage  `json:"raw_walk_response"`
}

func parseRouteID(c *gin.Context) (string, bool) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid route id"})
		return "", false
	}
	return id, true
}

func (s *Server) ConfigHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"map_api_key": s.mapAPIKey})
}

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

	maneuversJSON, _ := json.Marshal(maneuvers)

	meta := database.RouteMetadata{
		TotalDistance:  routeInfo.TotalDistance,
		TotalDuration: routeInfo.TotalDuration,
		StartLon:      routeInfo.Waypoints[0].ProjectedPoint.Lon,
		StartLat:      routeInfo.Waypoints[0].ProjectedPoint.Lat,
		EndLon:        routeInfo.Waypoints[1].ProjectedPoint.Lon,
		EndLat:        routeInfo.Waypoints[1].ProjectedPoint.Lat,
		Maneuvers:     maneuversJSON,
	}

	routeID, err := s.db.SaveRoute(c.Request.Context(), mergedWKT, meta)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save route"})
		return
	}

	segments := make([]geo.Segment, 0, len(routeSegments))
	for _, rs := range routeSegments {
		coords, err := geo.ParseWKTLinestring(rs.WKT)
		if err != nil {
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
	_ = s.cache.SetRoute(c.Request.Context(), routeID, resp)
	_ = s.cache.SetRawResponse(c.Request.Context(), routeID, rawJSON)

	c.JSON(http.StatusCreated, resp)
}

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

	_ = s.cache.AddClosestPoint(c.Request.Context(), routeID, resp)

	c.JSON(http.StatusOK, resp)
}

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

	_ = s.cache.AddWalk(c.Request.Context(), routeID, resp)

	c.JSON(http.StatusOK, resp)
}

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

	coords, _ := geo.ParseWKTLinestring(row.Geometry)
	resp := CreateRouteResponse{
		RouteID:       row.ID,
		Segments:      []geo.Segment{{Coords: coords}},
		TotalDistance:  formatDistance(row.TotalDistance),
		TotalDuration: formatDuration(row.TotalDuration),
	}
	c.JSON(http.StatusOK, resp)
}

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
	return fmt.Sprintf("%d min", seconds/60)
}
