package sharing

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
)

type ListAddressBookSharesOutput struct {
	Shares []AddressBookShareInfo `json:"shares"`
}

type AddressBookShareInfo struct {
	ID         string   `json:"id"`
	SharedWith UserInfo `json:"shared_with"`
	Permission string   `json:"permission"`
	CreatedAt  string   `json:"created_at"`
}

type ListAddressBookSharesUseCase struct {
	shareRepo       sharing.AddressBookShareRepository
	addressBookRepo addressbook.Repository
}

func NewListAddressBookSharesUseCase(
	shareRepo sharing.AddressBookShareRepository,
	addressBookRepo addressbook.Repository,
) *ListAddressBookSharesUseCase {
	return &ListAddressBookSharesUseCase{
		shareRepo:       shareRepo,
		addressBookRepo: addressBookRepo,
	}
}

func (uc *ListAddressBookSharesUseCase) Execute(ctx context.Context, requestingUserID, addressBookID uint) (*ListAddressBookSharesOutput, error) {
	// 1. Verify ownership
	ab, err := uc.addressBookRepo.GetByID(ctx, addressBookID)
	if err != nil || ab == nil {
		return nil, fmt.Errorf("address book not found")
	}
	if ab.UserID != requestingUserID {
		return nil, fmt.Errorf("permission denied")
	}

	// 2. Get shares
	shares, err := uc.shareRepo.ListByAddressBookID(ctx, addressBookID)
	if err != nil {
		return nil, err
	}

	// 3. Map to output
	output := &ListAddressBookSharesOutput{
		Shares: make([]AddressBookShareInfo, 0, len(shares)),
	}
	for _, s := range shares {
		output.Shares = append(output.Shares, AddressBookShareInfo{
			ID: s.UUID,
			SharedWith: UserInfo{
				ID:          s.SharedWith.UUID,
				Username:    s.SharedWith.Username,
				DisplayName: s.SharedWith.DisplayName,
				Email:       s.SharedWith.Email,
			},
			Permission: s.Permission,
			CreatedAt:  s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	return output, nil
}
