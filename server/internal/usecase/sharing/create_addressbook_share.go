package sharing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
)

type CreateAddressBookShareInput struct {
	AddressBookID  uint   `json:"addressbook_id"`
	UserIdentifier string `json:"user_identifier"` // Email or Username
	Permission     string `json:"permission"`
}

type CreateAddressBookShareOutput struct {
	ID            string    `json:"id"`
	AddressBookID string    `json:"addressbook_id"`
	SharedWith    UserInfo  `json:"shared_with"`
	Permission    string    `json:"permission"`
	CreatedAt     time.Time `json:"created_at"`
}

type CreateAddressBookShareUseCase struct {
	shareRepo       sharing.AddressBookShareRepository
	addressBookRepo addressbook.Repository
	userRepo        user.UserRepository
}

func NewCreateAddressBookShareUseCase(
	shareRepo sharing.AddressBookShareRepository,
	addressBookRepo addressbook.Repository,
	userRepo user.UserRepository,
) *CreateAddressBookShareUseCase {
	return &CreateAddressBookShareUseCase{
		shareRepo:       shareRepo,
		addressBookRepo: addressBookRepo,
		userRepo:        userRepo,
	}
}

func (uc *CreateAddressBookShareUseCase) Execute(ctx context.Context, requestingUserID uint, input CreateAddressBookShareInput) (*CreateAddressBookShareOutput, error) {
	// 1. Verify address book ownership
	ab, err := uc.addressBookRepo.GetByID(ctx, input.AddressBookID)
	if err != nil || ab == nil {
		return nil, fmt.Errorf("address book not found")
	}
	if ab.UserID != requestingUserID {
		return nil, fmt.Errorf("permission denied")
	}

	// 2. Find target user
	targetUser, err := uc.userRepo.GetByEmail(ctx, input.UserIdentifier)
	if err != nil || targetUser == nil {
		// Try by username
		targetUser, err = uc.userRepo.GetByUsername(ctx, input.UserIdentifier)
	}
	if err != nil || targetUser == nil {
		return nil, fmt.Errorf("user '%s' not found", input.UserIdentifier)
	}

	// 3. Validation
	if targetUser.ID == requestingUserID {
		return nil, fmt.Errorf("cannot share address book with yourself")
	}
	if input.Permission != "read" && input.Permission != "read-write" {
		return nil, fmt.Errorf("invalid permission")
	}

	// 4. Check existing share
	existing, _ := uc.shareRepo.GetByAddressBookAndUser(ctx, input.AddressBookID, targetUser.ID)
	if existing != nil {
		return nil, fmt.Errorf("address book is already shared with this user")
	}

	// 5. Create share
	share := &sharing.AddressBookShare{
		UUID:          uuid.New().String(),
		AddressBookID: input.AddressBookID,
		SharedWithID:  targetUser.ID,
		Permission:    input.Permission,
	}

	if err := uc.shareRepo.Create(ctx, share); err != nil {
		return nil, err
	}

	// 6. Return output
	return &CreateAddressBookShareOutput{
		ID:            share.UUID,
		AddressBookID: ab.UUID,
		SharedWith: UserInfo{
			ID:          targetUser.UUID,
			Username:    targetUser.Username,
			DisplayName: targetUser.DisplayName,
			Email:       targetUser.Email,
		},
		Permission: share.Permission,
		CreatedAt:  share.CreatedAt,
	}, nil
}
