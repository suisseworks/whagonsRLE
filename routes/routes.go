package routes

import (
	"github.com/desarso/whagonsRealtimeEngine/controllers"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// EngineInterface combines all the interfaces needed by controllers
type EngineInterface interface {
	controllers.RealtimeEngineInterface
	controllers.HealthEngineInterface
}

// SetupRoutes configures all API routes
func SetupRoutes(app *fiber.App, engine EngineInterface) {
	// Create controllers
	sessionController := controllers.NewSessionController(engine)
	healthController := controllers.NewHealthController(engine)

	// Add middleware for logging, CORS, and recovery
	setupMiddleware(app)

	// API v1 group
	api := app.Group("/api")

	// Health endpoints
	health := api.Group("/health")
	health.Get("/", healthController.GetHealth)

	// Metrics endpoint
	api.Get("/metrics", healthController.GetMetrics)

	// Session management endpoints
	sessions := api.Group("/sessions")
	sessions.Get("/count", sessionController.GetSessionsCount)
	sessions.Post("/disconnect-all", sessionController.DisconnectAllSessions)

	// Broadcasting endpoint
	api.Post("/broadcast", sessionController.BroadcastMessage)
}

// setupMiddleware configures middleware for the Fiber app
func setupMiddleware(app *fiber.App) {
	// CORS middleware
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders: "Content-Type,Authorization",
	}))

	// Request logging middleware
	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${status} - ${latency} ${method} ${path}\n",
	}))

	// Recovery middleware
	app.Use(recover.New())
}
