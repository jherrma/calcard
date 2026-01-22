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

	// 2. Initialize database
	db, err := database.New(cfg)
	if err != nil {
		fmt.Printf("Failed to initialize database: %v\n", err)
		os.Exit(1)
	}

	// 3. Initialize and run server
	srv := server.New(cfg, db)
	if err := srv.Run(); err != nil {
		fmt.Printf("Server error: %v\n", err)
		os.Exit(1)
	}
}
