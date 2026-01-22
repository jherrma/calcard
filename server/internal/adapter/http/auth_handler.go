package http

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/usecase/auth"
)

type AuthHandler struct {
	registerUC *auth.RegisterUseCase
	verifyUC   *auth.VerifyUseCase
}

func NewAuthHandler(registerUC *auth.RegisterUseCase, verifyUC *auth.VerifyUseCase) *AuthHandler {
	return &AuthHandler{
		registerUC: registerUC,
		verifyUC:   verifyUC,
	}
}

// Register (POST /api/v1/auth/register)
func (h *AuthHandler) Register(c fiber.Ctx) error {
	var req dto.RegisterRequest
	if err := c.Bind().JSON(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	user, _, err := h.registerUC.Execute(c.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		if err == auth.ErrUserAlreadyExists {
			return ErrorResponse(c, fiber.StatusConflict, "Email already registered")
		}
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return SuccessResponse(c, dto.RegisterResponse{
		ID:            user.UUID,
		Email:         user.Email,
		DisplayName:   user.DisplayName,
		IsActive:      user.IsActive,
		EmailVerified: user.EmailVerified,
		CreatedAt:     user.CreatedAt,
	})
}

// Verify (GET /api/v1/auth/verify)
func (h *AuthHandler) Verify(c fiber.Ctx) error {
	token := c.Query("token")
	if token == "" {
		return ErrorResponse(c, fiber.StatusBadRequest, "Token is required")
	}

	if err := h.verifyUC.Execute(c.Context(), token); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return SuccessResponse(c, "Account verified successfully")
}
