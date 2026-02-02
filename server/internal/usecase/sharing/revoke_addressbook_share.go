package sharing

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
)

type RevokeAddressBookShareUseCase struct {
	shareRepo       sharing.AddressBookShareRepository
	addressBookRepo addressbook.Repository
}

func NewRevokeAddressBookShareUseCase(
	shareRepo sharing.AddressBookShareRepository,
	addressBookRepo addressbook.Repository,
) *RevokeAddressBookShareUseCase {
	return &RevokeAddressBookShareUseCase{
		shareRepo:       shareRepo,
		addressBookRepo: addressBookRepo,
	}
}

func (uc *RevokeAddressBookShareUseCase) Execute(ctx context.Context, requestingUserID, addressBookID uint, shareUUID string) error {
	// 1. Verify ownership
	ab, err := uc.addressBookRepo.GetByID(ctx, addressBookID)
	if err != nil || ab == nil {
		return fmt.Errorf("address book not found")
	}
	if ab.UserID != requestingUserID {
		return fmt.Errorf("permission denied")
	}

	// 2. Get share
	share, err := uc.shareRepo.GetByUUID(ctx, shareUUID)
	if err != nil || share == nil {
		return fmt.Errorf("share not found")
	}
	if share.AddressBookID != addressBookID {
		return fmt.Errorf("share not found")
	}

	// 3. Revoke
	return uc.shareRepo.Revoke(ctx, share.ID)
}
