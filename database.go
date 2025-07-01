package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

// connectToLandlord establishes connection to the landlord database
func (e *RealtimeEngine) connectToLandlord() error {
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		config.DBHost, config.DBPort, config.DBUsername, config.DBPassword, config.DBLandlord)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open landlord database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping landlord database: %w", err)
	}

	e.landlordDB = db
	log.Println("✅ Connected to landlord database")
	return nil
}

// loadTenantDatabases queries the landlord database for tenant information and connects to each tenant database
func (e *RealtimeEngine) loadTenantDatabases() error {
	query := "SELECT id, name, domain, database FROM tenants WHERE database IS NOT NULL"
	rows, err := e.landlordDB.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query tenants: %w", err)
	}
	defer rows.Close()

	var tenants []TenantDB
	for rows.Next() {
		var tenant TenantDB
		if err := rows.Scan(&tenant.ID, &tenant.Name, &tenant.Domain, &tenant.Database); err != nil {
			log.Printf("⚠️  Error scanning tenant row: %v", err)
			continue
		}
		tenants = append(tenants, tenant)
	}

	log.Printf("📊 Found %d tenant databases", len(tenants))

	// Connect to each tenant database
	for _, tenant := range tenants {
		if err := e.connectToTenant(tenant); err != nil {
			log.Printf("⚠️  Failed to connect to tenant %s: %v", tenant.Name, err)
			continue
		}
		log.Printf("✅ Connected to tenant database: %s", tenant.Name)
	}

	return nil
}

// connectToTenant establishes connection to a specific tenant database
func (e *RealtimeEngine) connectToTenant(tenant TenantDB) error {
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		config.DBHost, config.DBPort, config.DBUsername, config.DBPassword, tenant.Database)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open tenant database %s: %w", tenant.Database, err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping tenant database %s: %w", tenant.Database, err)
	}

	e.mutex.Lock()
	e.tenantDBs[tenant.Name] = db
	e.mutex.Unlock()

	return nil
}

// closeDatabases closes all database connections gracefully
func (e *RealtimeEngine) closeDatabases() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// Close tenant databases
	for name, db := range e.tenantDBs {
		if err := db.Close(); err != nil {
			log.Printf("⚠️  Error closing tenant database %s: %v", name, err)
		} else {
			log.Printf("✅ Closed tenant database: %s", name)
		}
	}

	// Close landlord database
	if e.landlordDB != nil {
		if err := e.landlordDB.Close(); err != nil {
			log.Printf("⚠️  Error closing landlord database: %v", err)
		} else {
			log.Println("✅ Closed landlord database")
		}
	}
}
