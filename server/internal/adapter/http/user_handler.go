package http

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
	userusecase "github.com/jherrma/caldav-server/internal/usecase/user"
)

type UserHandler struct {
	changePasswordUC *authusecase.ChangePasswordUseCase
	getProfileUC     *userusecase.GetProfileUseCase
	updateProfileUC  *userusecase.UpdateProfileUseCase
	deleteAccountUC  *userusecase.DeleteAccountUseCase
}

func NewUserHandler(
	changePasswordUC *authusecase.ChangePasswordUseCase,
	getProfileUC *userusecase.GetProfileUseCase,
	updateProfileUC *userusecase.UpdateProfileUseCase,
	deleteAccountUC *userusecase.DeleteAccountUseCase,
) *UserHandler {
	return &UserHandler{
		changePasswordUC: changePasswordUC,
		getProfileUC:     getProfileUC,
		updateProfileUC:  updateProfileUC,
		deleteAccountUC:  deleteAccountUC,
	}
}

// GetProfile (GET /api/v1/users/me)
func (h *UserHandler) GetProfile(c fiber.Ctx) error {
	userUUID, ok := c.Locals("user_uuid").(string)
	if !ok {
		return UnauthorizedResponse(c, "Unauthorized")
	}

	u, err := h.getProfileUC.Execute(c.Context(), userUUID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get profile")
	}
	if u == nil {
		return ErrorResponse(c, fiber.StatusNotFound, "User not found")
	}

	res := dto.UserProfileResponse{
		ID:            u.UUID,
		Email:         u.Email,
		DisplayName:   u.DisplayName,
		IsActive:      u.IsActive,
		EmailVerified: u.EmailVerified,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
		AuthMethods:   []string{"local"},
		Stats: dto.UserProfileStats{
			CalendarCount:    0,
			ContactCount:     0,
			AppPasswordCount: 0,
		},
	}

	return SuccessResponse(c, res)
}

// UpdateProfile (PATCH /api/v1/users/me)
func (h *UserHandler) UpdateProfile(c fiber.Ctx) error {
	var req dto.UpdateProfileRequest
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	userUUID, ok := c.Locals("user_uuid").(string)
	if !ok {
		return UnauthorizedResponse(c, "Unauthorized")
	}

	usecaseReq := userusecase.UpdateProfileRequest{
		DisplayName: req.DisplayName,
	}

	u, err := h.updateProfileUC.Execute(c.Context(), userUUID, usecaseReq)
	if err != nil {
		if errors.Is(err, userusecase.ErrEmailAlreadyExists) {
			return ConflictResponse(c, err.Error())
		}
		if errors.Is(err, userusecase.ErrDisplayNameTooLong) {
			return BadRequestResponse(c, err.Error())
		}
		if err.Error() == "user not found" {
			return ErrorResponse(c, fiber.StatusNotFound, err.Error())
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update profile")
	}

	res := dto.UserProfileResponse{
		ID:            u.UUID,
		Email:         u.Email,
		DisplayName:   u.DisplayName,
		IsActive:      u.IsActive,
		EmailVerified: u.EmailVerified,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
		AuthMethods:   []string{"local"},
		Stats: dto.UserProfileStats{
			CalendarCount:    0,
			ContactCount:     0,
			AppPasswordCount: 0,
		},
	}

	return SuccessResponse(c, res)
}

// DeleteAccount (DELETE /api/v1/users/me)
func (h *UserHandler) DeleteAccount(c fiber.Ctx) error {
	var req dto.DeleteAccountRequest
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	userUUID, ok := c.Locals("user_uuid").(string)
	if !ok {
		return UnauthorizedResponse(c, "Unauthorized")
	}

	err := h.deleteAccountUC.Execute(c.Context(), userUUID, req.Password, req.Confirmation)
	if err != nil {
		if errors.Is(err, userusecase.ErrConfirmationRequired) {
			return BadRequestResponse(c, err.Error())
		}
		if errors.Is(err, userusecase.ErrIncorrectPassword) {
			return UnauthorizedResponse(c, "Password is incorrect")
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete account")
	}

	return c.SendStatus(fiber.StatusNoContent)
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
