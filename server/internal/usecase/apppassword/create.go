package apppassword

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
	"golang.org/x/crypto/bcrypt"
)

type CreateAppPasswordRequest struct {
	UserUUID  string
	Name      string
	Scopes    []string
	IP        string
	UserAgent string
}

type CreateAppPasswordResult struct {
	ID        string
	Name      string
	Scopes    []string
	Password  string
	CreatedAt string
	Username  string
}

type CreateUseCase struct {
	userRepo user.UserRepository
	repo     user.AppPasswordRepository
	logger   *logging.SecurityLogger
}

func NewCreateUseCase(userRepo user.UserRepository, repo user.AppPasswordRepository, logger *logging.SecurityLogger) *CreateUseCase {
	return &CreateUseCase{userRepo: userRepo, repo: repo, logger: logger}
}

func (uc *CreateUseCase) Execute(ctx context.Context, req CreateAppPasswordRequest) (*CreateAppPasswordResult, error) {
	u, err := uc.userRepo.GetByUUID(ctx, req.UserUUID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, fmt.Errorf("user not found")
	}

	rawPassword := generateAppPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(rawPassword), 12)
	if err != nil {
		return nil, err
	}

	scopesJSON, _ := json.Marshal(req.Scopes)

	ap := &user.AppPassword{
		UUID:         uuid.New().String(),
		UserID:       u.ID,
		Name:         req.Name,
		PasswordHash: string(hash),
		Scopes:       string(scopesJSON),
	}

	if err := uc.repo.Create(ctx, ap); err != nil {
		return nil, err
	}

	uc.logger.LogAppPasswordCreated(ctx, u.ID, req.Name, req.IP, req.UserAgent)

	return &CreateAppPasswordResult{
		ID:        ap.UUID,
		Name:      ap.Name,
		Scopes:    req.Scopes,
		Password:  rawPassword,
		CreatedAt: ap.CreatedAt.Format("2006-01-02T15:04:05Z"),
		Username:  u.Username,
	}, nil
}

func generateAppPassword() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 24)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}
