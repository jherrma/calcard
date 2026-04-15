//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserFlow walks a complete user journey against the live server: first
// boot, admin-equivalent first user, a second self-registered user, login,
// profile, calendar/event/addressbook/contact CRUD, password change, logout.
// Sub-tests share state through a pointer-receiver so each step is a labelled
// t.Run but they read each other's progress (token, ids, etc.).
//
// This runs against the per-package server started by TestMain; subsequent
// tests (backup_test.go, caldav_test.go, carddav_test.go) reuse that same
// server but each creates its own users to stay independent.
func TestUserFlow(t *testing.T) {
	s := &flowState{}

	// Note: the true "first boot" settings check lives in TestMain so it runs
	// exactly once, before any test registers a user. Here we just verify the
	// rest of the admin-creation flow.
	t.Run("RegisterAdmin", s.registerAdmin)
	t.Run("AfterAdminSettings", s.afterAdminSettings)
	t.Run("RegisterSecondUser", s.registerSecondUser)
	t.Run("Login", s.login)
	t.Run("Profile", s.profile)
	t.Run("CreateCalendar", s.createCalendar)
	t.Run("UpdateCalendar", s.updateCalendar)
	t.Run("CreateEvent", s.createEvent)
	t.Run("UpdateEvent", s.updateEvent)
	t.Run("DeleteEvent", s.deleteEvent)
	t.Run("DeleteSecondCalendar", s.deleteSecondCalendar)
	t.Run("CreateAddressBook", s.createAddressBook)
	t.Run("UpdateAddressBook", s.updateAddressBook)
	t.Run("CreateContact", s.createContact)
	t.Run("UpdateContact", s.updateContact)
	t.Run("DeleteContact", s.deleteContact)
	t.Run("DeleteSecondAddressBook", s.deleteSecondAddressBook)
	t.Run("ChangePassword", s.changePassword)
	t.Run("Logout", s.logout)
}

type flowState struct {
	adminEmail    string
	adminPassword string
	adminToken    string
	adminRefresh  string

	secondEmail string

	calendarUUID   string
	calendarID     uint
	secondCalendar string // UUID of a second calendar we create so we can delete it

	eventID string

	addressBookID    uint
	secondAddressBk  uint
	contactID        string
}

func (s *flowState) registerAdmin(t *testing.T) {
	s.adminEmail = "admin@example.test"
	s.adminPassword = "adminSecret!123"

	reqBody := map[string]string{
		"email":        s.adminEmail,
		"password":     s.adminPassword,
		"display_name": "Admin User",
	}
	var resp struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		IsActive      bool   `json:"is_active"`
		EmailVerified bool   `json:"email_verified"`
	}
	code := doJSON(t, http.MethodPost, "/auth/register", "", reqBody, &resp)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, s.adminEmail, resp.Email)
	assert.True(t, resp.IsActive, "SMTP disabled → auto-activated")
	assert.True(t, resp.EmailVerified)
}

func (s *flowState) afterAdminSettings(t *testing.T) {
	var body struct {
		AdminConfigured bool `json:"admin_configured"`
	}
	doJSON(t, http.MethodGet, "/system/settings", "", nil, &body)
	assert.True(t, body.AdminConfigured, "admin_configured flips once the first user is created")
}

func (s *flowState) registerSecondUser(t *testing.T) {
	s.secondEmail = "second@example.test"
	reqBody := map[string]string{
		"email":        s.secondEmail,
		"password":     "secondSecret!123",
		"display_name": "Second User",
	}
	code := doJSON(t, http.MethodPost, "/auth/register", "", reqBody, nil)
	require.Equal(t, http.StatusOK, code)
}

func (s *flowState) login(t *testing.T) {
	reqBody := map[string]string{
		"email":    s.adminEmail,
		"password": s.adminPassword,
	}
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresAt    int64  `json:"expires_at"`
	}
	code := doJSON(t, http.MethodPost, "/auth/login", "", reqBody, &resp)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, resp.AccessToken)
	require.NotEmpty(t, resp.RefreshToken)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.Greater(t, resp.ExpiresAt, time.Now().Unix(), "expires_at is a Unix timestamp in the future")

	s.adminToken = resp.AccessToken
	s.adminRefresh = resp.RefreshToken
}

func (s *flowState) profile(t *testing.T) {
	var resp struct {
		Email         string `json:"email"`
		DisplayName   string `json:"display_name"`
		EmailVerified bool   `json:"email_verified"`
		Stats         struct {
			CalendarCount int `json:"calendar_count"`
			ContactCount  int `json:"contact_count"`
		} `json:"stats"`
	}
	code := doJSON(t, http.MethodGet, "/users/me", s.adminToken, nil, &resp)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, s.adminEmail, resp.Email)
	assert.True(t, resp.EmailVerified)
	// Register hook seeds a default "Personal" calendar.
	assert.GreaterOrEqual(t, resp.Stats.CalendarCount, 1)
}

func (s *flowState) createCalendar(t *testing.T) {
	reqBody := map[string]string{
		"name":        "Work",
		"description": "Work events",
		"color":       "#336699",
		"timezone":    "Europe/Berlin",
	}
	var cal struct {
		ID       uint   `json:"id"`
		UUID     string `json:"uuid"`
		Name     string `json:"name"`
		Color    string `json:"color"`
		Timezone string `json:"timezone"`
	}
	code := doJSONRaw(t, http.MethodPost, "/calendars/", s.adminToken, reqBody, &cal)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, cal.UUID)
	require.NotZero(t, cal.ID)
	assert.Equal(t, "Work", cal.Name)
	assert.Equal(t, "#336699", cal.Color)
	assert.Equal(t, "Europe/Berlin", cal.Timezone)

	s.calendarUUID = cal.UUID
	s.calendarID = cal.ID

	// A second calendar we're willing to delete (cannot delete the last one).
	var second struct {
		UUID string `json:"uuid"`
	}
	code2 := doJSONRaw(t, http.MethodPost, "/calendars/", s.adminToken,
		map[string]string{"name": "Disposable", "color": "#ff0000"}, &second)
	require.Equal(t, http.StatusCreated, code2)
	require.NotEmpty(t, second.UUID)
	s.secondCalendar = second.UUID
}

func (s *flowState) updateCalendar(t *testing.T) {
	newColor := "#00ff00"
	newName := "Work (renamed)"
	reqBody := map[string]*string{
		"name":  &newName,
		"color": &newColor,
	}
	var cal struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	code := doJSONRaw(t, http.MethodPatch, "/calendars/"+s.calendarUUID, s.adminToken, reqBody, &cal)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, newName, cal.Name)
	assert.Equal(t, newColor, cal.Color)
}

func (s *flowState) createEvent(t *testing.T) {
	start := time.Date(2030, 1, 15, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	reqBody := map[string]any{
		"summary":     "Planning meeting",
		"description": "Plan the quarter",
		"location":    "Room 4",
		"start":       start.Format(time.RFC3339),
		"end":         end.Format(time.RFC3339),
		"timezone":    "UTC",
		"all_day":     false,
	}
	var ev struct {
		ID      string `json:"id"`
		UID     string `json:"uid"`
		Summary string `json:"summary"`
	}
	path := "/calendars/" + uintStr(s.calendarID) + "/events/"
	code := doJSONRaw(t, http.MethodPost, path, s.adminToken, reqBody, &ev)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, ev.ID, "event should have a UUID")
	require.NotEmpty(t, ev.UID, "event should have a vCalendar UID")
	assert.Equal(t, "Planning meeting", ev.Summary)
	s.eventID = ev.ID
}

func (s *flowState) updateEvent(t *testing.T) {
	newSummary := "Planning meeting (moved)"
	reqBody := map[string]any{
		"summary": newSummary,
	}
	var ev struct {
		Summary string `json:"summary"`
	}
	path := "/calendars/" + uintStr(s.calendarID) + "/events/" + s.eventID
	code := doJSONRaw(t, http.MethodPatch, path, s.adminToken, reqBody, &ev)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, newSummary, ev.Summary)
}

func (s *flowState) deleteEvent(t *testing.T) {
	path := "/calendars/" + uintStr(s.calendarID) + "/events/" + s.eventID
	status, _ := restCall(t, http.MethodDelete, path, s.adminToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	// GET should now 404.
	status, _ = restCall(t, http.MethodGet, path, s.adminToken, nil)
	assert.Equal(t, http.StatusNotFound, status)
}

func (s *flowState) deleteSecondCalendar(t *testing.T) {
	reqBody := map[string]string{"confirmation": "DELETE"}
	status, raw := restCall(t, http.MethodDelete, "/calendars/"+s.secondCalendar, s.adminToken, reqBody)
	require.Equal(t, http.StatusNoContent, status, "delete disposable calendar: %s", errorMessage(raw))
}

func (s *flowState) createAddressBook(t *testing.T) {
	reqBody := map[string]string{
		"name":        "Colleagues",
		"description": "Work contacts",
	}
	// AddressBook JSON fields have no JSON tags → Go encodes them as PascalCase.
	var ab struct {
		ID          uint   `json:"ID"`
		UUID        string `json:"UUID"`
		Name        string `json:"Name"`
		Description string `json:"Description"`
	}
	code := doJSONRaw(t, http.MethodPost, "/addressbooks/", s.adminToken, reqBody, &ab)
	require.Equal(t, http.StatusCreated, code)
	require.NotZero(t, ab.ID)
	require.NotEmpty(t, ab.UUID)
	assert.Equal(t, "Colleagues", ab.Name)
	s.addressBookID = ab.ID

	// Second address book — we delete this one so we don't run into the
	// "cannot delete your last address book" guard when exercising DELETE.
	var ab2 struct {
		ID uint `json:"ID"`
	}
	code2 := doJSONRaw(t, http.MethodPost, "/addressbooks/", s.adminToken,
		map[string]string{"name": "Temp"}, &ab2)
	require.Equal(t, http.StatusCreated, code2)
	require.NotZero(t, ab2.ID)
	s.secondAddressBk = ab2.ID
}

func (s *flowState) updateAddressBook(t *testing.T) {
	newName := "Colleagues (renamed)"
	reqBody := map[string]*string{"name": &newName}
	var ab struct {
		Name string `json:"Name"`
	}
	code := doJSONRaw(t, http.MethodPatch, "/addressbooks/"+uintStr(s.addressBookID), s.adminToken, reqBody, &ab)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, newName, ab.Name)
}

func (s *flowState) createContact(t *testing.T) {
	// POST /addressbooks/:id/contacts is bound to ContactHandler.Create, which
	// takes the contact.Contact struct directly (not the vcard_data DTO used
	// by the addressbook Create route).
	reqBody := map[string]any{
		"given_name":     "Jane",
		"family_name":    "Doe",
		"formatted_name": "Jane Doe",
		"organization":   "Example Corp",
		"emails": []map[string]any{
			{"type": "work", "value": "jane@example.com", "primary": true},
		},
	}
	var ct struct {
		ID            string `json:"id"`
		UID           string `json:"uid"`
		FormattedName string `json:"formatted_name"`
	}
	path := "/addressbooks/" + uintStr(s.addressBookID) + "/contacts"
	code := doJSONRaw(t, http.MethodPost, path, s.adminToken, reqBody, &ct)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, ct.ID)
	assert.Equal(t, "Jane Doe", ct.FormattedName)
	s.contactID = ct.ID
}

func (s *flowState) updateContact(t *testing.T) {
	reqBody := map[string]any{
		"formatted_name": "Jane Q. Doe",
		"given_name":     "Jane",
		"middle_name":    "Q.",
		"family_name":    "Doe",
	}
	var ct struct {
		FormattedName string `json:"formatted_name"`
	}
	path := "/addressbooks/" + uintStr(s.addressBookID) + "/contacts/" + s.contactID
	code := doJSONRaw(t, http.MethodPatch, path, s.adminToken, reqBody, &ct)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "Jane Q. Doe", ct.FormattedName)
}

func (s *flowState) deleteContact(t *testing.T) {
	path := "/addressbooks/" + uintStr(s.addressBookID) + "/contacts/" + s.contactID
	status, _ := restCall(t, http.MethodDelete, path, s.adminToken, nil)
	require.Equal(t, http.StatusNoContent, status)

	// And a search for it should now come up empty. The search endpoint
	// returns raw JSON keyed with lowercase fields.
	status, raw := restCall(t, http.MethodGet, "/contacts/search?q=Jane", s.adminToken, nil)
	require.Equal(t, http.StatusOK, status)
	var out struct {
		Contacts []any `json:"contacts"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Len(t, out.Contacts, 0, "deleted contact should no longer match search")
}

func (s *flowState) deleteSecondAddressBook(t *testing.T) {
	reqBody := map[string]string{"confirmation": "DELETE"}
	status, raw := restCall(t, http.MethodDelete, "/addressbooks/"+uintStr(s.secondAddressBk), s.adminToken, reqBody)
	require.Equal(t, http.StatusNoContent, status, "delete temp addressbook: %s", errorMessage(raw))
}

func (s *flowState) changePassword(t *testing.T) {
	newPassword := "newAdminSecret!456"
	reqBody := map[string]string{
		"current_password": s.adminPassword,
		"new_password":     newPassword,
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	code := doJSON(t, http.MethodPut, "/users/me/password", s.adminToken, reqBody, &resp)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, resp.AccessToken, "change-password returns a freshly signed token")

	// Old password no longer works.
	status, _ := restCall(t, http.MethodPost, "/auth/login", "",
		map[string]string{"email": s.adminEmail, "password": s.adminPassword})
	assert.Equal(t, http.StatusUnauthorized, status)

	// New password does.
	var loginResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	code = doJSON(t, http.MethodPost, "/auth/login", "",
		map[string]string{"email": s.adminEmail, "password": newPassword}, &loginResp)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, loginResp.AccessToken)
	s.adminPassword = newPassword
	s.adminToken = loginResp.AccessToken
	s.adminRefresh = loginResp.RefreshToken
}

func (s *flowState) logout(t *testing.T) {
	reqBody := map[string]string{"refresh_token": s.adminRefresh}
	code := doJSON(t, http.MethodPost, "/auth/logout", s.adminToken, reqBody, nil)
	require.Equal(t, http.StatusOK, code)

	// The refresh token must be gone: subsequent refresh with it should fail.
	status, _ := restCall(t, http.MethodPost, "/auth/refresh", "", reqBody)
	assert.Equal(t, http.StatusUnauthorized, status, "logged-out refresh token must be invalid")
}

// uintStr is a tiny helper to keep path building readable.
func uintStr(v uint) string {
	return strconv.FormatUint(uint64(v), 10)
}
