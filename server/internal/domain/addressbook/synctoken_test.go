package addressbook

import (
	"strings"
	"testing"
)

// TestGenerateSyncTokenIsURI confirms the addressbook sync token stays a URI.
func TestGenerateSyncTokenIsURI(t *testing.T) {
	tok := GenerateSyncToken()
	if !strings.HasPrefix(tok, "data:,") {
		t.Fatalf("sync token must be a data: URI, got %q", tok)
	}
}

// TestNewETagHasNoDataURIPrefix is the regression test for #50: contact ETags
// used to alias the sync-token format and literally read "data:,...".
func TestNewETagHasNoDataURIPrefix(t *testing.T) {
	etag := NewETag()
	if strings.Contains(etag, "data:,") {
		t.Fatalf("ETag must not contain the sync-token 'data:,' prefix, got %q", etag)
	}
	if strings.ContainsAny(etag, ",") {
		t.Fatalf("ETag must not contain a comma, got %q", etag)
	}
}
