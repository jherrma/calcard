//go:build integration

package integration_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExportImportRoundtrip seeds calendars+events and address books+contacts,
// invokes GET /users/me/export to download the ZIP, deletes the original data,
// creates fresh empty collections, re-imports each .ics/.vcf from the ZIP,
// and asserts that every seeded UID (events and contacts) is present again.
//
// This exercises: the backup export use case, calendar .ics import, contact
// .vcf import, and confirms that the export format is actually re-importable
// without loss — which is the only guarantee that matters for "backup".
func TestExportImportRoundtrip(t *testing.T) {
	email := "roundtrip@example.test"
	password := "roundtripSecret!123"
	token := registerAndLogin(t, email, password, "Roundtrip User")

	// --- Seed data ----------------------------------------------------------
	// Two calendars, three events each. We capture the (calendar name → set of
	// event UIDs) map so we can assert it comes back intact after re-import.

	seededEvents := map[string]map[string]string{} // calName -> UID -> summary
	for _, calName := range []string{"Trip Cal A", "Trip Cal B"} {
		calID, _ := createCalendar(t, token, calName, "#112233")
		seededEvents[calName] = map[string]string{}
		for i := 0; i < 3; i++ {
			uid, summary := createSeededEvent(t, token, calID, calName, i)
			seededEvents[calName][uid] = summary
		}
	}

	seededContacts := map[string]map[string]string{} // abName -> UID -> FN
	for _, abName := range []string{"Trip AB A", "Trip AB B"} {
		abID := createAddressBook(t, token, abName)
		seededContacts[abName] = map[string]string{}
		for i := 0; i < 3; i++ {
			uid, fn := createSeededContact(t, token, abID, abName, i)
			seededContacts[abName][uid] = fn
		}
	}

	// --- Export -------------------------------------------------------------

	zipBytes := exportBackup(t, token)
	archive, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	require.NoError(t, err, "unzip export")

	metadata := readExportMetadata(t, archive)
	assertCountsMatch(t, metadata, seededEvents, seededContacts)

	// Collect the actual .ics / .vcf payloads keyed by their original collection
	// name, limited to the collections we explicitly seeded. The user also has
	// a default "Personal" calendar and "Contacts" address book that are
	// exported too, but they're not part of what this roundtrip covers.
	icsByName := map[string][]byte{}
	vcfByName := map[string][]byte{}
	for _, f := range archive.File {
		data := readZipFile(t, f)
		switch {
		case strings.HasPrefix(f.Name, "calendars/") && strings.HasSuffix(f.Name, ".ics"):
			name := strings.TrimSuffix(strings.TrimPrefix(f.Name, "calendars/"), ".ics")
			if _, ok := seededEvents[name]; ok {
				icsByName[name] = data
			}
		case strings.HasPrefix(f.Name, "addressbooks/") && strings.HasSuffix(f.Name, ".vcf"):
			name := strings.TrimSuffix(strings.TrimPrefix(f.Name, "addressbooks/"), ".vcf")
			if _, ok := seededContacts[name]; ok {
				vcfByName[name] = data
			}
		}
	}
	require.Len(t, icsByName, len(seededEvents), "expected one .ics per seeded calendar")
	require.Len(t, vcfByName, len(seededContacts), "expected one .vcf per seeded address book")

	// --- Wipe the seeded collections ----------------------------------------
	// Each user always keeps its default "Personal" calendar and "Contacts"
	// address book (the last-one guard in the delete use case prevents us from
	// removing them). That's fine: we only need to wipe the seeded data.

	deleteCalendarByName(t, token, "Trip Cal A")
	deleteCalendarByName(t, token, "Trip Cal B")
	deleteAddressBookByName(t, token, "Trip AB A")
	deleteAddressBookByName(t, token, "Trip AB B")

	assert.Len(t, listEventsForSeededCalendars(t, token, seededEvents), 0,
		"after wipe, listing events through the seeded calendars should find nothing")

	// --- Re-import from the ZIP ---------------------------------------------

	for name, data := range icsByName {
		// New empty calendar with the old name
		calUUID, _ := createCalendarReturningUUID(t, token, name, "#445566")
		importCalendar(t, token, calUUID, data)
	}
	for name, data := range vcfByName {
		abID := createAddressBook(t, token, name)
		importAddressBook(t, token, abID, data)
	}

	// --- Verify: every seeded UID is present again --------------------------

	actualEvents := collectEventsByCalendarName(t, token, seededEvents)
	for calName, wantUIDs := range seededEvents {
		gotUIDs, ok := actualEvents[calName]
		require.True(t, ok, "calendar %q missing after re-import", calName)
		assert.ElementsMatch(t, keys(wantUIDs), keys(gotUIDs), "UIDs for calendar %q", calName)
		for uid, wantSummary := range wantUIDs {
			assert.Equal(t, wantSummary, gotUIDs[uid], "summary for event %s in %s", uid, calName)
		}
	}

	actualContacts := collectContactsByAddressBookName(t, token, seededContacts)
	for abName, wantUIDs := range seededContacts {
		gotUIDs, ok := actualContacts[abName]
		require.True(t, ok, "address book %q missing after re-import", abName)
		assert.ElementsMatch(t, keys(wantUIDs), keys(gotUIDs), "UIDs for address book %q", abName)
		for uid, wantFN := range wantUIDs {
			assert.Equal(t, wantFN, gotUIDs[uid], "FN for contact %s in %s", uid, abName)
		}
	}
}

// --- test-local helpers (shared with caldav/carddav tests too) --------------

// registerAndLogin registers a fresh user and returns a bearer token. It
// discards the register response; callers that also need the server-issued
// opaque username (used in DAV URLs) should use registerAndLoginFull.
func registerAndLogin(t *testing.T, email, password, displayName string) string {
	t.Helper()
	tok, _ := registerAndLoginFull(t, email, password, displayName)
	return tok
}

// registerAndLoginFull is like registerAndLogin but also returns the random
// 16-character username the server assigns at registration. That username is
// what appears in CalDAV/CardDAV paths (/dav/{username}/calendars/...), but
// is NOT what Basic Auth should use — the DAV auth middleware looks up users
// by email only, so Basic Auth credentials are (email, app_password).
func registerAndLoginFull(t *testing.T, email, password, displayName string) (token, username string) {
	t.Helper()
	var reg struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		Username string `json:"username"`
	}
	code := doJSON(t, http.MethodPost, "/auth/register", "", map[string]string{
		"email":        email,
		"password":     password,
		"display_name": displayName,
	}, &reg)
	require.Equal(t, http.StatusOK, code, "register %s", email)
	require.NotEmpty(t, reg.Username, "register response should include username")

	var login struct {
		AccessToken string `json:"access_token"`
	}
	code = doJSON(t, http.MethodPost, "/auth/login", "", map[string]string{
		"email":    email,
		"password": password,
	}, &login)
	require.Equal(t, http.StatusOK, code, "login %s", email)
	require.NotEmpty(t, login.AccessToken)
	return login.AccessToken, reg.Username
}

func createCalendar(t *testing.T, token, name, color string) (id uint, uuid string) {
	t.Helper()
	var cal struct {
		ID   uint   `json:"id"`
		UUID string `json:"uuid"`
	}
	code := doJSONRaw(t, http.MethodPost, "/calendars/", token, map[string]string{
		"name": name, "color": color,
	}, &cal)
	require.Equal(t, http.StatusCreated, code, "create calendar %s", name)
	require.NotZero(t, cal.ID)
	require.NotEmpty(t, cal.UUID)
	return cal.ID, cal.UUID
}

func createCalendarReturningUUID(t *testing.T, token, name, color string) (uuid string, id uint) {
	t.Helper()
	id, uuid = createCalendar(t, token, name, color)
	return uuid, id
}

func createAddressBook(t *testing.T, token, name string) uint {
	t.Helper()
	var ab struct {
		ID uint `json:"ID"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/", token, map[string]string{
		"name": name,
	}, &ab)
	require.Equal(t, http.StatusCreated, code, "create addressbook %s", name)
	require.NotZero(t, ab.ID)
	return ab.ID
}

// createSeededEvent makes an event with a deterministic summary and returns
// (uid, summary).
func createSeededEvent(t *testing.T, token string, calendarID uint, calName string, idx int) (uid, summary string) {
	t.Helper()
	summary = fmt.Sprintf("%s event %d", calName, idx)
	start := time.Date(2030, 6, 1+idx, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	body := map[string]any{
		"summary":  summary,
		"start":    start.Format(time.RFC3339),
		"end":      end.Format(time.RFC3339),
		"timezone": "UTC",
		"all_day":  false,
	}
	var ev struct {
		UID     string `json:"uid"`
		Summary string `json:"summary"`
	}
	path := "/calendars/" + uintStr(calendarID) + "/events/"
	code := doJSONRaw(t, http.MethodPost, path, token, body, &ev)
	require.Equal(t, http.StatusCreated, code, "create event in %s", calName)
	require.NotEmpty(t, ev.UID)
	return ev.UID, summary
}

// createSeededContact creates a vCard contact and returns (uid, formattedName).
// We post a minimal JSON shape that ContactHandler.Create accepts, then read
// the resulting UID from the response.
func createSeededContact(t *testing.T, token string, addressBookID uint, abName string, idx int) (uid, fn string) {
	t.Helper()
	fn = fmt.Sprintf("%s contact %d", abName, idx)
	body := map[string]any{
		"formatted_name": fn,
		"given_name":     fmt.Sprintf("Given%d", idx),
		"family_name":    fmt.Sprintf("Family%d", idx),
	}
	var ct struct {
		UID           string `json:"uid"`
		FormattedName string `json:"formatted_name"`
	}
	path := "/addressbooks/" + uintStr(addressBookID) + "/contacts"
	code := doJSONRaw(t, http.MethodPost, path, token, body, &ct)
	require.Equal(t, http.StatusCreated, code, "create contact in %s", abName)
	require.NotEmpty(t, ct.UID)
	return ct.UID, ct.FormattedName
}

// exportBackup hits /users/me/export and returns the raw ZIP bytes.
func exportBackup(t *testing.T, token string) []byte {
	t.Helper()
	status, raw := restCall(t, http.MethodGet, "/users/me/export", token, nil)
	require.Equal(t, http.StatusOK, status, "backup export: %s", errorMessage(raw))
	require.NotEmpty(t, raw)
	return raw
}

func readExportMetadata(t *testing.T, archive *zip.Reader) map[string]any {
	t.Helper()
	for _, f := range archive.File {
		if f.Name != "metadata.json" {
			continue
		}
		data := readZipFile(t, f)
		var out map[string]any
		require.NoError(t, json.Unmarshal(data, &out), "parse metadata.json")
		return out
	}
	t.Fatalf("metadata.json missing from export")
	return nil
}

func readZipFile(t *testing.T, f *zip.File) []byte {
	t.Helper()
	rc, err := f.Open()
	require.NoError(t, err, "open zip entry %s", f.Name)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	require.NoError(t, err, "read zip entry %s", f.Name)
	return data
}

func assertCountsMatch(t *testing.T, meta map[string]any, events, contacts map[string]map[string]string) {
	t.Helper()
	calList, _ := meta["calendars"].([]any)
	byName := map[string]int{}
	for _, entry := range calList {
		e, _ := entry.(map[string]any)
		name, _ := e["name"].(string)
		count, _ := e["event_count"].(float64)
		byName[name] = int(count)
	}
	for calName, uids := range events {
		assert.Equalf(t, len(uids), byName[calName],
			"metadata event_count for %s", calName)
	}

	abList, _ := meta["addressbooks"].([]any)
	abByName := map[string]int{}
	for _, entry := range abList {
		e, _ := entry.(map[string]any)
		name, _ := e["name"].(string)
		count, _ := e["contact_count"].(float64)
		abByName[name] = int(count)
	}
	for abName, uids := range contacts {
		assert.Equalf(t, len(uids), abByName[abName],
			"metadata contact_count for %s", abName)
	}
}

// listCalendarsIndex returns {name -> (id, uuid)} for the logged-in user.
// Duplicate names win last (fine — our tests don't rely on stable ordering
// after re-import because we wipe before reimporting).
func listCalendarsIndex(t *testing.T, token string) map[string]struct {
	ID   uint
	UUID string
} {
	t.Helper()
	var wrap struct {
		Calendars []struct {
			ID   uint   `json:"id"`
			UUID string `json:"uuid"`
			Name string `json:"name"`
		} `json:"calendars"`
	}
	code := doJSONRaw(t, http.MethodGet, "/calendars/", token, nil, &wrap)
	require.Equal(t, http.StatusOK, code)
	out := map[string]struct {
		ID   uint
		UUID string
	}{}
	for _, c := range wrap.Calendars {
		out[c.Name] = struct {
			ID   uint
			UUID string
		}{c.ID, c.UUID}
	}
	return out
}

func listAddressBooksIndex(t *testing.T, token string) map[string]uint {
	t.Helper()
	// AddressBook fields have no JSON tags → PascalCase.
	var wrap struct {
		AddressBooks []struct {
			ID   uint   `json:"ID"`
			Name string `json:"Name"`
		} `json:"addressbooks"`
	}
	code := doJSONRaw(t, http.MethodGet, "/addressbooks/", token, nil, &wrap)
	require.Equal(t, http.StatusOK, code)
	out := map[string]uint{}
	for _, ab := range wrap.AddressBooks {
		out[ab.Name] = ab.ID
	}
	return out
}

func deleteCalendarByName(t *testing.T, token, name string) {
	t.Helper()
	idx := listCalendarsIndex(t, token)
	entry, ok := idx[name]
	require.True(t, ok, "calendar %q not found for delete", name)
	status, raw := restCall(t, http.MethodDelete, "/calendars/"+entry.UUID, token,
		map[string]string{"confirmation": "DELETE"})
	require.Equal(t, http.StatusNoContent, status, "delete calendar %s: %s", name, errorMessage(raw))
}

func deleteAddressBookByName(t *testing.T, token, name string) {
	t.Helper()
	idx := listAddressBooksIndex(t, token)
	id, ok := idx[name]
	require.True(t, ok, "addressbook %q not found for delete", name)
	status, raw := restCall(t, http.MethodDelete, "/addressbooks/"+uintStr(id), token,
		map[string]string{"confirmation": "DELETE"})
	require.Equal(t, http.StatusNoContent, status, "delete addressbook %s: %s", name, errorMessage(raw))
}

func importCalendar(t *testing.T, token, calendarUUID string, icsData []byte) {
	t.Helper()
	status, raw := rawCall(t, http.MethodPost, baseURL+"/api/v1/calendars/"+calendarUUID+"/import",
		token, icsData, map[string]string{"Content-Type": "text/calendar"})
	require.Equalf(t, http.StatusOK, status, "import calendar: %s", errorMessage(raw))

	// The import endpoint returns 200 even when some events fail to parse —
	// the body carries the per-event result. Fail loudly here: if any event
	// didn't import, the roundtrip assertion would misleadingly blame the
	// downstream comparison.
	var result struct {
		Total    int `json:"total"`
		Imported int `json:"imported"`
		Failed   int `json:"failed"`
	}
	require.NoError(t, json.Unmarshal(raw, &result), "decode import result")
	require.Equalf(t, 0, result.Failed, "calendar import had %d failures: %s", result.Failed, string(raw))
	require.Equal(t, result.Total, result.Imported, "all events should import")
}

func importAddressBook(t *testing.T, token string, abID uint, vcfData []byte) {
	t.Helper()
	status, raw := rawCall(t, http.MethodPost, baseURL+"/api/v1/addressbooks/"+uintStr(abID)+"/import",
		token, vcfData, map[string]string{"Content-Type": "text/vcard"})
	require.Equalf(t, http.StatusOK, status, "import addressbook: %s", errorMessage(raw))

	var result struct {
		Total    int `json:"total"`
		Imported int `json:"imported"`
		Failed   int `json:"failed"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equalf(t, 0, result.Failed, "contact import had %d failures: %s", result.Failed, string(raw))
	require.Equal(t, result.Total, result.Imported)
}

func listEventsForSeededCalendars(t *testing.T, token string, seeded map[string]map[string]string) map[string]string {
	t.Helper()
	_ = seeded // signature kept symmetric
	rangeQS := "?start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z&expand=false"
	all := map[string]string{}
	for _, entry := range listCalendarsIndex(t, token) {
		var resp struct {
			Events []struct {
				UID     string `json:"uid"`
				Summary string `json:"summary"`
			} `json:"events"`
		}
		code := doJSONRaw(t, http.MethodGet, "/calendars/"+uintStr(entry.ID)+"/events/"+rangeQS, token, nil, &resp)
		if code != http.StatusOK {
			continue
		}
		for _, ev := range resp.Events {
			all[ev.UID] = ev.Summary
		}
	}
	return all
}

// collectEventsByCalendarName returns calendar-name → UID → summary, limited
// to the calendars we actually seeded (Personal and other unrelated calendars
// are filtered out so the assertion stays focused).
func collectEventsByCalendarName(t *testing.T, token string, seeded map[string]map[string]string) map[string]map[string]string {
	t.Helper()
	idx := listCalendarsIndex(t, token)
	out := map[string]map[string]string{}
	// Supply an explicit wide time window: the repository-side query filters
	// on start_time/end_time, so a zero window would return zero events.
	rangeQS := "?start=2000-01-01T00:00:00Z&end=2099-12-31T23:59:59Z&expand=false"
	for calName := range seeded {
		entry, ok := idx[calName]
		if !ok {
			continue
		}
		var resp struct {
			Events []struct {
				UID     string `json:"uid"`
				Summary string `json:"summary"`
			} `json:"events"`
		}
		status, raw := restCall(t, http.MethodGet, "/calendars/"+uintStr(entry.ID)+"/events/"+rangeQS, token, nil)
		require.Equal(t, http.StatusOK, status, "list events for %s: %s", calName, string(raw))
		require.NoError(t, json.Unmarshal(raw, &resp))
		out[calName] = map[string]string{}
		for _, ev := range resp.Events {
			out[calName][ev.UID] = ev.Summary
		}
	}
	return out
}

func collectContactsByAddressBookName(t *testing.T, token string, seeded map[string]map[string]string) map[string]map[string]string {
	t.Helper()
	idx := listAddressBooksIndex(t, token)
	out := map[string]map[string]string{}
	for abName := range seeded {
		abID, ok := idx[abName]
		if !ok {
			continue
		}
		// Contact list endpoint returns raw JSON with PascalCase outer fields
		// and lowercase inner fields on each contact (contact.Contact has JSON tags).
		var resp struct {
			Contacts []struct {
				UID           string `json:"uid"`
				FormattedName string `json:"formatted_name"`
			} `json:"Contacts"`
		}
		code := doJSONRaw(t, http.MethodGet, "/addressbooks/"+uintStr(abID)+"/contacts?limit=100", token, nil, &resp)
		require.Equal(t, http.StatusOK, code, "list contacts for %s", abName)
		out[abName] = map[string]string{}
		for _, ct := range resp.Contacts {
			out[abName][ct.UID] = ct.FormattedName
		}
	}
	return out
}

// keys returns the (sorted) keys of a string map. Used so ElementsMatch
// produces stable diagnostic output.
func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
