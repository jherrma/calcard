package contact

import (
	"context"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/contact"
)

type SearchUseCase struct {
	repo addressbook.Repository
}

func NewSearchUseCase(repo addressbook.Repository) *SearchUseCase {
	return &SearchUseCase{repo: repo}
}

type SearchInput struct {
	UserID        uint
	Query         string
	AddressBookID *uint
	Limit         int
}

type SearchOutput struct {
	Contacts []*contact.Contact `json:"contacts"`
	Query    string             `json:"query"`
	Count    int                `json:"count"`
}

func (uc *SearchUseCase) Execute(ctx context.Context, input SearchInput) (*SearchOutput, error) {
	if input.Limit <= 0 {
		input.Limit = 20
	}

	// Returns contacts from all accessible address books or filtered by specific ID
	objs, err := uc.repo.SearchObjects(ctx, input.UserID, input.Query, input.AddressBookID, input.Limit)
	if err != nil {
		return nil, err
	}

	var contacts []*contact.Contact
	for _, obj := range objs {
		contacts = append(contacts, FromAddressObject(&obj))
	}

	return &SearchOutput{
		Contacts: contacts,
		Query:    input.Query,
		Count:    len(contacts),
	}, nil
}
