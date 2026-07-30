package http

import (
	"github.com/gofiber/fiber/v3"
	aboutuc "github.com/jherrma/caldav-server/internal/usecase/about"
)

// Referenced so swag emits the schema for the embedded manifest types (#101).
var _ = aboutuc.OpenSourcePackage{}

// AboutHandler serves project metadata (open-source attribution, story 101).
type AboutHandler struct {
	listOpenSourceUC *aboutuc.ListOpenSourceUseCase
}

// NewAboutHandler creates a new AboutHandler
func NewAboutHandler(listOpenSourceUC *aboutuc.ListOpenSourceUseCase) *AboutHandler {
	return &AboutHandler{listOpenSourceUC: listOpenSourceUC}
}

// OpenSource godoc
// @Summary      List backend open-source dependencies
// @Description  Returns the attribution list for the Go modules linked into the server binary. The list is generated at build time (`go run ./tools/genlicenses`) and embedded in the binary, so no network access is involved. License detection is best-effort — a license of "unknown" means it could not be determined automatically, NOT that the package is unlicensed.
// @Tags         About
// @Produce      json
// @Success      200  {object}  object{generator=string,note=string,count=int,packages=[]about.OpenSourcePackage}
// @Failure      401  {object}  ErrorResponseBody
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /about/open-source [get]
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
