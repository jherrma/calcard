//go:build integration

package integration_test

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContactPhoto uploads a JPEG photo for a contact, fetches it back, then
// replaces it with a second image, and finally deletes it. The JPEG bytes are
// loaded from files on disk so we're really exercising the multipart/photo
// pipeline (content-type sniffing, byte round-trip, delete path) rather than
// some hand-rolled payload.
func TestContactPhoto(t *testing.T) {
	email := "photo@example.test"
	password := "photoSecret!123"
	token := registerAndLogin(t, email, password, "Photo User")

	abID := createAddressBook(t, token, "Photo Book")

	// Create a contact we can attach the photo to.
	var contact struct {
		ID  string `json:"id"`
		UID string `json:"uid"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/"+uintStr(abID)+"/contacts", token,
		map[string]any{
			"formatted_name": "Alice Avatar",
			"given_name":     "Alice",
			"family_name":    "Avatar",
		}, &contact)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, contact.ID)

	// Load the first profile icon from disk.
	icon1 := readAsset(t, "user-icon.jpg")
	require.Greater(t, len(icon1), 1000, "asset appears truncated")
	assert.Equal(t, []byte{0xFF, 0xD8, 0xFF}, icon1[:3], "user-icon.jpg should be a JPEG")

	// --- Upload ----------------------------------------------------------

	photoURL := "/api/v1/addressbooks/" + uintStr(abID) + "/contacts/" + contact.ID + "/photo"
	status, raw := rawCall(t, http.MethodPut, baseURL+photoURL, token, icon1, map[string]string{
		"Content-Type": "image/jpeg",
	})
	require.Equalf(t, http.StatusNoContent, status, "PUT photo: %s", errorMessage(raw))

	// --- Fetch (unauthenticated GET isn't allowed; use the user's token) -

	status, hdrs, body := rawCall2(t, http.MethodGet, baseURL+photoURL, token, nil, nil)
	require.Equalf(t, http.StatusOK, status, "GET photo: %s", string(body))
	assert.Equal(t, "image/jpeg", hdrs.Get("Content-Type"), "content-type should reflect photo format")
	assert.Equal(t, []byte{0xFF, 0xD8, 0xFF}, body[:3], "fetched bytes should still look like a JPEG")

	// --- Replace with icon 2 ---------------------------------------------

	icon2 := readAsset(t, "user-icon-2.jpg")
	require.NotEqual(t, icon1, icon2, "the two asset files should differ")

	status, raw = rawCall(t, http.MethodPut, baseURL+photoURL, token, icon2, map[string]string{
		"Content-Type": "image/jpeg",
	})
	require.Equalf(t, http.StatusNoContent, status, "PUT replacement photo: %s", errorMessage(raw))

	status, _, body = rawCall2(t, http.MethodGet, baseURL+photoURL, token, nil, nil)
	require.Equal(t, http.StatusOK, status)
	// Bytes after re-upload must match the icon-2 file we sent in — assert the
	// full bytes, not just the length. A stale-serving bug that returned the
	// ORIGINAL image would pass a length check whenever the two files happened
	// to be the same size. (Also proves the base64 decode path round-trips.)
	assert.Equal(t, icon2, body, "fetched photo bytes must equal the replacement icon 2")

	// --- Delete ----------------------------------------------------------

	status, raw = restCall(t, http.MethodDelete, photoURL[len("/api/v1"):], token, nil)
	require.Equalf(t, http.StatusNoContent, status, "DELETE photo: %s", errorMessage(raw))

	// After delete, GET should 404 and the contact itself (re-fetched via
	// single-contact GET) should have its photo URL stripped.
	status, _, _ = rawCall2(t, http.MethodGet, baseURL+photoURL, token, nil, nil)
	assert.Equal(t, http.StatusNotFound, status, "deleted photo must 404")

	var after struct {
		PhotoURL string `json:"photo_url"`
	}
	code = doJSONRaw(t, http.MethodGet,
		"/addressbooks/"+uintStr(abID)+"/contacts/"+contact.ID, token, nil, &after)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, after.PhotoURL, "contact must not advertise a photo_url once the photo is gone")
}

// readAsset loads a fixture from server/integration/Assets.
func readAsset(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("Assets", name))
	require.NoErrorf(t, err, "read asset %s", name)
	return data
}

// rawCall2 is a bearer-auth GET that also exposes the response headers.
// The existing `rawCall` helper drops headers; here we need Content-Type of
// the fetched photo so we can assert the server restores it.
func rawCall2(t *testing.T, method, fullURL, bearerToken string, body any, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	_ = body // no body for the GETs we make here
	req, err := http.NewRequest(method, fullURL, nil)
	require.NoError(t, err)
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, respBody
}

// TestCardDAVPhotoRoundTrip is the regression test for H5: photos must survive
// a CardDAV GET → PUT round-trip (previously the GET served a photo-stripped
// vCard, and re-PUTting it deleted the stored photo). Also checks Content-Length
// matches the served body.
func TestCardDAVPhotoRoundTrip(t *testing.T) {
	email := "photo-rt@example.test"
	token, username := registerAndLoginFull(t, email, "photoSecret!123", "Photo RT")
	_, appPass := createAppPassword(t, token, "photo-rt")

	abPath := addressBookPath(t, token, "Contacts")
	require.NotEmpty(t, abPath)
	collection := "/dav/" + username + "/addressbooks/" + abPath + "/"

	icon := readAsset(t, "user-icon.jpg")
	b64 := base64.StdEncoding.EncodeToString(icon)
	uid := "photo-rt-uid"
	path := collection + uid + ".vcf"
	vcardWithPhoto := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:Photo Person\r\nN:Person;Photo;;;\r\n" +
		"PHOTO;ENCODING=b;TYPE=JPEG:" + b64 + "\r\nEND:VCARD\r\n"

	// PUT the contact with an embedded photo.
	status, _, body := davCall(t, "PUT", path, email, appPass, vcardWithPhoto,
		map[string]string{"Content-Type": "text/vcard; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent, http.StatusOK}, status, "PUT: %s", string(body))

	// GET it back: the body must include the PHOTO, and Content-Length must
	// match the actual body length.
	status, hdrs, getBody := davCall(t, "GET", path, email, appPass, "", nil)
	require.Equalf(t, http.StatusOK, status, "GET: %s", string(getBody))
	assert.Equal(t, 1, countPhotoProps(string(getBody)),
		"served vCard must include exactly one PHOTO property (a strip/re-inject asymmetry would duplicate it)")
	cl := hdrs.Get("Content-Length")
	require.NotEmpty(t, cl, "GET response must set a Content-Length header")
	assert.Equalf(t, len(getBody), mustAtoi(t, cl),
		"Content-Length (%s) must match body length (%d)", cl, len(getBody))

	// Re-PUT the exact body we got back (what a syncing client does).
	status, _, body = davCall(t, "PUT", path, email, appPass, string(getBody),
		map[string]string{"Content-Type": "text/vcard; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent, http.StatusOK}, status, "re-PUT: %s", string(body))

	// The photo must still be served via REST (proves it wasn't deleted).
	abID := addressBookID(t, token, "Contacts")
	var listResp struct {
		Contacts []struct {
			ID string `json:"id"`
		} `json:"Contacts"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet,
		"/addressbooks/"+uintStr(abID)+"/contacts", token, nil, &listResp))
	require.NotEmpty(t, listResp.Contacts)
	contactID := listResp.Contacts[0].ID

	// Single-contact GET advertises a photo_url when a photo is present.
	var single struct {
		PhotoURL string `json:"photo_url"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet,
		fmt.Sprintf("/addressbooks/%d/contacts/%s", abID, contactID), token, nil, &single))
	require.NotEmptyf(t, single.PhotoURL, "photo must survive the GET→PUT round-trip")

	// And the photo endpoint returns the JPEG bytes.
	status, photoBytes := rawCall(t, http.MethodGet, baseURL+"/api/v1/addressbooks/"+uintStr(abID)+"/contacts/"+contactID+"/photo", token, nil, nil)
	require.Equal(t, http.StatusOK, status)
	require.GreaterOrEqual(t, len(photoBytes), 3)
	assert.Equal(t, []byte{0xFF, 0xD8, 0xFF}, photoBytes[:3], "round-tripped photo must still be a JPEG")
}

// countPhotoProps counts PHOTO property occurrences in a serialized vCard by
// their delimiter (";" for parameters, ":" for a bare value) so a base64 photo
// value that happens to contain the letters "PHOTO" can't inflate the count.
func countPhotoProps(vcardBody string) int {
	return strings.Count(vcardBody, "PHOTO;") + strings.Count(vcardBody, "PHOTO:")
}

// TestContactPhotoStrippedOnMove is the regression test for the contact-move
// photo-duplication bug: MoveUseCase loads the object via GetObjectByUUID (which
// hydrates the PHOTO back into the body), so if the move re-saved that body
// verbatim the photo would live inline AND in the side table, and every later
// read would inject a second copy. After a REST move, the DAV-served vCard must
// carry exactly one PHOTO.
func TestContactPhotoStrippedOnMove(t *testing.T) {
	email := "photo-move@example.test"
	token, username := registerAndLoginFull(t, email, "photoSecret!123", "Photo Move")
	_, appPass := createAppPassword(t, token, "photo-move")

	srcPath := addressBookPath(t, token, "Contacts")
	require.NotEmpty(t, srcPath)
	srcID := addressBookID(t, token, "Contacts")
	dstID := createAddressBook(t, token, "Archive")
	dstPath := addressBookPath(t, token, "Archive")
	require.NotEmpty(t, dstPath)

	collection := "/dav/" + username + "/addressbooks/" + srcPath + "/"
	icon := readAsset(t, "user-icon.jpg")
	b64 := base64.StdEncoding.EncodeToString(icon)
	uid := "photo-move-uid"
	vcardWithPhoto := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:Move Me\r\nN:Me;Move;;;\r\n" +
		"PHOTO;ENCODING=b;TYPE=JPEG:" + b64 + "\r\nEND:VCARD\r\n"

	status, _, body := davCall(t, "PUT", collection+uid+".vcf", email, appPass, vcardWithPhoto,
		map[string]string{"Content-Type": "text/vcard; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent, http.StatusOK}, status, "PUT: %s", string(body))

	// Resolve the contact's REST id within the source book.
	var listResp struct {
		Contacts []struct {
			ID  string `json:"id"`
			UID string `json:"uid"`
		} `json:"Contacts"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet,
		"/addressbooks/"+uintStr(srcID)+"/contacts", token, nil, &listResp))
	var contactID string
	for _, c := range listResp.Contacts {
		if c.UID == uid {
			contactID = c.ID
		}
	}
	require.NotEmpty(t, contactID, "seeded contact must appear in the source book")

	// Move it to the target book via the dedicated REST move route.
	movePath := "/addressbooks/" + uintStr(srcID) + "/contacts/" + contactID + "/move"
	var moved struct {
		UID string `json:"uid"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodPost, movePath, token,
		map[string]string{"target_addressbook_id": uintStr(dstID)}, &moved))

	// DAV-GET the moved object from the TARGET collection: exactly one PHOTO.
	dstObj := "/dav/" + username + "/addressbooks/" + dstPath + "/" + uid + ".vcf"
	status, _, served := davCall(t, "GET", dstObj, email, appPass, "", nil)
	require.Equalf(t, http.StatusOK, status, "GET moved vCard: %s", string(served))
	assert.Equal(t, 1, countPhotoProps(string(served)),
		"moved contact must carry exactly one PHOTO property, not a duplicate")
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	require.NoError(t, err)
	return n
}

// addressBookID returns the numeric ID of a named address book.
func addressBookID(t *testing.T, token, name string) uint {
	t.Helper()
	var wrap struct {
		AddressBooks []struct {
			ID   uint   `json:"ID"`
			Name string `json:"Name"`
		} `json:"addressbooks"`
	}
	require.Equal(t, http.StatusOK, doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &wrap))
	for _, ab := range wrap.AddressBooks {
		if ab.Name == name {
			return ab.ID
		}
	}
	return 0
}
