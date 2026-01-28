package auth

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
)

// SAMLServiceProvider wraps the crewjam/saml samlsp.Middleware
type SAMLServiceProvider struct {
	sp *samlsp.Middleware
}

// NewSAMLServiceProvider creates a new SAML Service Provider adapter
func NewSAMLServiceProvider(
	entityID string,
	rootURL string,
	idpMetadataURL string,
	spKeyPEM []byte,
	spCertPEM []byte,
) (*SAMLServiceProvider, error) {
	keyPair, err := tls.X509KeyPair(spCertPEM, spKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load SP key pair: %w", err)
	}
	keyPair.Leaf, err = x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse SP certificate: %w", err)
	}

	idpMetadata, err := samlsp.FetchMetadata(context.Background(), http.DefaultClient, *mustParseURL(idpMetadataURL))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch IDP metadata: %w", err)
	}

	root, err := url.Parse(rootURL)
	if err != nil {
		return nil, fmt.Errorf("invalid root URL: %w", err)
	}

	sp, err := samlsp.New(samlsp.Options{
		URL:               *root,
		Key:               keyPair.PrivateKey.(*rsa.PrivateKey),
		Certificate:       keyPair.Leaf,
		IDPMetadata:       idpMetadata,
		EntityID:          entityID,
		AllowIDPInitiated: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create SAML SP: %w", err)
	}

	return &SAMLServiceProvider{sp: sp}, nil
}

// GetMetadata returns the SP metadata XML
func (s *SAMLServiceProvider) GetMetadata() ([]byte, error) {
	// Metadata() returns *saml.EntityDescriptor
	meta := s.sp.ServiceProvider.Metadata()
	return xml.Marshal(meta)
}

// LoginURL returns the IDP login URL (AuthnRequest)
func (s *SAMLServiceProvider) LoginURL() (string, error) {
	// MakeAuthenticationRequest(idpURL, binding, relayState)
	// We use HTTPRedirectBinding.
	// RelayState can be empty or used for redirect after login.
	// We need to resolve the IDP URL for redirect binding.
	bindingLocation := s.sp.ServiceProvider.GetSSOBindingLocation(saml.HTTPRedirectBinding)
	if bindingLocation == "" {
		return "", fmt.Errorf("IDP does not support HTTP-Redirect binding")
	}

	authReq, err := s.sp.ServiceProvider.MakeAuthenticationRequest(bindingLocation, saml.HTTPRedirectBinding, "")
	if err != nil {
		return "", err
	}

	u, err := authReq.Redirect(bindingLocation, &s.sp.ServiceProvider)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// Ensure strict adherence to the library usage.
func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

// Middleware exposes the underlying middleware if we want to use it directly
func (s *SAMLServiceProvider) Middleware() *samlsp.Middleware {
	return s.sp
}
