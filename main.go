package main

import (
	"database/sql"
	"log"

	"github.com/desarso/whagonsRealtimeEngine/routes"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
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

	// Create Fiber app
	app := fiber.New(fiber.Config{
		ServerHeader: "Whagons Realtime Engine",
		AppName:      "Whagons Realtime Engine v1.0.0",
	})

	// Setup API routes with controllers
	routes.SetupRoutes(app, engine)

	// SockJS handler - mount at /ws to match client expectations
	sockjsHandler := sockjs.NewHandler("/ws", sockjs.DefaultOptions, engine.sockjsHandler)

	// Mount SockJS on Fiber app using adaptor
	app.All("/ws/*", adaptor.HTTPHandler(sockjsHandler))

	// Server startup messages
	log.Printf("🚀 Whagons Realtime Engine starting...")
	log.Printf("📡 Server listening on port: %s", config.ServerPort)
	log.Printf("🔌 SockJS WebSocket endpoint: http://localhost:%s/ws", config.ServerPort)
	log.Printf("📊 API endpoints available:")
	log.Printf("   GET  /api/health - Health check")
	log.Printf("   GET  /api/metrics - System metrics")
	log.Printf("   GET  /api/sessions/count - Get connected sessions count")
	log.Printf("   POST /api/sessions/disconnect-all - Disconnect all sessions")
	log.Printf("   POST /api/broadcast - Broadcast message to all sessions")

	// Start HTTP server with Fiber
	log.Fatal(app.Listen(":" + config.ServerPort))
}
