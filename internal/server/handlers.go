package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

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
	RouteID        int           `json:"route_id"`
	Segments       []geo.Segment `json:"segments"`
	TotalDistance   string        `json:"total_distance"`
	TotalDuration  string        `json:"total_duration"`
}

type FindClosestPointRequest struct {
	Lat float64 `json:"lat" binding:"required"`
	Lon float64 `json:"lon" binding:"required"`
}

type FindClosestPointResponse struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type WalkToClosestResponse struct {
	ClosestLat   float64     `json:"closest_lat"`
	ClosestLon   float64     `json:"closest_lon"`
	Geometry     []geo.Coord `json:"geometry"`
	WalkDistance string      `json:"walk_distance"`
	WalkDuration string      `json:"walk_duration"`
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

	routeSegments, mergedWKT, routeInfo, err := s.doublegis.GetRoute(c.Request.Context(),
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

	c.JSON(http.StatusCreated, CreateRouteResponse{
		RouteID:       routeID,
		Segments:      segments,
		TotalDistance:  routeInfo.UITotalDistance,
		TotalDuration: routeInfo.UITotalDuration,
	})
}

func (s *Server) FindClosestPointHandler(c *gin.Context) {
	routeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid route id"})
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

	c.JSON(http.StatusOK, FindClosestPointResponse{Lat: closest.Lat, Lon: closest.Lon})
}

func (s *Server) WalkToClosestHandler(c *gin.Context) {
	routeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid route id"})
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

	walkWKT, walkInfo, err := s.doublegis.GetWalkingRoute(c.Request.Context(),
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

	c.JSON(http.StatusOK, WalkToClosestResponse{
		ClosestLat:   closest.Lat,
		ClosestLon:   closest.Lon,
		Geometry:     walkCoords,
		WalkDistance:  walkInfo.UITotalDistance,
		WalkDuration: walkInfo.UITotalDuration,
	})
}
