package server

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"}, // Add your frontend URL
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true, // Enable cookies/auth
	}))

	r.GET("/", s.HelloWorldHandler)
	r.GET("/health", s.healthHandler)

	r.StaticFile("/app", "./web/index.html")
	r.StaticFile("/docs", "./web/swagger.html")
	r.GET("/docs/swagger.json", s.SwaggerJSONHandler)
	r.GET("/config", s.ConfigHandler)

	r.POST("/routes", s.CreateRouteHandler)
	r.GET("/routes/:id", s.GetRouteHandler)
	r.GET("/routes/:id/raw", s.GetRawRouteHandler)
	r.POST("/routes/:id/closest-point", s.FindClosestPointHandler)
	r.GET("/routes/:id/closest-points", s.GetClosestPointsHandler)
	r.POST("/routes/:id/walk-to-closest", s.WalkToClosestHandler)
	r.GET("/routes/:id/walks", s.GetWalksHandler)

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
