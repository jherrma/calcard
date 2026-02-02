package auth

import (
	adapter "github.com/jherrma/caldav-server/internal/adapter/auth"
)

// SAMLMetadataUseCase handles retrieval of SP metadata
type SAMLMetadataUseCase struct {
	sp *adapter.SAMLServiceProvider
}

// NewSAMLMetadataUseCase creates a new SAML metadata use case
func NewSAMLMetadataUseCase(sp *adapter.SAMLServiceProvider) *SAMLMetadataUseCase {
	return &SAMLMetadataUseCase{sp: sp}
}

// Execute returns the SP metadata XML
func (uc *SAMLMetadataUseCase) Execute() ([]byte, error) {
	return uc.sp.GetMetadata()
}
