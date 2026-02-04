// @title           CalDAV/CardDAV Server API
// @version         1.0
// @description     REST API for the CalDAV/CardDAV server. Provides calendar and contact management.
// @description
// @description     ## Authentication
// @description     Most endpoints require JWT Bearer token authentication.
// @description     Obtain a token via the `/api/v1/auth/login` endpoint.

// @host            localhost:8080
// @BasePath        /api/v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description JWT Bearer token. Format: "Bearer {token}"

// @securityDefinitions.basic BasicAuth
// @description HTTP Basic Authentication for DAV endpoints

// @tag.name Authentication
// @tag.description User authentication and session management
// @tag.name Users
// @tag.description User profile management
// @tag.name Calendars
// @tag.description Calendar management
// @tag.name Events
// @tag.description Calendar event management
// @tag.name Address Books
// @tag.description Address book management
// @tag.name Contacts
// @tag.description Contact management
// @tag.name Sharing
// @tag.description Calendar and address book sharing
// @tag.name Credentials
// @tag.description CalDAV/CardDAV access credentials
// @tag.name Import/Export
// @tag.description Data import and export operations

package main

import (
	"fmt"
	"os"

	_ "github.com/jherrma/caldav-server/docs" // swagger docs
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	"github.com/jherrma/caldav-server/internal/infrastructure/server"
)

func main() {
	// 1. Load configuration
	cfg, err := config.Load("")
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Printf("Configuration validaton failed: %v\n", err)
		os.Exit(1)
	}

	// 2. Initialize database
	db, err := database.New(cfg)
	if err != nil {
		fmt.Printf("Failed to initialize database: %v\n", err)
		os.Exit(1)
	}

	// 3. Handle CLI commands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			fmt.Println("Running migrations...")
			if err := db.Migrate(database.Models()...); err != nil {
				fmt.Printf("Migration failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Migrations completed successfully")
			return
		}
	}

	// 4. Auto-migration
	if cfg.Database.AutoMigrate {
		fmt.Println("Auto-migrating database...")
		if err := db.Migrate(database.Models()...); err != nil {
			fmt.Printf("Auto-migration failed: %v\n", err)
			os.Exit(1)
		}
	}

	// 5. Initialize and run server
	srv := server.New(cfg, db)
	if err := srv.Run(); err != nil {
		fmt.Printf("Server error: %v\n", err)
		os.Exit(1)
	}
}
