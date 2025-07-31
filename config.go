package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all configuration values
type Config struct {
	DBHost     string
	DBPort     string
	DBUsername string
	DBPassword string
	DBLandlord string
	ServerPort string
}

var config Config

func init() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("⚠️  Warning: Error loading .env file: %v", err)
		log.Println("🔍 Will use system environment variables instead")
	}

	// Initialize configuration
	config = Config{
		DBHost:     getEnv("DB_HOST", "127.0.0.1"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUsername: getEnv("DB_USERNAME", "postgres"),
		DBPassword: getEnv("DB_PASSWORD", ""),
		DBLandlord: getEnv("DB_LANDLORD", "landlord"),
		ServerPort: getEnv("SERVER_PORT", "8082"),
	}

	// Validate environment variables (warn but don't fail)
	if config.DBPassword == "" {
		log.Println("⚠️  Warning: DB_PASSWORD environment variable is not set")
		log.Println("🔍 Database connection may fail without proper credentials")
	}

	log.Println("✅ Environment configuration loaded")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
