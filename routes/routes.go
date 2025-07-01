package routes

import (
	"github.com/desarso/whagonsRealtimeEngine/controllers"
	"github.com/gin-gonic/gin"
)

// EngineInterface combines all the interfaces needed by controllers
type EngineInterface interface {
	controllers.RealtimeEngineInterface
	controllers.HealthEngineInterface
}

// SetupRoutes configures all API routes
func SetupRoutes(router *gin.Engine, engine EngineInterface) {
	// Create controllers
	sessionController := controllers.NewSessionController(engine)
	healthController := controllers.NewHealthController(engine)

	// API v1 group
	v1 := router.Group("/api")
	{
		// Health endpoints
		health := v1.Group("/health")
		{
			health.GET("", healthController.GetHealth)
		}

		// Metrics endpoint
		v1.GET("/metrics", healthController.GetMetrics)

		// Session management endpoints
		sessions := v1.Group("/sessions")
		{
			sessions.GET("/count", sessionController.GetSessionsCount)
			sessions.POST("/disconnect-all", sessionController.DisconnectAllSessions)
		}

		// Broadcasting endpoint
		v1.POST("/broadcast", sessionController.BroadcastMessage)
	}

	// Add middleware for logging and CORS if needed
	setupMiddleware(router)
}

// setupMiddleware configures middleware for the router
func setupMiddleware(router *gin.Engine) {
	// CORS middleware
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Request logging middleware (Gin's default logger)
	router.Use(gin.Logger())

	// Recovery middleware
	router.Use(gin.Recovery())
}
