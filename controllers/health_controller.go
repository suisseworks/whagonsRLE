package controllers

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// HealthController handles health-related endpoints
type HealthController struct {
	engine HealthEngineInterface
}

// HealthEngineInterface defines the methods we need from RealtimeEngine for health checks
type HealthEngineInterface interface {
	GetConnectedSessionsCount() int
	GetTenantDatabasesCount() int
	IsLandlordConnected() bool
}

// NewHealthController creates a new health controller
func NewHealthController(engine HealthEngineInterface) *HealthController {
	return &HealthController{
		engine: engine,
	}
}

// GetHealth provides a health check endpoint
// @Summary Health check
// @Description Returns the health status of the Whagons Realtime Engine
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/health [get]
func (hc *HealthController) GetHealth(c *fiber.Ctx) error {
	sessionCount := hc.engine.GetConnectedSessionsCount()
	tenantCount := hc.engine.GetTenantDatabasesCount()
	landlordConnected := hc.engine.IsLandlordConnected()

	status := "healthy"
	httpStatus := fiber.StatusOK
	if !landlordConnected {
		status = "degraded"
		httpStatus = fiber.StatusServiceUnavailable
	}

	response := fiber.Map{
		"status":  status,
		"service": "Whagons Realtime Engine",
		"version": "1.0.0",
		"data": fiber.Map{
			"connected_sessions": sessionCount,
			"tenant_databases":   tenantCount,
			"landlord_connected": landlordConnected,
			"uptime":             time.Now().Format(time.RFC3339),
		},
	}

	return c.Status(httpStatus).JSON(response)
}

// GetMetrics provides detailed metrics endpoint
// @Summary Get system metrics
// @Description Returns detailed system metrics and statistics
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/metrics [get]
func (hc *HealthController) GetMetrics(c *fiber.Ctx) error {
	sessionCount := hc.engine.GetConnectedSessionsCount()
	tenantCount := hc.engine.GetTenantDatabasesCount()
	landlordConnected := hc.engine.IsLandlordConnected()

	response := fiber.Map{
		"status": "success",
		"metrics": fiber.Map{
			"sessions": fiber.Map{
				"connected_count": sessionCount,
			},
			"databases": fiber.Map{
				"tenant_count":       tenantCount,
				"landlord_connected": landlordConnected,
			},
			"system": fiber.Map{
				"uptime":  time.Now().Format(time.RFC3339),
				"service": "Whagons Realtime Engine",
				"version": "1.0.0",
			},
		},
	}

	return c.Status(fiber.StatusOK).JSON(response)
}
