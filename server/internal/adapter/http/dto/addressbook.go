package dto

// CreateAddressBookRequest represents the request body for creating an address book
type CreateAddressBookRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdateAddressBookRequest represents the request body for updating an address book
type UpdateAddressBookRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// DeleteAddressBookRequest represents the request body for deleting an address book
type DeleteAddressBookRequest struct {
	Confirmation string `json:"confirmation"`
}

// CreateContactRequest represents the request body for creating a contact
type CreateContactRequest struct {
	VCardData string `json:"vcard_data" example:"BEGIN:VCARD..."`
}
