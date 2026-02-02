package sharing

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
)

type UpdateAddressBookShareInput struct {
	Permission string `json:"permission"`
}

type UpdateAddressBookShareUseCase struct {
	shareRepo       sharing.AddressBookShareRepository
	addressBookRepo addressbook.Repository
}

func NewUpdateAddressBookShareUseCase(
	shareRepo sharing.AddressBookShareRepository,
	addressBookRepo addressbook.Repository,
) *UpdateAddressBookShareUseCase {
	return &UpdateAddressBookShareUseCase{
		shareRepo:       shareRepo,
		addressBookRepo: addressBookRepo,
	}
}

func (uc *UpdateAddressBookShareUseCase) Execute(ctx context.Context, requestingUserID, addressBookID uint, shareUUID string, input UpdateAddressBookShareInput) (*CreateAddressBookShareOutput, error) {
	// 1. Verify ownership
	ab, err := uc.addressBookRepo.GetByID(ctx, addressBookID)
	if err != nil || ab == nil {
		return nil, fmt.Errorf("address book not found")
	}
	if ab.UserID != requestingUserID {
		return nil, fmt.Errorf("permission denied")
	}

	// 2. Get share
	share, err := uc.shareRepo.GetByUUID(ctx, shareUUID)
	if err != nil || share == nil {
		return nil, fmt.Errorf("share not found")
	}
	if share.AddressBookID != addressBookID {
		return nil, fmt.Errorf("share not found")
	}

	// 3. Validate permission
	if input.Permission != "read" && input.Permission != "read-write" {
		return nil, fmt.Errorf("invalid permission")
	}

	// 4. Update
	share.Permission = input.Permission
	if err := uc.shareRepo.Update(ctx, share); err != nil {
		return nil, err
	}

	return &CreateAddressBookShareOutput{
		ID:            share.UUID,
		AddressBookID: ab.UUID,
		SharedWith: UserInfo{
			ID:          share.SharedWith.UUID,
			Username:    share.SharedWith.Username,
			DisplayName: share.SharedWith.DisplayName,
			Email:       share.SharedWith.Email,
		},
		Permission: share.Permission,
		CreatedAt:  share.CreatedAt,
	}, nil
}
