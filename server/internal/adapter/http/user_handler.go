package http

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
)

type UserHandler struct {
	changePasswordUC *authusecase.ChangePasswordUseCase
}

func NewUserHandler(changePasswordUC *authusecase.ChangePasswordUseCase) *UserHandler {
	return &UserHandler{
		changePasswordUC: changePasswordUC,
	}
}

// ChangePassword (PUT /api/v1/users/me/password)
func (h *UserHandler) ChangePassword(c fiber.Ctx) error {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	// Get user UUID from middleware context
	userUUID, ok := c.Locals("user_uuid").(string)
	if !ok {
		return UnauthorizedResponse(c, "Unauthorized")
	}

	usecaseReq := authusecase.ChangePasswordRequest{
		UserUUID:        userUUID,
		CurrentPassword: req.CurrentPassword,
		NewPassword:     req.NewPassword,
	}

	res, err := h.changePasswordUC.Execute(c.Context(), usecaseReq)
	if err != nil {
		if errors.Is(err, authusecase.ErrIncorrectPassword) || errors.Is(err, authusecase.ErrSamePassword) {
			return ErrorResponse(c, fiber.StatusUnauthorized, err.Error())
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to change password")
	}

	return SuccessResponse(c, fiber.Map{
		"message":      "Password changed successfully",
		"access_token": res.AccessToken,
	})
}
