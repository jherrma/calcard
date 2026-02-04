package http

import (
	"os"
	"path/filepath"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/docs"
)

// DocsHandler handles API documentation routes
type DocsHandler struct {
	docsPath string
}

// NewDocsHandler creates a new docs handler
func NewDocsHandler(docsPath string) *DocsHandler {
	return &DocsHandler{docsPath: docsPath}
}

// ServeOpenAPIJSON serves the OpenAPI spec as JSON
func (h *DocsHandler) ServeOpenAPIJSON(c fiber.Ctx) error {
	// Use embedded swagger.json
	c.Set("Content-Type", "application/json")
	return c.Send(docs.SwaggerJSON)
}

// ServeOpenAPIYAML serves the OpenAPI spec as YAML
func (h *DocsHandler) ServeOpenAPIYAML(c fiber.Ctx) error {
	// Use embedded swagger.yaml
	c.Set("Content-Type", "application/x-yaml")
	return c.Send(docs.SwaggerYAML)
}

// ServeSwaggerUI serves the Swagger UI
func (h *DocsHandler) ServeSwaggerUI(c fiber.Ctx) error {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>CalDAV/CardDAV Server API</title>
    <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
    <style>
        body { margin: 0; padding: 0; }
        .swagger-ui .topbar { display: none; }
        .swagger-ui .info .title { font-size: 2rem; }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
        window.onload = function() {
            SwaggerUIBundle({
                url: "/api/v1/swagger.json",
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIBundle.SwaggerUIStandalonePreset
                ],
                layout: "BaseLayout",
                docExpansion: "list",
                filter: true,
                tryItOutEnabled: true
            });
        };
    </script>
</body>
</html>`

	c.Set("Content-Type", "text/html")
	return c.SendString(html)
}

// ServeDAVDocs serves the DAV documentation
func (h *DocsHandler) ServeDAVDocs(c fiber.Ctx) error {
	davPath := filepath.Join(h.docsPath, "dav")

	// Serve index.html for root path
	path := c.Params("*")
	if path == "" || path == "/" {
		path = "index.html"
	}

	filePath := filepath.Join(davPath, path)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("Documentation not found")
	}

	// Set content type based on extension
	ext := filepath.Ext(path)
	switch ext {
	case ".html":
		c.Set("Content-Type", "text/html")
	case ".css":
		c.Set("Content-Type", "text/css")
	case ".js":
		c.Set("Content-Type", "application/javascript")
	case ".md":
		c.Set("Content-Type", "text/markdown")
	default:
		c.Set("Content-Type", "text/plain")
	}

	return c.Send(data)
}

// SetupDocsRoutes registers all documentation routes
func SetupDocsRoutes(app *fiber.App, docsPath string) {
	handler := NewDocsHandler(docsPath)

	// OpenAPI spec endpoints
	app.Get("/api/v1/swagger.json", handler.ServeOpenAPIJSON)
	app.Get("/api/v1/swagger.yaml", handler.ServeOpenAPIYAML)

	// Swagger UI
	app.Get("/api/docs", handler.ServeSwaggerUI)
	app.Get("/api/docs/", handler.ServeSwaggerUI)

	// DAV documentation
	app.Get("/api/docs/dav", handler.ServeDAVDocs)
	app.Get("/api/docs/dav/*", handler.ServeDAVDocs)
}
