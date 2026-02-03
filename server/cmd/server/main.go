package main

import (
	"fmt"
	"os"

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
