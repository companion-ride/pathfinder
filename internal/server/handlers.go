package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

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
	RouteID  int           `json:"route_id"`
	Segments []geo.Segment `json:"segments"`
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
	ClosestLat float64     `json:"closest_lat"`
	ClosestLon float64     `json:"closest_lon"`
	Geometry   []geo.Coord `json:"geometry"`
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

	routeSegments, mergedWKT, err := s.doublegis.GetRoute(c.Request.Context(),
		req.PointA.Lat, req.PointA.Lon,
		req.PointB.Lat, req.PointB.Lon,
	)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to get route: " + err.Error()})
		return
	}

	routeID, err := s.db.SaveRoute(c.Request.Context(), mergedWKT)
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
			Coords: coords,
			Color:  rs.Color,
		})
	}

	c.JSON(http.StatusCreated, CreateRouteResponse{RouteID: routeID, Segments: segments})
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

	walkWKT, err := s.doublegis.GetWalkingRoute(c.Request.Context(),
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
		ClosestLat: closest.Lat,
		ClosestLon: closest.Lon,
		Geometry:   walkCoords,
	})
}
