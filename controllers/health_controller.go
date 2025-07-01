package controllers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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
func (hc *HealthController) GetHealth(c *gin.Context) {
	sessionCount := hc.engine.GetConnectedSessionsCount()
	tenantCount := hc.engine.GetTenantDatabasesCount()
	landlordConnected := hc.engine.IsLandlordConnected()

	status := "healthy"
	if !landlordConnected {
		status = "degraded"
	}

	response := gin.H{
		"status":  status,
		"service": "Whagons Realtime Engine",
		"version": "1.0.0",
		"data": gin.H{
			"connected_sessions": sessionCount,
			"tenant_databases":   tenantCount,
			"landlord_connected": landlordConnected,
			"uptime":             time.Now().Format(time.RFC3339),
		},
	}

	if status == "healthy" {
		c.JSON(http.StatusOK, response)
	} else {
		c.JSON(http.StatusServiceUnavailable, response)
	}
}

// GetMetrics provides detailed metrics endpoint
// @Summary Get system metrics
// @Description Returns detailed system metrics and statistics
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/metrics [get]
func (hc *HealthController) GetMetrics(c *gin.Context) {
	sessionCount := hc.engine.GetConnectedSessionsCount()
	tenantCount := hc.engine.GetTenantDatabasesCount()
	landlordConnected := hc.engine.IsLandlordConnected()

	response := gin.H{
		"status": "success",
		"metrics": gin.H{
			"sessions": gin.H{
				"connected_count": sessionCount,
			},
			"databases": gin.H{
				"tenant_count":       tenantCount,
				"landlord_connected": landlordConnected,
			},
			"system": gin.H{
				"uptime":  time.Now().Format(time.RFC3339),
				"service": "Whagons Realtime Engine",
				"version": "1.0.0",
			},
		},
	}

	c.JSON(http.StatusOK, response)
}
