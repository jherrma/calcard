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
	"github.com/jherrma/caldav-server/internal/config"
)

// SAMLServiceProvider wraps the crewjam/saml middleware
type SAMLServiceProvider struct {
	middleware *samlsp.Middleware
}

// NewSAMLServiceProvider creates a new SAML service provider from config.
// Returns nil if SAML is not configured.
func NewSAMLServiceProvider(cfg *config.SAMLConfig, baseURL string) (*SAMLServiceProvider, error) {
	// Check if SAML is configured (following OAuth pattern)
	if cfg.EntityID == "" || cfg.IDPMetadataURL == "" {
		return nil, nil // SAML not configured, return nil without error
	}

	rootURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL: %w", err)
	}

	// Load SP key pair
	var keyPair tls.Certificate
	if cfg.SPKey != "" && cfg.SPCert != "" {
		keyPair, err = tls.X509KeyPair([]byte(cfg.SPCert), []byte(cfg.SPKey))
		if err != nil {
			return nil, fmt.Errorf("failed to load SP key pair: %w", err)
		}
		keyPair.Leaf, err = x509.ParseCertificate(keyPair.Certificate[0])
		if err != nil {
			return nil, fmt.Errorf("failed to parse SP certificate: %w", err)
		}
	}

	// Fetch IDP metadata
	idpMetadataURL, err := url.Parse(cfg.IDPMetadataURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse IDP metadata URL: %w", err)
	}

	idpMetadata, err := samlsp.FetchMetadata(context.Background(), http.DefaultClient, *idpMetadataURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch IDP metadata: %w", err)
	}

	opts := samlsp.Options{
		EntityID:    cfg.EntityID,
		URL:         *rootURL,
		IDPMetadata: idpMetadata,
	}

	// Set key if provided
	if keyPair.PrivateKey != nil {
		opts.Key = keyPair.PrivateKey.(*rsa.PrivateKey)
		opts.Certificate = keyPair.Leaf
	}

	// Configure signing preferences
	opts.SignRequest = cfg.SignRequests

	middleware, err := samlsp.New(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create SAML middleware: %w", err)
	}

	return &SAMLServiceProvider{middleware: middleware}, nil
}

// LoginURL returns the URL to redirect the user to for SAML login
func (sp *SAMLServiceProvider) LoginURL() (string, error) {
	if sp.middleware == nil {
		return "", fmt.Errorf("SAML service provider not initialized")
	}
	// Build the AuthnRequest URL
	return sp.middleware.ServiceProvider.GetSSOBindingLocation(saml.HTTPRedirectBinding), nil
}

// GetMetadata returns the SP metadata as XML bytes
func (sp *SAMLServiceProvider) GetMetadata() ([]byte, error) {
	if sp.middleware == nil {
		return nil, fmt.Errorf("SAML service provider not initialized")
	}
	meta := sp.middleware.ServiceProvider.Metadata()
	return xml.MarshalIndent(meta, "", "  ")
}

// Middleware returns the underlying SAML middleware for ACS parsing
func (sp *SAMLServiceProvider) Middleware() *samlsp.Middleware {
	return sp.middleware
}

// ParseAssertion parses a SAML response and returns the assertion
func (sp *SAMLServiceProvider) ParseAssertion(r *http.Request, possibleRequestIDs []string) (*saml.Assertion, error) {
	if sp.middleware == nil {
		return nil, fmt.Errorf("SAML service provider not initialized")
	}
	return sp.middleware.ServiceProvider.ParseResponse(r, possibleRequestIDs)
}
