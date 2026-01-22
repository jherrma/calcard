package http

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
)

type AuthHandler struct {
	registerUC *authusecase.RegisterUseCase
	verifyUC   *authusecase.VerifyUseCase
	loginUC    *authusecase.LoginUseCase
	refreshUC  *authusecase.RefreshUseCase
	logoutUC   *authusecase.LogoutUseCase
}

func NewAuthHandler(
	registerUC *authusecase.RegisterUseCase,
	verifyUC *authusecase.VerifyUseCase,
	loginUC *authusecase.LoginUseCase,
	refreshUC *authusecase.RefreshUseCase,
	logoutUC *authusecase.LogoutUseCase,
) *AuthHandler {
	return &AuthHandler{
		registerUC: registerUC,
		verifyUC:   verifyUC,
		loginUC:    loginUC,
		refreshUC:  refreshUC,
		logoutUC:   logoutUC,
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
