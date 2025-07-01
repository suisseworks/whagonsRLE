package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// authenticateToken validates a Laravel Sanctum bearer token for a specific tenant domain
func (e *RealtimeEngine) authenticateTokenForDomain(bearerToken, domain string) (*AuthenticatedSession, error) {
	// First, look up the tenant information from the landlord database
	tenantInfo, err := e.getTenantByDomain(domain)
	if err != nil {
		return nil, fmt.Errorf("tenant not found for domain %s: %w", domain, err)
	}

	log.Printf("🔍 Found tenant '%s' with database '%s' for domain: %s", tenantInfo.Name, tenantInfo.Database, domain)

	// Get the tenant database connection
	e.mutex.RLock()
	tenantDB, exists := e.tenantDBs[tenantInfo.Name]
	e.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("database connection not found for tenant: %s", tenantInfo.Name)
	}

	// Parse Laravel Sanctum token format: {token_id}|{plain_text_token}
	tokenParts := strings.Split(bearerToken, "|")
	if len(tokenParts) != 2 {
		return nil, fmt.Errorf("invalid token format")
	}

	tokenID, err := strconv.Atoi(tokenParts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid token ID: %w", err)
	}

	plainTextToken := tokenParts[1]

	// Hash the plain text token (Laravel uses SHA-256)
	hasher := sha256.New()
	hasher.Write([]byte(plainTextToken))
	hashedToken := hex.EncodeToString(hasher.Sum(nil))

	log.Printf("🔍 Authenticating token ID %d for tenant %s with hash: %s", tokenID, tenantInfo.Name, hashedToken[:16]+"...")

	// Validate the token in the specific tenant database
	authSession, err := e.validateTokenInTenant(tenantInfo.Name, tenantDB, tokenID, hashedToken)
	if err != nil {
		return nil, fmt.Errorf("authentication failed for tenant %s: %w", tenantInfo.Name, err)
	}

	log.Printf("✅ Token authenticated for domain %s, tenant: %s, user: %d", domain, tenantInfo.Name, authSession.UserID)
	return authSession, nil
}

// getTenantByDomain looks up tenant information by domain in the landlord database
func (e *RealtimeEngine) getTenantByDomain(domain string) (*TenantDB, error) {
	query := "SELECT name, domain, database FROM tenants WHERE domain = $1 AND database IS NOT NULL"

	var tenant TenantDB
	err := e.landlordDB.QueryRow(query, domain).Scan(&tenant.Name, &tenant.Domain, &tenant.Database)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no tenant found for domain: %s", domain)
		}
		return nil, fmt.Errorf("database error looking up tenant: %w", err)
	}

	return &tenant, nil
}

// Legacy function for backwards compatibility - now deprecated
func (e *RealtimeEngine) authenticateToken(bearerToken string) (*AuthenticatedSession, error) {
	return nil, fmt.Errorf("authenticateToken is deprecated - use authenticateTokenForDomain instead")
}

// validateTokenInTenant checks if a token exists and is valid in a specific tenant database
func (e *RealtimeEngine) validateTokenInTenant(tenantName string, db *sql.DB, tokenID int, hashedToken string) (*AuthenticatedSession, error) {
	query := `
		SELECT id, tokenable_type, tokenable_id, name, token, abilities, 
		       last_used_at, expires_at, created_at, updated_at
		FROM personal_access_tokens 
		WHERE id = $1 AND token = $2
	`

	var token PersonalAccessToken
	var lastUsedAt, expiresAt sql.NullTime

	err := db.QueryRow(query, tokenID, hashedToken).Scan(
		&token.ID,
		&token.TokenableType,
		&token.TokenableID,
		&token.Name,
		&token.Token,
		&token.Abilities,
		&lastUsedAt,
		&expiresAt,
		&token.CreatedAt,
		&token.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("token not found in tenant %s", tenantName)
		}
		return nil, fmt.Errorf("database error in tenant %s: %w", tenantName, err)
	}

	// Convert nullable times
	if lastUsedAt.Valid {
		token.LastUsedAt = &lastUsedAt.Time
	}
	if expiresAt.Valid {
		token.ExpiresAt = &expiresAt.Time
	}

	// Check if token is expired
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("token expired")
	}

	// Update last_used_at timestamp
	_, err = db.Exec("UPDATE personal_access_tokens SET last_used_at = $1 WHERE id = $2", time.Now(), tokenID)
	if err != nil {
		log.Printf("⚠️ Failed to update last_used_at for token %d: %v", tokenID, err)
	}

	// Parse abilities (Laravel stores as JSON array)
	abilities := []string{}
	if token.Abilities != "" {
		// Simple parsing for ["*"] or ["ability1", "ability2"]
		cleanAbilities := strings.Trim(token.Abilities, `[]"`)
		if cleanAbilities != "" {
			abilities = strings.Split(strings.ReplaceAll(cleanAbilities, `"`, ""), ",")
		}
	}

	return &AuthenticatedSession{
		TenantName: tenantName,
		UserID:     token.TokenableID,
		TokenID:    token.ID,
		Abilities:  abilities,
		ExpiresAt:  token.ExpiresAt,
		LastUsedAt: time.Now(),
	}, nil
}

// hasAbility checks if the authenticated session has a specific ability
func (auth *AuthenticatedSession) hasAbility(ability string) bool {
	for _, a := range auth.Abilities {
		if a == "*" || a == ability {
			return true
		}
	}
	return false
}

// canAccessTenant checks if the session can access a specific tenant's data
func (auth *AuthenticatedSession) canAccessTenant(tenantName string) bool {
	// User can only access their own tenant
	return auth.TenantName == tenantName
}

// extractBearerToken extracts the bearer token from various sources
func extractBearerToken(authHeader, queryParam string) string {
	// Try Authorization header first
	if authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	// Try query parameter as fallback (for WebSocket connections)
	if queryParam != "" {
		return queryParam
	}

	return ""
}
