package http

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
	userusecase "github.com/jherrma/caldav-server/internal/usecase/user"
)

type UserHandler struct {
	changePasswordUC    *authusecase.ChangePasswordUseCase
	getProfileUC        *userusecase.GetProfileUseCase
	updateProfileUC     *userusecase.UpdateProfileUseCase
	deleteAccountUC     *userusecase.DeleteAccountUseCase
	getPreferencesUC    *userusecase.GetPreferencesUseCase
	updatePreferencesUC *userusecase.UpdatePreferencesUseCase
	calendarRepo        calendar.CalendarRepository
	addressBookRepo     addressbook.Repository
	appPasswordRepo     user.AppPasswordRepository
}

func NewUserHandler(
	changePasswordUC *authusecase.ChangePasswordUseCase,
	getProfileUC *userusecase.GetProfileUseCase,
	updateProfileUC *userusecase.UpdateProfileUseCase,
	deleteAccountUC *userusecase.DeleteAccountUseCase,
	getPreferencesUC *userusecase.GetPreferencesUseCase,
	updatePreferencesUC *userusecase.UpdatePreferencesUseCase,
	calendarRepo calendar.CalendarRepository,
	addressBookRepo addressbook.Repository,
	appPasswordRepo user.AppPasswordRepository,
) *UserHandler {
	return &UserHandler{
		changePasswordUC:    changePasswordUC,
		getProfileUC:        getProfileUC,
		updateProfileUC:     updateProfileUC,
		deleteAccountUC:     deleteAccountUC,
		getPreferencesUC:    getPreferencesUC,
		updatePreferencesUC: updatePreferencesUC,
		calendarRepo:        calendarRepo,
		addressBookRepo:     addressBookRepo,
		appPasswordRepo:     appPasswordRepo,
	}
}

// GetProfile godoc
// @Summary      Get user profile
// @Description  Get current user's profile information
// @Tags         Users
// @Produce      json
// @Success      200  {object}  dto.UserProfileResponse
// @Failure      401  {object}  ErrorResponseBody
// @Failure      404  {object}  ErrorResponseBody
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /users/me [get]
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

	calCount, _ := h.calendarRepo.CountByUserID(c.Context(), u.ID)
	contactCount, _ := h.addressBookRepo.CountContactsByUserID(c.Context(), u.ID)
	appPwdCount, _ := h.appPasswordRepo.CountByUserID(c.Context(), u.ID)

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
			CalendarCount:    int(calCount),
			ContactCount:     int(contactCount),
			AppPasswordCount: int(appPwdCount),
		},
	}

	return SuccessResponse(c, res)
}

// UpdateProfile godoc
// @Summary      Update user profile
// @Description  Update current user's display name
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        profile  body      dto.UpdateProfileRequest  true  "Profile updates"
// @Success      200      {object}  dto.UserProfileResponse
// @Failure      400      {object}  ErrorResponseBody
// @Failure      401      {object}  ErrorResponseBody
// @Failure      404      {object}  ErrorResponseBody
// @Failure      409      {object}  ErrorResponseBody
// @Failure      500      {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /users/me [patch]
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

	calCount, _ := h.calendarRepo.CountByUserID(c.Context(), u.ID)
	contactCount, _ := h.addressBookRepo.CountContactsByUserID(c.Context(), u.ID)
	appPwdCount, _ := h.appPasswordRepo.CountByUserID(c.Context(), u.ID)

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
			CalendarCount:    int(calCount),
			ContactCount:     int(contactCount),
			AppPasswordCount: int(appPwdCount),
		},
	}

	return SuccessResponse(c, res)
}

// DeleteAccount godoc
// @Summary      Delete user account
// @Description  Permanently delete user account
// @Tags         Users
// @Accept       json
// @Param        request  body  dto.DeleteAccountRequest  true  "Delete confirmation"
// @Success      204
// @Failure      400       {object}  ErrorResponseBody
// @Failure      401       {object}  ErrorResponseBody
// @Failure      500       {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /users/me [delete]
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

// ChangePassword godoc
// @Summary      Change password
// @Description  Change the current user's password
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        request  body      dto.ChangePasswordRequest  true  "Password change details"
// @Success      200      {object}  object{message=string,access_token=string}
// @Failure      400      {object}  ErrorResponseBody
// @Failure      401      {object}  ErrorResponseBody
// @Failure      500      {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /users/me/password [put]
func (h *UserHandler) ChangePassword(c fiber.Ctx) error {
	var req dto.ChangePasswordRequest
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
		IP:              c.IP(),
		UserAgent:       c.Get("User-Agent"),
	}

	res, err := h.changePasswordUC.Execute(c.Context(), usecaseReq)
	if err != nil {
		if errors.Is(err, authusecase.ErrIncorrectPassword) || errors.Is(err, authusecase.ErrSamePassword) {
			return ErrorResponse(c, fiber.StatusUnauthorized, err.Error())
		}
		// Password complexity sentinels carry safe, actionable messages so the
		// client can show the same policy feedback the register page does.
		if isSafeValidationError(err) {
			return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to change password")
	}

	return SuccessResponse(c, fiber.Map{
		"message":      "Password changed successfully",
		"access_token": res.AccessToken,
	})
}

// GetPreferences godoc
// @Summary      Get user preferences
// @Description  Get the current user's preferences. Keys the user has not set are returned with their default value.
// @Tags         Users
// @Produce      json
// @Success      200  {object}  dto.PreferencesResponse
// @Failure      401  {object}  ErrorResponseBody
// @Failure      404  {object}  ErrorResponseBody
// @Failure      500  {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /users/me/preferences [get]
func (h *UserHandler) GetPreferences(c fiber.Ctx) error {
	userUUID, ok := c.Locals("user_uuid").(string)
	if !ok {
		return UnauthorizedResponse(c, "Unauthorized")
	}

	prefs, err := h.getPreferencesUC.Execute(c.Context(), userUUID)
	if err != nil {
		if errors.Is(err, userusecase.ErrUserNotFound) {
			return ErrorResponse(c, fiber.StatusNotFound, "User not found")
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get preferences")
	}

	return SuccessResponse(c, dto.PreferencesResponse{Preferences: prefs})
}

// UpdatePreferences godoc
// @Summary      Update user preferences
// @Description  Upsert one or more preferences. Unknown keys and out-of-range values are rejected with 400; the full updated map is returned.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        preferences  body      dto.UpdatePreferencesRequest  true  "Preferences to set"
// @Success      200          {object}  dto.PreferencesResponse
// @Failure      400          {object}  ErrorResponseBody
// @Failure      401          {object}  ErrorResponseBody
// @Failure      404          {object}  ErrorResponseBody
// @Failure      500          {object}  ErrorResponseBody
// @Security     BearerAuth
// @Router       /users/me/preferences [patch]
func (h *UserHandler) UpdatePreferences(c fiber.Ctx) error {
	var req dto.UpdatePreferencesRequest
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	userUUID, ok := c.Locals("user_uuid").(string)
	if !ok {
		return UnauthorizedResponse(c, "Unauthorized")
	}

	prefs, err := h.updatePreferencesUC.Execute(c.Context(), userUUID, req.Preferences)
	if err != nil {
		// The validation sentinels carry the offending key and its allowed values,
		// so the message is safe (and useful) to hand back verbatim.
		if errors.Is(err, userusecase.ErrUnknownPreferenceKey) ||
			errors.Is(err, userusecase.ErrInvalidPreferenceValue) ||
			errors.Is(err, userusecase.ErrNoPreferencesGiven) {
			return BadRequestResponse(c, err.Error())
		}
		if errors.Is(err, userusecase.ErrUserNotFound) {
			return ErrorResponse(c, fiber.StatusNotFound, "User not found")
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update preferences")
	}

	return SuccessResponse(c, dto.PreferencesResponse{Preferences: prefs})
}
