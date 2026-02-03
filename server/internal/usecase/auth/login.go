package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInactiveAccount    = errors.New("account is not active")
)

// LoginUseCase handles user authentication
type LoginUseCase struct {
	userRepo   user.UserRepository
	tokenRepo  user.RefreshTokenRepository
	jwtManager user.TokenProvider
	cfg        *config.Config
	logger     *logging.SecurityLogger
}

// LoginResult contains the tokens and user info after successful login
type LoginResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	User         *user.User
}

// NewLoginUseCase creates a new login use case
func NewLoginUseCase(
	userRepo user.UserRepository,
	tokenRepo user.RefreshTokenRepository,
	jwtManager user.TokenProvider,
	cfg *config.Config,
	logger *logging.SecurityLogger,
) *LoginUseCase {
	return &LoginUseCase{
		userRepo:   userRepo,
		tokenRepo:  tokenRepo,
		jwtManager: jwtManager,
		cfg:        cfg,
		logger:     logger,
	}
}

// Execute performs the login logic
func (uc *LoginUseCase) Execute(ctx context.Context, email, password string, userAgent, ip string) (*LoginResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	u, err := uc.userRepo.GetByEmail(ctx, email)
	if err != nil {
		uc.logger.LogLoginAttempt(ctx, email, ip, userAgent, false, "db_error")
		return nil, ErrInvalidCredentials
	}
	if u == nil {
		uc.logger.LogLoginAttempt(ctx, email, ip, userAgent, false, "user_not_found")
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		uc.logger.LogLoginAttempt(ctx, email, ip, userAgent, false, "invalid_password")
		return nil, ErrInvalidCredentials
	}

	if !u.IsActive {
		uc.logger.LogLoginAttempt(ctx, email, ip, userAgent, false, "account_inactive")
		return nil, ErrInactiveAccount
	}

	uc.logger.LogLoginAttempt(ctx, email, ip, userAgent, true, "")

	accessToken, expiresAt, err := uc.jwtManager.GenerateAccessToken(u.UUID, u.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := uc.jwtManager.GenerateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	hash := uc.jwtManager.HashToken(refreshToken)

	rt := &user.RefreshToken{
		UserID:    u.ID,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(uc.cfg.JWT.RefreshExpiry),
		UserAgent: userAgent,
		IP:        ip,
	}

	if err := uc.tokenRepo.Create(ctx, rt); err != nil {
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}

	return &LoginResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		User:         u,
	}, nil
}
