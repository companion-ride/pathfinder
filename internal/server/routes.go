package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"pathfinder/internal/auth"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := gin.Default()

	// Public routes
	r.GET("/", s.HelloWorldHandler)
	r.GET("/health", s.healthHandler)
	r.StaticFile("/app", "./web/index.html")
	r.StaticFile("/docs", "./web/swagger.html")
	r.GET("/docs/swagger.json", s.SwaggerJSONHandler)
	r.GET("/config", s.ConfigHandler)

	// Protected routes
	protected := r.Group("/")
	protected.Use(auth.Middleware(s.jwtSecretKey))
	protected.POST("/routes", s.CreateRouteHandler)
	protected.GET("/routes/:id", s.GetRouteHandler)
	protected.GET("/routes/:id/raw", s.GetRawRouteHandler)
	protected.POST("/routes/:id/closest-point", s.FindClosestPointHandler)
	protected.GET("/routes/:id/closest-points", s.GetClosestPointsHandler)
	protected.POST("/routes/:id/walk-to-closest", s.WalkToClosestHandler)
	protected.GET("/routes/:id/walks", s.GetWalksHandler)

	return r
}

// HelloWorldHandler godoc
//
//	@Summary	Hello World
//	@Tags		system
//	@Produce	json
//	@Success	200	{object}	map[string]string
//	@Router		/ [get]
func (s *Server) HelloWorldHandler(c *gin.Context) {
	resp := make(map[string]string)
	resp["message"] = "Hello World"

	c.JSON(http.StatusOK, resp)
}

// healthHandler godoc
//
//	@Summary	Database health check
//	@Tags		system
//	@Produce	json
//	@Success	200	{object}	map[string]string
//	@Router		/health [get]
func (s *Server) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, s.db.Health())
}
