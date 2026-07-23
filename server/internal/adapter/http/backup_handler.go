package http

import (
	"bufio"
	"log"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/usecase/importexport"
)

// BackupHandler handles backup export HTTP requests
type BackupHandler struct {
	backupExportUC *importexport.BackupExportUseCase
}

// NewBackupHandler creates a new backup handler
func NewBackupHandler(backupExportUC *importexport.BackupExportUseCase) *BackupHandler {
	return &BackupHandler{backupExportUC: backupExportUC}
}

// Export handles GET /api/v1/users/me/export
func (h *BackupHandler) Export(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	// Compute the filename and commit the headers BEFORE streaming: once the
	// response body starts flowing, the status line and headers are already
	// sent and can no longer change. No Content-Length is set — the archive is
	// produced incrementally and its size is unknown up front.
	filename := h.backupExportUC.Filename()
	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	// Capture the request context and user id up front: the Fiber Ctx is
	// recycled once this handler returns, but the stream writer below runs
	// later, while fasthttp flushes the response body.
	ctx := c.Context()

	// Stream the archive straight to the socket. The status has already been
	// sent, so a mid-stream failure can't be surfaced to the client as an error
	// code — we log it and let the archive truncate (the accepted failure mode)
	// rather than pretend it succeeded.
	return c.SendStreamWriter(func(w *bufio.Writer) {
		if _, err := h.backupExportUC.Execute(ctx, userID, w); err != nil {
			log.Printf("backup export stream failed for user %d: %v", userID, err)
		}
	})
}
