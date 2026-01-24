package user

import (
	"context"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/crypto/bcrypt"
)

type mockUserRepo struct {
	mock.Mock
}

func (m *mockUserRepo) Create(ctx context.Context, u *user.User) error {
	args := m.Called(ctx, u)
	return args.Error(0)
}

func (m *mockUserRepo) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.User), args.Error(1)
}

func (m *mockUserRepo) GetByUUID(ctx context.Context, uuid string) (*user.User, error) {
	args := m.Called(ctx, uuid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.User), args.Error(1)
}

func (m *mockUserRepo) GetByID(ctx context.Context, id uint) (*user.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.User), args.Error(1)
}

func (m *mockUserRepo) Update(ctx context.Context, u *user.User) error {
	args := m.Called(ctx, u)
	return args.Error(0)
}

func (m *mockUserRepo) Delete(ctx context.Context, userID uint) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *mockUserRepo) CreateVerification(ctx context.Context, v *user.EmailVerification) error {
	args := m.Called(ctx, v)
	return args.Error(0)
}

func (m *mockUserRepo) GetVerificationByToken(ctx context.Context, token string) (*user.EmailVerification, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*user.EmailVerification), args.Error(1)
}

func (m *mockUserRepo) DeleteVerification(ctx context.Context, token string) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

func TestGetProfileUseCase_Execute(t *testing.T) {
	repo := new(mockUserRepo)
	uc := NewGetProfileUseCase(repo)

	ctx := context.Background()
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	u := &user.User{UUID: uuid, Email: "test@example.com", DisplayName: "Test User"}

	repo.On("GetByUUID", ctx, uuid).Return(u, nil)

	result, err := uc.Execute(ctx, uuid)

	assert.NoError(t, err)
	assert.Equal(t, u, result)
	repo.AssertExpectations(t)
}

func TestUpdateProfileUseCase_Execute(t *testing.T) {
	repo := new(mockUserRepo)
	uc := NewUpdateProfileUseCase(repo)

	ctx := context.Background()
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	u := &user.User{UUID: uuid, Email: "test@example.com", DisplayName: "Test User"}

	t.Run("Update DisplayName", func(t *testing.T) {
		newName := "New Name"
		repo.On("GetByUUID", ctx, uuid).Return(u, nil).Once()
		repo.On("Update", ctx, mock.MatchedBy(func(usr *user.User) bool {
			return usr.DisplayName == newName
		})).Return(nil).Once()

		result, err := uc.Execute(ctx, uuid, UpdateProfileRequest{DisplayName: &newName})

		assert.NoError(t, err)
		assert.Equal(t, newName, result.DisplayName)
	})
}

func TestDeleteAccountUseCase_Execute(t *testing.T) {
	repo := new(mockUserRepo)
	uc := NewDeleteAccountUseCase(repo)

	ctx := context.Background()
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	password := "SecurePass123!"
	fromHash, _ := bcrypt.GenerateFromPassword([]byte(password), 12)
	u := &user.User{ID: 1, UUID: uuid, PasswordHash: string(fromHash)}

	t.Run("Success", func(t *testing.T) {
		repo.On("GetByUUID", ctx, uuid).Return(u, nil).Once()
		repo.On("Delete", ctx, u.ID).Return(nil).Once()

		err := uc.Execute(ctx, uuid, password, "DELETE")

		assert.NoError(t, err)
	})

	t.Run("Confirmation Failure", func(t *testing.T) {
		err := uc.Execute(ctx, uuid, password, "WRONG")
		assert.Equal(t, ErrConfirmationRequired, err)
	})
}
