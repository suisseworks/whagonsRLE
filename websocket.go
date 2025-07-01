package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/igm/sockjs-go/v3/sockjs"
)

// sockjsHandler handles individual SockJS WebSocket connections with authentication
func (e *RealtimeEngine) sockjsHandler(session sockjs.Session) {
	// Log session details
	log.Printf("📡 SockJS session connected: %s", session.ID())
	log.Printf("🔍 SockJS Remote Address: %s", session.Request().RemoteAddr)

	// Extract bearer token and domain from query parameters or headers
	request := session.Request()
	token := extractBearerToken(
		request.Header.Get("Authorization"),
		request.URL.Query().Get("token"),
	)
	domain := request.URL.Query().Get("domain")

	if token == "" {
		log.Printf("❌ No bearer token provided for session %s", session.ID())
		e.sendAuthError(session, "Bearer token required")
		session.Close(4001, "Authentication required")
		return
	}

	if domain == "" {
		log.Printf("❌ No domain provided for session %s", session.ID())
		e.sendAuthError(session, "Domain parameter required")
		session.Close(4001, "Domain required")
		return
	}

	// Authenticate the token for the specific domain
	authSession, err := e.authenticateTokenForDomain(token, domain)
	if err != nil {
		log.Printf("❌ Authentication failed for session %s (domain: %s): %v", session.ID(), domain, err)
		e.sendAuthError(session, fmt.Sprintf("Authentication failed for domain %s", domain))
		session.Close(4001, "Authentication failed")
		return
	}

	// Set the session ID in the auth session
	authSession.SessionID = session.ID()

	// Add session to tracking
	e.mutex.Lock()
	e.sessions[session.ID()] = session
	e.authenticatedSessions[session.ID()] = authSession
	e.mutex.Unlock()

	log.Printf("✅ Authenticated session %s for domain: %s, tenant: %s, user: %d",
		session.ID(), domain, authSession.TenantName, authSession.UserID)

	// Send welcome message with tenant info
	welcomeMsg := SystemMessage{
		Type:      "system",
		Operation: "authenticated",
		Message:   fmt.Sprintf("Authenticated for domain: %s (tenant: %s)", domain, authSession.TenantName),
		Data: map[string]interface{}{
			"domain":      domain,
			"tenant_name": authSession.TenantName,
			"user_id":     authSession.UserID,
			"abilities":   authSession.Abilities,
		},
		Timestamp: time.Now().Format(time.RFC3339),
		SessionId: session.ID(),
	}
	if welcomeJSON, err := json.Marshal(welcomeMsg); err == nil {
		session.Send(string(welcomeJSON))
		log.Printf("📤 Sent welcome message to authenticated session %s", session.ID())
	}

	// Handle incoming messages
	for {
		if msg, err := session.Recv(); err == nil {
			log.Printf("📥 SockJS received: '%s' from authenticated session %s (tenant: %s)",
				msg, session.ID(), authSession.TenantName)

			// Echo the message back
			response := SystemMessage{
				Type:      "echo",
				Operation: "echo",
				Message:   fmt.Sprintf("Echo from %s: %s", authSession.TenantName, msg),
				Data:      msg,
				Timestamp: time.Now().Format(time.RFC3339),
				SessionId: session.ID(),
			}

			if responseJSON, err := json.Marshal(response); err == nil {
				if sendErr := session.Send(string(responseJSON)); sendErr != nil {
					log.Printf("❌ SockJS send error: %v", sendErr)
					break
				}
				log.Printf("📤 SockJS sent echo to authenticated session %s", session.ID())
			}
		} else {
			log.Printf("❌ SockJS receive error from session %s: %v", session.ID(), err)
			break
		}
	}

	// Remove session from tracking when disconnected
	e.mutex.Lock()
	delete(e.sessions, session.ID())
	delete(e.authenticatedSessions, session.ID())
	e.mutex.Unlock()

	log.Printf("📡 Authenticated session %s disconnected (tenant: %s)", session.ID(), authSession.TenantName)
}

// sendAuthError sends an authentication error message
func (e *RealtimeEngine) sendAuthError(session sockjs.Session, message string) {
	errorMsg := SystemMessage{
		Type:      "error",
		Operation: "auth_error",
		Message:   message,
		Timestamp: time.Now().Format(time.RFC3339),
		SessionId: session.ID(),
	}
	if errorJSON, err := json.Marshal(errorMsg); err == nil {
		session.Send(string(errorJSON))
	}
}

// broadcastSystemMessage sends a system message to all connected sessions
func (e *RealtimeEngine) BroadcastSystemMessage(message SystemMessage) {
	e.mutex.RLock()
	sessions := make(map[string]sockjs.Session)
	for id, session := range e.sessions {
		sessions[id] = session
	}
	e.mutex.RUnlock()

	broadcastCount := 0
	for sessionID, session := range sessions {
		// Set the sessionId for this specific session
		message.SessionId = sessionID

		jsonMessage, err := json.Marshal(message)
		if err != nil {
			log.Printf("❌ Failed to marshal system message: %v", err)
			continue
		}

		if err := session.Send(string(jsonMessage)); err != nil {
			log.Printf("❌ Failed to send to session %s: %v", sessionID, err)
			// Remove failed session
			e.mutex.Lock()
			delete(e.sessions, sessionID)
			e.mutex.Unlock()
		} else {
			broadcastCount++
		}
	}

	if broadcastCount > 0 {
		log.Printf("📡 Broadcasted system message to %d sessions", broadcastCount)
	}
}

// getConnectedSessionsCount returns the number of currently connected sessions
func (e *RealtimeEngine) GetConnectedSessionsCount() int {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return len(e.sessions)
}

// disconnectAllSessions gracefully disconnects all active sessions
func (e *RealtimeEngine) DisconnectAllSessions() {
	e.mutex.Lock()
	sessions := make(map[string]sockjs.Session)
	for id, session := range e.sessions {
		sessions[id] = session
	}
	e.mutex.Unlock()

	// Send disconnect notification
	disconnectMsg := SystemMessage{
		Type:      "system",
		Operation: "server_shutdown",
		Message:   "Server is shutting down",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	for sessionID, session := range sessions {
		disconnectMsg.SessionId = sessionID
		if msgJSON, err := json.Marshal(disconnectMsg); err == nil {
			session.Send(string(msgJSON))
		}
		session.Close(1000, "Server shutdown")
		log.Printf("📡 Disconnected session: %s", sessionID)
	}

	// Clear all sessions
	e.mutex.Lock()
	e.sessions = make(map[string]sockjs.Session)
	e.mutex.Unlock()

	log.Printf("📡 All sessions disconnected")
}

// getTenantDatabasesCount returns the number of connected tenant databases
func (e *RealtimeEngine) GetTenantDatabasesCount() int {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return len(e.tenantDBs)
}

// IsLandlordConnected checks if the landlord database is connected
func (e *RealtimeEngine) IsLandlordConnected() bool {
	return e.landlordDB != nil
}

// BroadcastMessage is a simplified interface for controllers to broadcast messages
func (e *RealtimeEngine) BroadcastMessage(msgType, operation, message string, data interface{}) {
	systemMessage := SystemMessage{
		Type:      msgType,
		Operation: operation,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().Format(time.RFC3339),
		// SessionId will be set per session in BroadcastSystemMessage
	}

	e.BroadcastSystemMessage(systemMessage)
}
