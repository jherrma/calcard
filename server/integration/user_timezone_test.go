//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserTimezonePreference covers the `timezone` preference and the
// set-if-empty PUT /users/me/preferences/timezone-default (#169, story 01).
func TestUserTimezonePreference(t *testing.T) {
	email := "tz-pref@example.test"
	token := registerAndLogin(t, email, "tzSecret!12345", "Tz Pref")

	rowCount := func() (int64, string) {
		var rows []user.UserPreference
		require.NoError(t, testDB.Where("user_id = (SELECT id FROM users WHERE email = ?) AND key = ?", email, user.PrefTimezone).Find(&rows).Error)
		if len(rows) == 0 {
			return 0, ""
		}
		return int64(len(rows)), rows[0].Value
	}
	prefs := func() map[string]string {
		var out struct {
			Data struct {
				Preferences map[string]string `json:"preferences"`
			} `json:"data"`
		}
		status, body := restCall(t, http.MethodGet, "/users/me/preferences", token, nil)
		require.Equal(t, http.StatusOK, status)
		require.NoError(t, json.Unmarshal(body, &out))
		return out.Data.Preferences
	}
	put := func(tok string, body any) (int, string) {
		status, raw := restCall(t, http.MethodPut, "/users/me/preferences/timezone-default", tok, body)
		var out struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Data    struct {
				Timezone string `json:"timezone"`
			} `json:"data"`
		}
		_ = json.Unmarshal(raw, &out)
		if status == http.StatusOK {
			assert.Equal(t, "ok", out.Status)
			return status, out.Data.Timezone
		}
		return status, out.Message
	}
	patch := func(v string) (int, string) {
		status, raw := restCall(t, http.MethodPatch, "/users/me/preferences", token,
			map[string]any{"preferences": map[string]string{"timezone": v}})
		var out struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &out)
		return status, out.Message
	}

	// Default is UTC and there is no row.
	assert.Equal(t, "UTC", prefs()["timezone"])
	n, _ := rowCount()
	assert.Zero(t, n)

	// PATCH validation: nothing is written for a bad value.
	for _, bad := range []string{"", "Local", "Europe/Nowhere", "../etc/passwd", strings.Repeat("a", 65)} {
		status, msg := patch(bad)
		assert.Equal(t, http.StatusBadRequest, status, "%q", bad)
		assert.Equal(t, `invalid preference value: "timezone" must be an IANA time zone name such as Europe/Berlin`, msg)
	}
	n, _ = rowCount()
	assert.Zero(t, n)

	// PUT creates the row, a second PUT changes nothing.
	status, tz := put(token, map[string]string{"timezone": "Europe/Berlin"})
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "Europe/Berlin", tz)
	_, tz = put(token, map[string]string{"timezone": "America/New_York"})
	assert.Equal(t, "Europe/Berlin", tz)
	n, v := rowCount()
	assert.EqualValues(t, 1, n)
	assert.Equal(t, "Europe/Berlin", v)

	// PATCH wins and is trimmed; PUT then answers it.
	status, _ = patch(" Asia/Tokyo ")
	assert.Equal(t, http.StatusOK, status)
	_, v = rowCount()
	assert.Equal(t, "Asia/Tokyo", v)
	_, tz = put(token, map[string]string{"timezone": "Europe/Berlin"})
	assert.Equal(t, "Asia/Tokyo", tz)

	// An explicit UTC is a row and is never replaced.
	status, _ = patch("UTC")
	assert.Equal(t, http.StatusOK, status)
	_, tz = put(token, map[string]string{"timezone": "Europe/Berlin"})
	assert.Equal(t, "UTC", tz)

	// A hand-edited invalid row is replaced.
	require.NoError(t, testDB.Exec("UPDATE user_preferences SET value = ? WHERE key = ? AND user_id = (SELECT id FROM users WHERE email = ?)",
		"Mars/Base", user.PrefTimezone, email).Error)
	_, tz = put(token, map[string]string{"timezone": "Europe/Berlin"})
	assert.Equal(t, "Europe/Berlin", tz)

	// Request errors.
	for _, body := range []any{"not json", map[string]any{}, map[string]any{"timezone": 5}} {
		status, msg := put(token, body)
		assert.Equal(t, http.StatusBadRequest, status)
		assert.Equal(t, "Invalid request body", msg)
	}
	status, _ = put("", map[string]string{"timezone": "UTC"})
	assert.Equal(t, http.StatusUnauthorized, status)

	// Concurrent first PUTs: one row, identical answers.
	token2 := registerAndLogin(t, "tz-pref2@example.test", "tzSecret!12345", "Tz Pref 2")
	zones := []string{"Europe/Berlin", "Asia/Tokyo", "America/New_York", "Australia/Sydney"}
	answers := make([]string, len(zones))
	var wg sync.WaitGroup
	for i, z := range zones {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, answers[i] = put(token2, map[string]string{"timezone": z})
		}()
	}
	wg.Wait()
	var rows []user.UserPreference
	require.NoError(t, testDB.Where("user_id = (SELECT id FROM users WHERE email = ?) AND key = ?", "tz-pref2@example.test", user.PrefTimezone).Find(&rows).Error)
	require.Len(t, rows, 1)
	for _, a := range answers {
		assert.Equal(t, rows[0].Value, a)
	}
}
