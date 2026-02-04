package dto

// MoveContactRequest represents the request body for moving a contact
type MoveContactRequest struct {
	TargetAddressBookID string `json:"target_addressbook_id"`
}
