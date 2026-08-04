//go:build integration

package integration_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// searchResponse mirrors the grouped payload of GET /api/v1/search (#156). Only
// the fields the assertions need are decoded.
type searchResponse struct {
	Query    string   `json:"query"`
	Types    []string `json:"types"`
	Limit    int      `json:"limit"`
	MaxLimit int      `json:"max_limit"`
	Events   struct {
		Items []struct {
			Event struct {
				ID           string    `json:"id"`
				Summary      string    `json:"summary"`
				Start        time.Time `json:"start"`
				RecurrenceID *string   `json:"recurrence_id"`
				IsRecurring  bool      `json:"is_recurring"`
			} `json:"event"`
			CalendarUUID  string `json:"calendar_uuid"`
			CalendarName  string `json:"calendar_name"`
			CalendarColor string `json:"calendar_color"`
		} `json:"items"`
		Count    int  `json:"count"`
		HasMore  bool `json:"has_more"`
		Searched bool `json:"searched"`
	} `json:"events"`
	Contacts struct {
		Items []struct {
			Contact struct {
				FormattedName string `json:"formatted_name"`
			} `json:"contact"`
			AddressBookName string `json:"addressbook_name"`
		} `json:"items"`
		Searched bool `json:"searched"`
	} `json:"contacts"`
	Calendars struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
		Searched bool `json:"searched"`
	} `json:"calendars"`
	AddressBooks struct {
		Items []struct {
			Name string `json:"Name"`
		} `json:"items"`
		Searched bool `json:"searched"`
	} `json:"addressbooks"`
}

func search(t *testing.T, token, query string) (int, searchResponse) {
	t.Helper()
	var out searchResponse
	// Raw JSON, like its sibling /contacts/search — no {status, data} envelope.
	code := doJSONRaw(t, http.MethodGet, "/search?"+query, token, nil, &out)
	return code, out
}

func eventTitles(resp searchResponse) []string {
	out := make([]string, 0, len(resp.Events.Items))
	for _, item := range resp.Events.Items {
		out = append(out, item.Event.Summary)
	}
	return out
}

// TestUnifiedSearch exercises the route as it is actually wired in routes.go
// (the handler tests build their own app, so a wiring mistake would slip past
// them) and covers the defect #156 fixed: with the previous client-side ±6-month
// window, an event years away could not be found at all.
func TestUnifiedSearch(t *testing.T) {
	token, _ := registerAndLoginFull(t, "search-user@example.test", "searchSecret!123", "Search User")
	_, calUUID := createCalendar(t, token, "Expedition Calendar", "#abcdef")
	_, abID := createAddressBook(t, token, "Expedition Contacts")

	seedEvent := func(summary string, start time.Time, recurrence map[string]any) {
		t.Helper()
		body := map[string]any{
			"summary":  summary,
			"start":    start.Format(time.RFC3339),
			"end":      start.Add(time.Hour).Format(time.RFC3339),
			"timezone": "UTC",
			"all_day":  false,
		}
		if recurrence != nil {
			body["recurrence"] = recurrence
		}
		status, raw := restCall(t, http.MethodPost, "/calendars/"+calUUID+"/events/", token, body)
		require.Equalf(t, http.StatusCreated, status, "seed %s: %s", summary, errorMessage(raw))
	}

	now := time.Now().UTC()
	seedEvent("Expedition briefing far ahead", now.AddDate(3, 0, 0), nil)
	seedEvent("Expedition debrief long past", now.AddDate(-3, 0, 0), nil)
	seedEvent("Expedition weekly sync", now.AddDate(-1, 0, 0).Truncate(time.Hour), map[string]any{"frequency": "WEEKLY"})
	seedEvent("Unrelated dentist", now.Add(24*time.Hour), nil)

	status, raw := restCall(t, http.MethodPost, "/addressbooks/"+abID+"/contacts", token, map[string]any{
		"formatted_name": "Expedition Leader",
		"emails":         []map[string]string{{"type": "work", "value": "leader@example.test"}},
	})
	require.Equalf(t, http.StatusCreated, status, "seed contact: %s", errorMessage(raw))

	t.Run("finds matches across every category with no date bound", func(t *testing.T) {
		code, resp := search(t, token, "q=expedition")
		require.Equal(t, http.StatusOK, code)

		assert.ElementsMatch(t, []string{
			"Expedition briefing far ahead",
			"Expedition debrief long past",
			"Expedition weekly sync",
		}, eventTitles(resp), "events three years out and three years back must both be findable")

		require.Len(t, resp.Contacts.Items, 1)
		assert.Equal(t, "Expedition Leader", resp.Contacts.Items[0].Contact.FormattedName)
		assert.Equal(t, "Expedition Contacts", resp.Contacts.Items[0].AddressBookName)

		require.Len(t, resp.Calendars.Items, 1)
		assert.Equal(t, "Expedition Calendar", resp.Calendars.Items[0].Name)
		require.Len(t, resp.AddressBooks.Items, 1)
		assert.Equal(t, "Expedition Contacts", resp.AddressBooks.Items[0].Name)

		assert.Equal(t, 100, resp.MaxLimit, "the cap must be reported so a client can spot truncation")
	})

	t.Run("a recurring series is represented by its next occurrence", func(t *testing.T) {
		code, resp := search(t, token, "q=weekly+sync")
		require.Equal(t, http.StatusOK, code)
		require.Len(t, resp.Events.Items, 1, "one hit per series, not one per occurrence")

		hit := resp.Events.Items[0]
		assert.True(t, hit.Event.IsRecurring)
		require.NotNil(t, hit.Event.RecurrenceID, "must carry the occurrence's recurrence id")
		assert.NotEmpty(t, *hit.Event.RecurrenceID)
		assert.Truef(t, hit.Event.Start.After(now.Add(-2*time.Hour)),
			"expected an upcoming occurrence, got %s (series began a year ago)", hit.Event.Start)
		assert.Equal(t, calUUID, hit.CalendarUUID)
		assert.Equal(t, "Expedition Calendar", hit.CalendarName)
		assert.Equal(t, "#abcdef", hit.CalendarColor)
	})

	t.Run("types narrows the search and says what it skipped", func(t *testing.T) {
		code, resp := search(t, token, "q=expedition&types=contacts")
		require.Equal(t, http.StatusOK, code)
		assert.True(t, resp.Contacts.Searched)
		assert.False(t, resp.Events.Searched, "an unsearched group must not read as 'nothing matched'")
		assert.Empty(t, resp.Events.Items)
	})

	t.Run("explicit bounds are honoured when asked for", func(t *testing.T) {
		q := url.Values{}
		q.Set("q", "expedition")
		q.Set("types", "events")
		q.Set("start", now.Format(time.RFC3339))
		q.Set("end", now.AddDate(0, 1, 0).Format(time.RFC3339))

		code, resp := search(t, token, q.Encode())
		require.Equal(t, http.StatusOK, code)
		// Only the still-running weekly series overlaps the next month.
		assert.Equal(t, []string{"Expedition weekly sync"}, eventTitles(resp))
	})

	t.Run("rejects a too-short query", func(t *testing.T) {
		status, _ := restCall(t, http.MethodGet, "/search?q=e", token, nil)
		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("requires authentication", func(t *testing.T) {
		status, _ := restCall(t, http.MethodGet, "/search?q=expedition", "", nil)
		assert.Equal(t, http.StatusUnauthorized, status)
	})

	// Cross-user isolation: another account must not learn that any of this
	// exists — not the events, not the contact, not even the collection names.
	t.Run("does not leak another user's data", func(t *testing.T) {
		otherToken, _ := registerAndLoginFull(t, "search-other@example.test", "searchSecret!456", "Other Searcher")

		code, resp := search(t, otherToken, "q=expedition")
		require.Equal(t, http.StatusOK, code)
		assert.Empty(t, resp.Events.Items)
		assert.Empty(t, resp.Contacts.Items)
		assert.Empty(t, resp.Calendars.Items)
		assert.Empty(t, resp.AddressBooks.Items)
	})
}
