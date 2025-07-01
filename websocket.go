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
	// Log session details with current session count
	e.mutex.RLock()
	currentSessionCount := len(e.sessions)
	e.mutex.RUnlock()

	log.Printf("📡 SockJS session handler called: %s (current sessions: %d)", session.ID(), currentSessionCount)
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

	// DON'T add to session tracking yet - wait until we receive the first real message
	// This prevents counting SockJS negotiation sessions that will be discarded
	log.Printf("✅ Authenticated negotiation session %s for domain: %s, tenant: %s, user: %d (not yet active)",
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
		if sendErr := session.Send(string(welcomeJSON)); sendErr != nil {
			log.Printf("💀 Negotiation session %s failed to send welcome - connection dead", session.ID())
			return
		}
		log.Printf("📤 Sent welcome message to negotiation session %s", session.ID())
	}

	// Add this session to negotiation tracking - don't count toward active sessions yet
	e.mutex.Lock()
	e.negotiationSessions[session.ID()] = session
	e.authenticatedSessions[session.ID()] = authSession
	activeSessionCount := len(e.sessions)
	negotiationCount := len(e.negotiationSessions)
	e.mutex.Unlock()

	log.Printf("🎯 Session %s added to NEGOTIATION (active: %d, negotiating: %d) - waiting for real communication",
		session.ID(), activeSessionCount, negotiationCount)

	// Set a timeout to close unused negotiation sessions
	negotiationTimeout := time.NewTimer(15 * time.Second)
	sessionClosed := make(chan bool, 1)

	// Goroutine to handle negotiation timeout
	go func() {
		select {
		case <-negotiationTimeout.C:
			// Timeout reached - close this negotiation session if it's still unused
			e.mutex.Lock()
			if _, stillNegotiating := e.negotiationSessions[session.ID()]; stillNegotiating {
				delete(e.negotiationSessions, session.ID())
				delete(e.authenticatedSessions, session.ID())
				e.mutex.Unlock()

				log.Printf("⏰ Negotiation timeout - closing unused session %s", session.ID())
				session.Close(4001, "Negotiation timeout - session unused")
				sessionClosed <- true
			} else {
				e.mutex.Unlock()
			}
		case <-sessionClosed:
			// Session was promoted or closed elsewhere, stop timeout
			negotiationTimeout.Stop()
		}
	}()

	// Handle incoming messages - promote to active session on first real message
	for {
		if msg, err := session.Recv(); err == nil {
			// This is a real message - promote session to active
			e.mutex.Lock()
			// Move from negotiation to active sessions
			if _, exists := e.negotiationSessions[session.ID()]; exists {
				delete(e.negotiationSessions, session.ID())
				e.sessions[session.ID()] = session
				activeCount := len(e.sessions)
				negotiationCount := len(e.negotiationSessions)
				e.mutex.Unlock()

				log.Printf("🔥 Session %s PROMOTED to ACTIVE (active: %d, negotiating: %d) - received first message",
					session.ID(), activeCount, negotiationCount)
			} else {
				e.mutex.Unlock()
			}

			log.Printf("📥 SockJS received: '%s' from active session %s (tenant: %s)",
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
				log.Printf("📤 SockJS sent echo to active session %s", session.ID())
			}
		} else {
			log.Printf("❌ SockJS receive error from session %s: %v", session.ID(), err)
			break
		}
	}

	// Clean up session when disconnected
	e.cleanupSession(session.ID(), authSession.TenantName)
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
	// Only broadcast to ACTIVE sessions, not negotiation sessions
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
			log.Printf("❌ Failed to send to active session %s: %v", sessionID, err)
			// Remove failed session
			e.mutex.Lock()
			delete(e.sessions, sessionID)
			delete(e.authenticatedSessions, sessionID)
			e.mutex.Unlock()
		} else {
			broadcastCount++
		}
	}

	if broadcastCount > 0 {
		log.Printf("📡 Broadcasted system message to %d ACTIVE sessions", broadcastCount)
	}
}

// getConnectedSessionsCount returns the number of currently connected sessions
func (e *RealtimeEngine) GetConnectedSessionsCount() int {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return len(e.sessions)
}

// getNegotiationSessionsCount returns the number of sessions currently in negotiation
func (e *RealtimeEngine) GetNegotiationSessionsCount() int {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return len(e.negotiationSessions)
}

// getTotalSessionsCount returns the total number of sessions (active + negotiation)
func (e *RealtimeEngine) GetTotalSessionsCount() int {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return len(e.sessions) + len(e.negotiationSessions)
}

// disconnectAllSessions gracefully disconnects all active sessions
func (e *RealtimeEngine) DisconnectAllSessions() {
	e.mutex.Lock()
	activeSessions := make(map[string]sockjs.Session)
	negotiationSessions := make(map[string]sockjs.Session)

	// Copy both active and negotiation sessions
	for id, session := range e.sessions {
		activeSessions[id] = session
	}
	for id, session := range e.negotiationSessions {
		negotiationSessions[id] = session
	}
	e.mutex.Unlock()

	// Send disconnect notification
	disconnectMsg := SystemMessage{
		Type:      "system",
		Operation: "server_shutdown",
		Message:   "Server is shutting down",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Disconnect active sessions
	for sessionID, session := range activeSessions {
		disconnectMsg.SessionId = sessionID
		if msgJSON, err := json.Marshal(disconnectMsg); err == nil {
			session.Send(string(msgJSON))
		}
		session.Close(1000, "Server shutdown")
		log.Printf("📡 Disconnected ACTIVE session: %s", sessionID)
	}

	// Disconnect negotiation sessions
	for sessionID, session := range negotiationSessions {
		session.Close(1000, "Server shutdown")
		log.Printf("📡 Disconnected NEGOTIATION session: %s", sessionID)
	}

	// Clear all sessions
	e.mutex.Lock()
	e.sessions = make(map[string]sockjs.Session)
	e.negotiationSessions = make(map[string]sockjs.Session)
	e.authenticatedSessions = make(map[string]*AuthenticatedSession)
	e.mutex.Unlock()

	totalDisconnected := len(activeSessions) + len(negotiationSessions)
	log.Printf("📡 All sessions disconnected - %d active, %d negotiation, %d total",
		len(activeSessions), len(negotiationSessions), totalDisconnected)
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

// GetCacheStats returns statistics about the token cache
func (e *RealtimeEngine) GetCacheStats() map[string]int {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	totalCached := len(e.tokenCache)
	expiredCount := 0
	now := time.Now()

	for _, cachedToken := range e.tokenCache {
		if now.After(cachedToken.ExpiresAt) {
			expiredCount++
		}
	}

	return map[string]int{
		"total_cached_tokens": totalCached,
		"expired_tokens":      expiredCount,
		"active_tokens":       totalCached - expiredCount,
	}
}

// cleanupSession removes a session from all tracking maps
func (e *RealtimeEngine) cleanupSession(sessionID, tenantName string) {
	e.mutex.Lock()
	// Remove from both active and negotiation sessions
	delete(e.sessions, sessionID)
	delete(e.negotiationSessions, sessionID)
	delete(e.authenticatedSessions, sessionID)
	remainingActive := len(e.sessions)
	remainingNegotiation := len(e.negotiationSessions)
	e.mutex.Unlock()

	log.Printf("📡 Session %s disconnected (tenant: %s) - active: %d, negotiating: %d remaining",
		sessionID, tenantName, remainingActive, remainingNegotiation)
}

// cleanupZombieSessions removes sessions that are no longer active (for failed transport attempts)
func (e *RealtimeEngine) cleanupZombieSessions() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	var zombieActiveSessions []string
	var zombieNegotiationSessions []string

	// Create a proper JSON ping message
	pingMsg := SystemMessage{
		Type:      "ping",
		Operation: "health_check",
		Message:   "Connection health check",
		Timestamp: time.Now().Format(time.RFC3339),
	}
	pingJSON, _ := json.Marshal(pingMsg)

	// Check active sessions
	for sessionID, session := range e.sessions {
		// Try to send a proper JSON ping to check if session is still alive
		if err := session.Send(string(pingJSON)); err != nil {
			log.Printf("🧟 Found zombie ACTIVE session: %s (error: %v)", sessionID, err)
			zombieActiveSessions = append(zombieActiveSessions, sessionID)
		}
	}

	// Check negotiation sessions and clean up old ones
	for sessionID, session := range e.negotiationSessions {
		// Try to send a proper JSON ping to check if session is still alive
		if err := session.Send(string(pingJSON)); err != nil {
			log.Printf("🧟 Found zombie NEGOTIATION session: %s (error: %v)", sessionID, err)
			zombieNegotiationSessions = append(zombieNegotiationSessions, sessionID)
		}
	}

	// Clean up zombie active sessions
	for _, sessionID := range zombieActiveSessions {
		delete(e.sessions, sessionID)
		delete(e.authenticatedSessions, sessionID)
		log.Printf("🧹 Cleaned up zombie ACTIVE session: %s", sessionID)
	}

	// Clean up zombie negotiation sessions
	for _, sessionID := range zombieNegotiationSessions {
		delete(e.negotiationSessions, sessionID)
		delete(e.authenticatedSessions, sessionID)
		log.Printf("🧹 Cleaned up zombie NEGOTIATION session: %s", sessionID)
	}

	totalCleaned := len(zombieActiveSessions) + len(zombieNegotiationSessions)
	if totalCleaned > 0 {
		log.Printf("🧹 Cleaned up %d zombie sessions - active: %d, negotiating: %d remaining",
			totalCleaned, len(e.sessions), len(e.negotiationSessions))
	}
}
