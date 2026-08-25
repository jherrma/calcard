package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	"github.com/jherrma/caldav-server/internal/infrastructure/server"
)

// resolveConfigPath determines the YAML config file path, honoring (in order):
// the --config flag, the documented CALDAV_CONFIG_PATH env var, then the
// CALDAV_CONFIG_FILE alias. Empty means env-and-defaults only (M18).
func resolveConfigPath(args []string) string {
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--config=") {
			return strings.TrimPrefix(args[i], "--config=")
		}
		if args[i] == "--config" && i+1 < len(args) {
			return args[i+1]
		}
	}
	if p := os.Getenv("CALDAV_CONFIG_PATH"); p != "" {
		return p
	}
	return os.Getenv("CALDAV_CONFIG_FILE")
}

func main() {
	// 1. Load configuration from the --config flag, CALDAV_CONFIG_PATH, or the
	// CALDAV_CONFIG_FILE alias; empty means env-and-defaults only (M18).
	cfg, err := config.Load(resolveConfigPath(os.Args[1:]))
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
			if err := database.RunDataMigrations(db.DB()); err != nil {
				fmt.Printf("Data migration failed: %v\n", err)
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
		if err := database.RunDataMigrations(db.DB()); err != nil {
			fmt.Printf("Data migration failed: %v\n", err)
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
