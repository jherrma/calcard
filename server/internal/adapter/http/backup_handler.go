package http

import (
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

	data, filename, err := h.backupExportUC.Execute(c.Context(), userID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	return c.Send(data)
}
