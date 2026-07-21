package calendar

import (
	"strings"
	"testing"
)

// TestGenerateSyncTokenIsURI is part of the regression coverage for #50:
// RFC 6578 §6.4 requires the sync token to be a URI.
func TestGenerateSyncTokenIsURI(t *testing.T) {
	tok := GenerateSyncToken()
	if !strings.HasPrefix(tok, "data:,") {
		t.Fatalf("sync token must be a data: URI, got %q", tok)
	}
}

// TestNewETagHasNoDataURIPrefix guards the other half of #50: ETags must not
// carry the sync token's "data:," prefix (a comma is hostile in If-Match).
func TestNewETagHasNoDataURIPrefix(t *testing.T) {
	etag := NewETag()
	if strings.Contains(etag, "data:,") {
		t.Fatalf("ETag must not contain the sync-token 'data:,' prefix, got %q", etag)
	}
	if strings.ContainsAny(etag, ",") {
		t.Fatalf("ETag must not contain a comma, got %q", etag)
	}
}

// TestNewETagUnique sanity-checks that ETags vary between calls.
func TestNewETagUnique(t *testing.T) {
	if NewETag() == NewETag() {
		t.Fatal("consecutive ETags should differ")
	}
}
