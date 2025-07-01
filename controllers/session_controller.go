package controllers

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// SessionController handles session-related endpoints
type SessionController struct {
	engine RealtimeEngineInterface
}

// RealtimeEngineInterface defines the methods we need from RealtimeEngine
type RealtimeEngineInterface interface {
	GetConnectedSessionsCount() int
	DisconnectAllSessions()
	BroadcastMessage(msgType, operation, message string, data interface{})
}

// SystemMessage represents system messages for JSON responses
type SystemMessage struct {
	Type      string      `json:"type"`
	Operation string      `json:"operation"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp string      `json:"timestamp"`
	SessionId string      `json:"sessionId"`
}

// NewSessionController creates a new session controller
func NewSessionController(engine RealtimeEngineInterface) *SessionController {
	return &SessionController{
		engine: engine,
	}
}

// GetSessionsCount returns the number of connected sessions
// @Summary Get connected sessions count
// @Description Returns the current number of active WebSocket sessions
// @Tags sessions
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/sessions/count [get]
func (sc *SessionController) GetSessionsCount(c *fiber.Ctx) error {
	count := sc.engine.GetConnectedSessionsCount()

	response := fiber.Map{
		"status": "success",
		"data": fiber.Map{
			"connected_sessions": count,
			"timestamp":          time.Now().Format(time.RFC3339),
		},
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

// DisconnectAllSessions disconnects all active sessions
// @Summary Disconnect all sessions
// @Description Gracefully disconnects all active WebSocket sessions
// @Tags sessions
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/sessions/disconnect-all [post]
func (sc *SessionController) DisconnectAllSessions(c *fiber.Ctx) error {
	// Get count before disconnecting
	countBefore := sc.engine.GetConnectedSessionsCount()

	// Disconnect all sessions
	sc.engine.DisconnectAllSessions()

	response := fiber.Map{
		"status":  "success",
		"message": "All sessions disconnected",
		"data": fiber.Map{
			"sessions_disconnected": countBefore,
			"timestamp":             time.Now().Format(time.RFC3339),
		},
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

// BroadcastMessage sends a message to all connected sessions
// @Summary Broadcast message to all sessions
// @Description Sends a message to all currently connected WebSocket sessions
// @Tags sessions
// @Accept json
// @Produce json
// @Param message body BroadcastRequest true "Broadcast message request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Router /api/broadcast [post]
func (sc *SessionController) BroadcastMessage(c *fiber.Ctx) error {
	var requestBody BroadcastRequest

	if err := c.BodyParser(&requestBody); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  "error",
			"message": "Invalid JSON request body",
			"error":   err.Error(),
		})
	}

	if requestBody.Message == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  "error",
			"message": "Message field is required",
		})
	}

	// Set default values
	if requestBody.Type == "" {
		requestBody.Type = "system"
	}
	if requestBody.Operation == "" {
		requestBody.Operation = "broadcast"
	}

	// Get session count before broadcasting
	sessionCount := sc.engine.GetConnectedSessionsCount()

	// Broadcast the message using the simplified interface
	sc.engine.BroadcastMessage(requestBody.Type, requestBody.Operation, requestBody.Message, requestBody.Data)

	// Create response message for JSON response
	systemMessage := SystemMessage{
		Type:      requestBody.Type,
		Operation: requestBody.Operation,
		Message:   requestBody.Message,
		Data:      requestBody.Data,
		Timestamp: time.Now().Format(time.RFC3339),
		SessionId: "", // Will be set per session by the engine
	}

	response := fiber.Map{
		"status":  "success",
		"message": "Message broadcasted successfully",
		"data": fiber.Map{
			"sessions_reached":  sessionCount,
			"broadcast_message": systemMessage,
			"timestamp":         time.Now().Format(time.RFC3339),
		},
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

// BroadcastRequest represents the request body for broadcasting messages
type BroadcastRequest struct {
	Type      string      `json:"type" example:"system"`
	Operation string      `json:"operation" example:"broadcast"`
	Message   string      `json:"message" binding:"required" example:"Hello all connected clients!"`
	Data      interface{} `json:"data,omitempty"`
}
