package http

import (
	"errors"
	"log"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
)

// SetupHandler serves first-run setup: creating the initial administrator.
type SetupHandler struct {
	setupUC *authusecase.SetupUseCase
}

func NewSetupHandler(setupUC *authusecase.SetupUseCase) *SetupHandler {
	return &SetupHandler{setupUC: setupUC}
}

// POST /api/v1/auth/setup — only succeeds while no user exists. It deliberately
// ignores Registration.Disabled: closing self-service registration must not
// make a fresh server impossible to set up.
func (h *SetupHandler) Setup(c fiber.Ctx) error {
	var req dto.SetupRequest
	if err := c.Bind().JSON(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	u, err := h.setupUC.Execute(c.Context(), req.Username, req.Email, req.Password, req.DisplayName)
	if err != nil {
		if errors.Is(err, authusecase.ErrSetupCompleted) {
			return ErrorResponse(c, fiber.StatusConflict, "setup already completed")
		}
		if errors.Is(err, authusecase.ErrInvalidUsername) || isSafeValidationError(err) {
			return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		log.Printf("setup failed: %v", err)
		return ErrorResponse(c, fiber.StatusBadRequest, "setup failed")
	}

	return SuccessResponse(c, dto.RegisterResponse{
		ID:            u.UUID,
		Email:         u.Email,
		Username:      u.Username,
		DisplayName:   u.DisplayName,
		IsActive:      u.IsActive,
		EmailVerified: u.EmailVerified,
		CreatedAt:     u.CreatedAt,
	})
}
