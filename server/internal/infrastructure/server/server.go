package server

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
)

// Server represents the HTTP server
type Server struct {
	app *fiber.App
	cfg *config.Config
	db  database.Database
}

// New creates a new Server instance
func New(cfg *config.Config, db database.Database) *Server {
	app := fiber.New(fiber.Config{
		AppName:      "CalDAV Server",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
		BodyLimit:    10 * 1024 * 1024, // 10 MB
		RequestMethods: append(fiber.DefaultMethods,
			"PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK", "REPORT", "MKCALENDAR",
		),
	})

	SetupMiddleware(app, cfg)
	SetupRoutes(app, db, cfg)

	return &Server{
		app: app,
		cfg: cfg,
		db:  db,
	}
}

// Run starts the server and listens for shutdown signals
func (s *Server) Run() error {
	// Start server in a goroutine
	addr := fmt.Sprintf("%s:%s", s.cfg.Server.Host, s.cfg.Server.Port)
	go func() {
		fmt.Printf("Server starting on %s\n", addr)
		var err error
		if s.cfg.TLS.Enabled {
			fmt.Printf("TLS Enabled. Cert: %s, Key: %s\n", s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile)
			err = s.app.Listen(addr, fiber.ListenConfig{
				CertFile:    s.cfg.TLS.CertFile,
				CertKeyFile: s.cfg.TLS.KeyFile,
			})
		} else {
			err = s.app.Listen(addr)
		}

		if err != nil {
			fmt.Printf("Server failed to start: %v\n", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	<-quit // Wait for signal
	fmt.Println("\nShutting down server...")

	// Create a deadline to wait for
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.app.ShutdownWithContext(ctx); err != nil {
		fmt.Printf("Server forced to shutdown: %v\n", err)
	}

	// Close database connection
	if err := s.db.Close(); err != nil {
		fmt.Printf("Error closing database: %v\n", err)
	}

	fmt.Println("Server exited cleanly")
	return nil
}
