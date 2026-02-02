package contact

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/contact"
)

type ListUseCase struct {
	repo addressbook.Repository
}

func NewListUseCase(repo addressbook.Repository) *ListUseCase {
	return &ListUseCase{repo: repo}
}

type ListInput struct {
	AddressBookID uint
	Limit         int
	Offset        int
	Sort          string // "name", "email", "updated_at"
	Order         string // "asc", "desc"
}

type ListOutput struct {
	Contacts []*contact.Contact
	Total    int
	Limit    int
	Offset   int
}

func (uc *ListUseCase) Execute(ctx context.Context, input ListInput) (*ListOutput, error) {
	// Defaults
	if input.Sort == "" {
		input.Sort = "name"
	}
	if input.Order == "" {
		input.Order = "asc"
	}
	if input.Limit <= 0 {
		input.Limit = 50
	}

	// 1. Fetch from repo with pagination
	objs, total, err := uc.repo.ListObjects(ctx, input.AddressBookID, input.Limit, input.Offset, input.Sort, input.Order)
	if err != nil {
		return nil, err
	}

	// 2. Map to lightweight contacts
	var contactsList []*contact.Contact
	for _, obj := range objs {
		contactsList = append(contactsList, FromAddressObject(&obj))
	}

	return &ListOutput{
		Contacts: contactsList,
		Total:    int(total),
		Limit:    input.Limit,
		Offset:   input.Offset,
	}, nil
}
