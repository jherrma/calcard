package http

import (
	"errors"
	"log"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
	authusecase "github.com/jherrma/caldav-server/internal/usecase/auth"
)

// isSafeValidationError reports whether err is one of the domain email/password
// validation sentinels whose message is safe (and useful) to return to the
// client. Everything else — repository/driver errors in particular — must be
// logged server-side and answered with a generic message.
func isSafeValidationError(err error) bool {
	switch {
	case errors.Is(err, user.ErrInvalidEmail),
		errors.Is(err, user.ErrPasswordTooShort),
		errors.Is(err, user.ErrPasswordTooLong),
		errors.Is(err, user.ErrPasswordNoUpper),
		errors.Is(err, user.ErrPasswordNoLower),
		errors.Is(err, user.ErrPasswordNoDigit),
		errors.Is(err, user.ErrPasswordNoSpecial):
		return true
	default:
		return false
	}
}

// loginFailedUnverifiedMessage is the single 401 message login answers with
// while e-mail verification is required, for a wrong password and an
// unconfirmed account alike (P01).
const loginFailedUnverifiedMessage = "invalid email or password, or the email address has not been confirmed yet"

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

// POST /api/v1/auth/register
//
// Answers 200 (not 201) with the RegisterResponse wrapped in {status, data}.
//
// With SMTP configured, registering an address that already has an account
// returns exactly the same body as a fresh registration — the owner is told
// by e-mail instead (P01). Without SMTP the new account is active at once, so
// a duplicate cannot be disguised as success and answers 409.
func (h *AuthHandler) Register(c fiber.Ctx) error {
	if h.config.Registration.Disabled {
		return ErrorResponse(c, fiber.StatusForbidden, "registration is disabled")
	}
	var req dto.RegisterRequest
	if err := c.Bind().JSON(&req); err != nil {
		return ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	verificationRequired := h.config.SMTP.Host != ""

	user, _, err := h.registerUC.Execute(c.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		if errors.Is(err, authusecase.ErrSetupRequired) {
			return ErrorResponse(c, fiber.StatusForbidden, "server setup is not complete: finish the initial setup first")
		}
		if errors.Is(err, authusecase.ErrUserAlreadyExists) {
			if verificationRequired {
				return SuccessResponse(c, pendingRegistrationResponse(req.Email, req.DisplayName))
			}
			return ErrorResponse(c, fiber.StatusConflict, "Email already registered")
		}
		// Email/password validation sentinels carry safe, actionable messages
		// (e.g. "password must be at least 8 characters"). Anything else is a
		// repository/driver error whose verbatim text (GORM/SQL) must not leak
		// to the client — log it server-side and return a generic message.
		if isSafeValidationError(err) {
			return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		log.Printf("register failed: %v", err)
		return ErrorResponse(c, fiber.StatusBadRequest, "registration failed")
	}

	if verificationRequired {
		return SuccessResponse(c, pendingRegistrationResponse(req.Email, req.DisplayName))
	}

	return SuccessResponse(c, dto.RegisterResponse{
		ID:            user.UUID,
		Email:         user.Email,
		Username:      user.Username,
		DisplayName:   user.DisplayName,
		IsActive:      user.IsActive,
		EmailVerified: user.EmailVerified,
		CreatedAt:     &user.CreatedAt,
	})
}

// pendingRegistrationResponse is the one body a registration answers with
// while e-mail verification is required — for a new address and a taken one
// alike. It is built from the request alone, never from a stored row, so no
// field can differ between the two cases.
func pendingRegistrationResponse(email, displayName string) dto.RegisterResponse {
	return dto.RegisterResponse{
		Email:         strings.ToLower(strings.TrimSpace(email)),
		DisplayName:   displayName,
		IsActive:      false,
		EmailVerified: false,
	}
}

// GET /api/v1/auth/verify
func (h *AuthHandler) Verify(c fiber.Ctx) error {
	token := c.Query("token")
	if token == "" {
		return ErrorResponse(c, fiber.StatusBadRequest, "Token is required")
	}

	if err := h.verifyUC.Execute(c.Context(), token); err != nil {
		if errors.Is(err, authusecase.ErrInvalidToken) {
			return ErrorResponse(c, fiber.StatusBadRequest, "invalid or expired verification token")
		}
		// Underlying error is a repository/driver failure — don't leak it.
		log.Printf("email verification failed: %v", err)
		return ErrorResponse(c, fiber.StatusBadRequest, "verification failed")
	}

	return SuccessResponse(c, "Account verified successfully")
}

// POST /api/v1/auth/login
func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req dto.LoginRequest
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	res, err := h.loginUC.Execute(c.Context(), req.Email, req.Password, c.Get("User-Agent"), c.IP())
	if err != nil {
		if err == authusecase.ErrInvalidCredentials || err == authusecase.ErrInactiveAccount {
			// With SMTP on, "not active" would tell whoever just registered an
			// address with a password of their choosing whether that created
			// an account (inactive) or hit an existing one (wrong password) —
			// undoing the generic register response (P01). One message
			// covers both.
			if h.config.SMTP.Host != "" {
				return UnauthorizedResponse(c, loginFailedUnverifiedMessage)
			}
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
			Username:    res.User.Username,
			DisplayName: res.User.DisplayName,
			IsAdmin:     res.User.IsAdmin,
		},
	})
}

// POST /api/v1/auth/refresh
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

	// The refresh token is rotated on every use: the client MUST replace the
	// token it presented with res.RefreshToken or its next refresh will present
	// a now-revoked token (#75).
	return SuccessResponse(c, fiber.Map{
		"access_token":  res.AccessToken,
		"refresh_token": res.RefreshToken,
		"token_type":    "Bearer",
		"expires_at":    res.ExpiresAt.Unix(),
	})
}

// POST /api/v1/auth/logout
func (h *AuthHandler) Logout(c fiber.Ctx) error {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	// A missing or unparseable body carries no refresh token to revoke, so it
	// is a success rather than a 400: the client clears its own session either
	// way.
	_ = c.Bind().JSON(&req)

	// The route is public (an expired access token must not prevent logout),
	// but a bearer token, when sent, also identifies whose access tokens to
	// invalidate.
	var accessToken string
	if parts := strings.SplitN(c.Get("Authorization"), " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		accessToken = strings.TrimSpace(parts[1])
	}

	if req.RefreshToken == "" && accessToken == "" {
		return SuccessResponse(c, "Logged out successfully")
	}

	if err := h.logoutUC.Execute(c.Context(), req.RefreshToken, accessToken); err != nil {
		// A revoke failure usually means the token was already deleted or never
		// existed. Return success regardless to avoid leaking token state.
	}

	return SuccessResponse(c, "Logged out successfully")
}

// POST /api/v1/auth/forgot-password
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
		// Log error but return success to prevent enumeration.
		log.Printf("forgot password failed: %v", err)
	}

	return SuccessResponse(c, "If an account with that email exists, a password reset link has been sent.")
}

// POST /api/v1/auth/reset-password
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
		// ErrInvalidResetToken is the sentinel the reset use case returns for a
		// bad/expired reset link; ErrInvalidToken covers the shared verification
		// sentinel. Both are safe, actionable 400s rather than a generic 500.
		if errors.Is(err, authusecase.ErrInvalidResetToken) || errors.Is(err, authusecase.ErrInvalidToken) {
			return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		// Password complexity sentinels carry safe, actionable messages so the
		// client can show the same policy feedback the register page does.
		if isSafeValidationError(err) {
			return ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to reset password")
	}

	return SuccessResponse(c, "Password has been reset successfully. Please login with your new password.")
}
