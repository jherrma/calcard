package http

import (
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/config"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
)

type AuthHandler struct {
	registerUC *authusecase.RegisterUseCase
	verifyUC   *authusecase.VerifyUseCase
	loginUC    *authusecase.LoginUseCase
	refreshUC  *authusecase.RefreshUseCase
	logoutUC   *authusecase.LogoutUseCase
	forgotUC   *authusecase.ForgotPasswordUseCase
	resetUC    *authusecase.ResetPasswordUseCase
	config     *config.Config
}

func NewAuthHandler(
	registerUC *authusecase.RegisterUseCase,
	verifyUC *authusecase.VerifyUseCase,
	loginUC *authusecase.LoginUseCase,
	refreshUC *authusecase.RefreshUseCase,
	logoutUC *authusecase.LogoutUseCase,
	forgotUC *authusecase.ForgotPasswordUseCase,
	resetUC *authusecase.ResetPasswordUseCase,
	cfg *config.Config,
) *AuthHandler {
	return &AuthHandler{
		registerUC: registerUC,
		verifyUC:   verifyUC,
		loginUC:    loginUC,
		refreshUC:  refreshUC,
		logoutUC:   logoutUC,
		forgotUC:   forgotUC,
		resetUC:    resetUC,
		config:     cfg,
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
		if err == authusecase.ErrUserAlreadyExists {
			return ErrorResponse(c, fiber.StatusConflict, "Email already registered")
		}
		return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return SuccessResponse(c, dto.RegisterResponse{
		ID:            user.UUID,
		Email:         user.Email,
		Username:      user.Username,
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

// Login (POST /api/v1/auth/login)
func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req dto.LoginRequest
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	res, err := h.loginUC.Execute(c.Context(), req.Email, req.Password, c.Get("User-Agent"), c.IP())
	if err != nil {
		if err == authusecase.ErrInvalidCredentials || err == authusecase.ErrInactiveAccount {
			return UnauthorizedResponse(c, err.Error())
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Internal server error")
	}

	return SuccessResponse(c, dto.LoginResponse{
		AccessToken:  res.AccessToken,
		RefreshToken: res.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    res.ExpiresAt.Unix(),
		User: dto.UserResponse{
			ID:          res.User.UUID,
			Email:       res.User.Email,
			DisplayName: res.User.DisplayName,
		},
	})
}

// Refresh (POST /api/v1/auth/refresh)
func (h *AuthHandler) Refresh(c fiber.Ctx) error {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	res, err := h.refreshUC.Execute(c.Context(), req.RefreshToken)
	if err != nil {
		return UnauthorizedResponse(c, "Invalid or expired refresh token")
	}

	return SuccessResponse(c, fiber.Map{
		"access_token": res.AccessToken,
		"token_type":   "Bearer",
		"expires_at":   res.ExpiresAt.Unix(),
	})
}

// Logout (POST /api/v1/auth/logout)
func (h *AuthHandler) Logout(c fiber.Ctx) error {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.Bind().JSON(&req); err != nil {
		// If no token in body, maybe check header? AC says POST body for refresh/logout
		return BadRequestResponse(c, "Invalid request body")
	}

	if err := h.logoutUC.Execute(c.Context(), req.RefreshToken); err != nil {
		// Log error but return success to avoid leaking info?
		// Actually, if it fails, it's usually already deleted or non-existent.
	}

	return SuccessResponse(c, "Logged out successfully")
}

// ForgotPassword (POST /api/v1/auth/forgot-password)
func (h *AuthHandler) ForgotPassword(c fiber.Ctx) error {
	var req struct {
		Email string `json:"email"`
	}
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	usecaseReq := authusecase.ForgotPasswordRequest{
		Email:   req.Email,
		BaseURL: h.config.BaseURL,
	}

	if err := h.forgotUC.Execute(c.Context(), usecaseReq); err != nil {
		// Log error but return success to prevent enumeration
		fmt.Printf("Forgot password failed: %v\n", err)
	}

	return SuccessResponse(c, "If an account with that email exists, a password reset link has been sent.")
}

// ResetPassword (POST /api/v1/auth/reset-password)
func (h *AuthHandler) ResetPassword(c fiber.Ctx) error {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	usecaseReq := authusecase.ResetPasswordRequest{
		Token:       req.Token,
		NewPassword: req.NewPassword,
	}

	if err := h.resetUC.Execute(c.Context(), usecaseReq); err != nil {
		if errors.Is(err, authusecase.ErrInvalidToken) {
			return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to reset password")
	}

	return SuccessResponse(c, "Password has been reset successfully. Please login with your new password.")
}
