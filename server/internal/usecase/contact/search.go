package contact

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/contact"
	addressbookuc "github.com/jherrma/caldav-server/internal/usecase/addressbook"
)

// ErrAddressBookNotFound is returned when the optional address-book filter names
// a book the caller cannot read — either it doesn't exist or it was never shared
// with them. Both collapse to one error on purpose so the response can't be used
// to probe which books exist; the handler turns it into a 404.
var ErrAddressBookNotFound = errors.New("address book not found")

// defaultSearchLimit applies when the caller doesn't ask for a page size.
const defaultSearchLimit = 20

type SearchUseCase struct {
	repo addressbook.Repository
	// abList resolves the caller's readable address books — owned plus shared
	// (#53). Reusing it instead of re-deriving that set here is what makes the
	// search corpus identical to the one the sidebar browses, which is the point
	// of #162: a contact must not vanish when you search for it.
	abList *addressbookuc.ListUseCase
}

func NewSearchUseCase(repo addressbook.Repository, abList *addressbookuc.ListUseCase) *SearchUseCase {
	return &SearchUseCase{repo: repo, abList: abList}
}

type SearchInput struct {
	UserID uint
	Query  string
	// AddressBookUUID optionally narrows the search to one book (#52 — the
	// external identifier is the UUID, not the numeric id). Empty means every
	// readable book. Because it can only narrow the resolved set and never widen
	// it, it needs no permission check of its own: a UUID that isn't in the set
	// is, from the caller's perspective, not found.
	AddressBookUUID string
	Limit           int
}

type SearchOutput struct {
	Contacts []*contact.Contact `json:"contacts"`
	Query    string             `json:"query"`
	Count    int                `json:"count"`
}

func (uc *SearchUseCase) Execute(ctx context.Context, input SearchInput) (*SearchOutput, error) {
	if input.Limit <= 0 {
		input.Limit = defaultSearchLimit
	}

	// Always a slice, never nil, so an empty result serialises as [] rather than
	// null and a client can't confuse "no matches" with "field missing".
	out := &SearchOutput{Contacts: []*contact.Contact{}, Query: input.Query}

	books, err := uc.abList.Execute(ctx, input.UserID)
	if err != nil {
		return nil, err
	}

	ids := make([]uint, 0, len(books))
	for _, ab := range books {
		if input.AddressBookUUID != "" && ab.UUID != input.AddressBookUUID {
			continue
		}
		ids = append(ids, ab.ID)
	}

	// A filter that matched no readable book is an error, not an empty corpus.
	// Dropping it silently would search every book instead — the caller asked
	// about one book and would be answered about others.
	if input.AddressBookUUID != "" && len(ids) == 0 {
		return nil, ErrAddressBookNotFound
	}
	if len(ids) == 0 {
		return out, nil
	}

	objs, err := uc.repo.SearchObjectsInBooks(ctx, ids, input.Query, input.Limit, 0)
	if err != nil {
		return nil, err
	}

	for i := range objs {
		if c := FromAddressObject(&objs[i]); c != nil {
			out.Contacts = append(out.Contacts, c)
		}
	}
	out.Count = len(out.Contacts)
	return out, nil
}
