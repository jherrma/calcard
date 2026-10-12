package apppassword

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"

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

// ErrInvalidInput marks a request the caller has to correct (400), as opposed
// to a server-side failure.
var ErrInvalidInput = errors.New("invalid input")

// normalizeScopes validates the requested scopes and returns what is stored.
// nil (the field omitted or null) means the default, every scope — what the
// web UI preselects. An explicit empty list is refused: it would create a
// password that reaches no calendar and no address book. Duplicates collapse.
func normalizeScopes(scopes []string) ([]string, error) {
	if scopes == nil {
		return user.AppPasswordScopes(), nil
	}
	if len(scopes) == 0 {
		return nil, fmt.Errorf("%w: at least one scope is required (%s)", ErrInvalidInput, strings.Join(user.AppPasswordScopes(), ", "))
	}
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if !user.IsAppPasswordScope(s) {
			return nil, fmt.Errorf("%w: unknown scope '%s', allowed: %s", ErrInvalidInput, s, strings.Join(user.AppPasswordScopes(), ", "))
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (uc *CreateUseCase) Execute(ctx context.Context, req CreateAppPasswordRequest) (*CreateAppPasswordResult, error) {
	name, ok := user.NormalizeCredentialName(req.Name)
	if !ok {
		return nil, fmt.Errorf("%w: name is required and must be at most %d characters", ErrInvalidInput, user.MaxCredentialNameLength)
	}
	scopes, err := normalizeScopes(req.Scopes)
	if err != nil {
		return nil, err
	}

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

	scopesJSON, _ := json.Marshal(scopes)

	ap := &user.AppPassword{
		UUID:         uuid.New().String(),
		UserID:       u.ID,
		Name:         name,
		PasswordHash: string(hash),
		Scopes:       string(scopesJSON),
	}

	if err := uc.repo.Create(ctx, ap); err != nil {
		return nil, err
	}

	uc.logger.LogAppPasswordCreated(ctx, u.ID, name, req.IP, req.UserAgent)

	return &CreateAppPasswordResult{
		ID:        ap.UUID,
		Name:      ap.Name,
		Scopes:    scopes,
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
