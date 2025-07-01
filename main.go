package main

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/igm/sockjs-go/v3/sockjs"
	_ "github.com/lib/pq"
)

func main() {
	engine := &RealtimeEngine{
		tenantDBs:             make(map[string]*sql.DB),
		sessions:              make(map[string]sockjs.Session),
		authenticatedSessions: make(map[string]*AuthenticatedSession),
	}

	// Connect to landlord database
	if err := engine.connectToLandlord(); err != nil {
		log.Fatalf("❌ Failed to connect to landlord database: %v", err)
	}
	defer engine.landlordDB.Close()

	// Load tenant databases
	if err := engine.loadTenantDatabases(); err != nil {
		log.Fatalf("❌ Failed to load tenant databases: %v", err)
	}

	// Start listening to publications from tenant databases
	go engine.startPublicationListeners()

	// SockJS handler - mount at /ws to match client expectations
	sockjsHandler := sockjs.NewHandler("/ws", sockjs.DefaultOptions, engine.sockjsHandler)
	http.Handle("/ws/", sockjsHandler)

	// Server startup messages
	log.Printf("🚀 Whagons Realtime Engine starting...")
	log.Printf("📡 Server listening on port: %s", config.ServerPort)
	log.Printf("🔌 SockJS WebSocket endpoint: http://localhost:%s/ws", config.ServerPort)
	log.Printf("🗄️  Database host: %s:%s", config.DBHost, config.DBPort)
	log.Printf("🏢 Landlord database: %s", config.DBLandlord)
	log.Println("✅ PostgreSQL publication listening enabled")
	log.Printf("🌐 Test interface: http://localhost:%s (serve test.html)", config.ServerPort)

	log.Fatal(http.ListenAndServe(":"+config.ServerPort, nil))
}
