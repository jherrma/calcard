package http

import (
	"github.com/gofiber/fiber/v3"
	aboutuc "github.com/jherrma/caldav-server/internal/usecase/about"
)

// AboutHandler serves project metadata (open-source attribution, story 101).
type AboutHandler struct {
	listOpenSourceUC *aboutuc.ListOpenSourceUseCase
}

// NewAboutHandler creates a new AboutHandler
func NewAboutHandler(listOpenSourceUC *aboutuc.ListOpenSourceUseCase) *AboutHandler {
	return &AboutHandler{listOpenSourceUC: listOpenSourceUC}
}

// GET /api/v1/about/open-source
//
// Returns the attribution list for the Go modules linked into the server
// binary. The list is generated at build time (`go run ./tools/genlicenses`)
// and embedded, so serving it involves no network access. License detection is
// best-effort — a license of "unknown" means it could not be determined
// automatically, NOT that the package is unlicensed.
func (h *AboutHandler) OpenSource(c fiber.Ctx) error {
	man, err := h.listOpenSourceUC.Execute()
	if err != nil {
		// A broken embed is a build/packaging defect, not user error.
		return ErrorResponse(c, fiber.StatusInternalServerError, "open-source attribution list unavailable")
	}

	return SuccessResponse(c, fiber.Map{
		"generator": man.Generator,
		"note":      man.Note,
		"count":     man.Count,
		"packages":  man.Packages,
	})
}
