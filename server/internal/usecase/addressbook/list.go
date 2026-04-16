package addressbook

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
)

// AddressBookOwner is the minimal owner info returned on shared address
// books so the frontend can render "Shared by <Alice>".
type AddressBookOwner struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// AddressBookListItem is what the REST list returns. Owned books appear
// with Shared=false and Owner=nil; shared books carry the sharer's
// identifier so the UI can distinguish them without an extra lookup.
type AddressBookListItem struct {
	*addressbook.AddressBook
	Shared bool              `json:"shared"`
	Owner  *AddressBookOwner `json:"owner,omitempty"`
}

type ListUseCase struct {
	repo      addressbook.Repository
	shareRepo sharing.AddressBookShareRepository
}

// NewListUseCase wires the list usecase. shareRepo may be nil in unit
// tests that don't exercise sharing — in that case only owned books
// come back (the loop below short-circuits).
func NewListUseCase(repo addressbook.Repository, shareRepo sharing.AddressBookShareRepository) *ListUseCase {
	return &ListUseCase{repo: repo, shareRepo: shareRepo}
}

// Execute returns owned + shared address books for the user. Mirrors
// the shape used by calendar.ListCalendarsUseCase so the frontend can
// treat both collection types the same way.
func (uc *ListUseCase) Execute(ctx context.Context, userID uint) ([]*AddressBookListItem, error) {
	owned, err := uc.repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := make([]*AddressBookListItem, 0, len(owned))
	for i := range owned {
		ab := owned[i]
		result = append(result, &AddressBookListItem{
			AddressBook: &ab,
			Shared:      false,
		})
	}

	if uc.shareRepo == nil {
		return result, nil
	}

	shares, err := uc.shareRepo.FindAddressBooksSharedWithUser(ctx, userID)
	if err != nil {
		// A failure fetching shares shouldn't blank out the owned
		// list — just return what we have and let the caller decide.
		return result, nil
	}

	for _, s := range shares {
		ab := s.AddressBook
		result = append(result, &AddressBookListItem{
			AddressBook: &ab,
			Shared:      true,
			Owner: &AddressBookOwner{
				ID:          ab.User.UUID,
				DisplayName: ab.User.DisplayName,
			},
		})
	}

	return result, nil
}
