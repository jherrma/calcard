//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestICalTextRoundTrip: SUMMARY, DESCRIPTION and LOCATION are plain text on
// the REST wire and RFC 5545 TEXT (§3.3.11, `\,` `\;` `\\` `\n`) in the
// iCalendar. A text with , ; \ and a line break written over REST is answered
// as sent and served over CalDAV escaped exactly once; the text a GET answered,
// PATCHed back, changes nothing (same ETag, same DAV bytes) instead of gaining
// a backslash per save; an escaped title a DAV client wrote reads as plain
// text. Tasks and events share the code, so both are covered, and the event
// search matches the plain text.
func TestICalTextRoundTrip(t *testing.T) {
	email := "text-escaping@example.test"
	token, username := registerAndLoginFull(t, email, "escapingSecret!123", "Text Escaping")
	_, appPass := createAppPassword(t, token, "text-escaping")
	list := createTaskCalendar(t, token, "Errands", "tasks")
	events := createTaskCalendar(t, token, "Plans", "events")
	taskCollection := "/dav/" + username + "/calendars/" + list.Path + "/"
	eventCollection := "/dav/" + username + "/calendars/" + events.Path + "/"

	const (
		summary     = `Buy milk, eggs; bread \ more`
		description = "Line one\nLine two, C:\\path; done"
		location    = `Market, Hall 2; Gate \ B`
		// The same texts as RFC 5545 TEXT, escaped exactly once.
		summaryLine     = `SUMMARY:Buy milk\, eggs\; bread \\ more`
		descriptionLine = `DESCRIPTION:Line one\nLine two\, C:\\path\; done`
		locationLine    = `LOCATION:Market\, Hall 2\; Gate \\ B`
	)

	t.Run("a REST task", func(t *testing.T) {
		created := createTask(t, token, list.UUID, map[string]any{"summary": summary, "description": description})
		assert.Equal(t, summary, created.Summary)
		assert.Equal(t, description, created.Description)

		object := taskCollection + created.ID + ".ics"
		body, davETag := davGet(t, object, email, appPass)
		lines := icalLines(body)
		assert.Contains(t, lines, summaryLine, "served over CalDAV escaped exactly once")
		assert.Contains(t, lines, descriptionLine)

		got := getTask(t, token, list.UUID, created.ID)
		assert.Equal(t, summary, got.Summary)
		assert.Equal(t, description, got.Description)

		// The web form's save loop: what GET answered, sent back, three times.
		for range 3 {
			status, raw := apiCall(t, http.MethodPatch, taskURL(list.UUID, created.ID), token,
				map[string]any{"summary": got.Summary, "description": got.Description}, ifMatch(got.ETag))
			require.Equal(t, http.StatusOK, status, string(raw))
			again := decodeTask(t, raw)
			assert.Equal(t, created.ETag, again.ETag, "nothing written")
			assert.Equal(t, summary, again.Summary)
			assert.Equal(t, description, again.Description)
		}
		after, afterETag := davGet(t, object, email, appPass)
		assert.Equal(t, body, after, "the DAV bytes are unchanged")
		assert.Equal(t, davETag, afterETag)

		status, raw := restCall(t, http.MethodPatch, taskURL(list.UUID, created.ID), token,
			map[string]any{"summary": summary + ", tomorrow"})
		require.Equal(t, http.StatusOK, status, string(raw))
		assert.Equal(t, summary+", tomorrow", decodeTask(t, raw).Summary)
		edited, _ := davGet(t, object, email, appPass)
		assert.Contains(t, icalLines(edited), summaryLine+`\, tomorrow`, "a changed text is escaped once")
		assert.Contains(t, icalLines(edited), descriptionLine, "the description not sent is untouched")
	})

	t.Run("a DAV-written task", func(t *testing.T) {
		davPut(t, taskCollection+"phone-text.ics", email, appPass, vcalendar(ics(
			"BEGIN:VTODO", "UID:phone-text@phone", "DTSTAMP:20301101T080000Z",
			`SUMMARY:Call Bob\, Alice\; maybe`, `DESCRIPTION:Agenda:\n1. budget\, 2. C:\\docs`,
			"END:VTODO")))
		phone := findTaskByUID(t, token, list.UUID, "phone-text@phone")
		assert.Equal(t, "Call Bob, Alice; maybe", phone.Summary)
		assert.Equal(t, "Agenda:\n1. budget, 2. C:\\docs", phone.Description)

		status, raw := restCall(t, http.MethodPatch, taskURL(list.UUID, phone.ID), token,
			map[string]any{"summary": phone.Summary, "description": phone.Description})
		require.Equal(t, http.StatusOK, status, string(raw))
		assert.Equal(t, phone.ETag, decodeTask(t, raw).ETag, "the text as read, sent back, writes nothing")
	})

	t.Run("a REST event", func(t *testing.T) {
		var ev struct {
			ID          string `json:"id"`
			Summary     string `json:"summary"`
			Description string `json:"description"`
			Location    string `json:"location"`
		}
		status := doJSONRaw(t, http.MethodPost, "/calendars/"+events.UUID+"/events", token, map[string]any{
			"summary": summary, "description": description, "location": location,
			"start": "2030-03-04T09:00:00Z", "end": "2030-03-04T10:00:00Z",
		}, &ev)
		require.Equal(t, http.StatusCreated, status)
		assert.Equal(t, summary, ev.Summary)
		assert.Equal(t, description, ev.Description)
		assert.Equal(t, location, ev.Location)

		object := eventCollection + ev.ID + ".ics"
		body, _ := davGet(t, object, email, appPass)
		for _, want := range []string{summaryLine, descriptionLine, locationLine} {
			assert.Contains(t, icalLines(body), want, "served over CalDAV escaped exactly once")
		}

		eventPath := "/calendars/" + events.UUID + "/events/" + ev.ID
		for range 2 {
			status, raw := restCall(t, http.MethodGet, eventPath, token, nil)
			require.Equal(t, http.StatusOK, status, string(raw))
			var got struct {
				Summary     string `json:"summary"`
				Description string `json:"description"`
				Location    string `json:"location"`
			}
			require.NoError(t, json.Unmarshal(raw, &got))
			assert.Equal(t, summary, got.Summary)
			assert.Equal(t, description, got.Description)
			assert.Equal(t, location, got.Location)

			status, raw = restCall(t, http.MethodPatch, eventPath, token,
				map[string]any{"summary": got.Summary, "description": got.Description, "location": got.Location})
			require.Equal(t, http.StatusOK, status, string(raw))
			body, _ := davGet(t, object, email, appPass)
			for _, want := range []string{summaryLine, descriptionLine, locationLine} {
				assert.Contains(t, icalLines(body), want, "no backslash added by a PATCH of the answered text")
			}
		}

		code, found := search(t, token, url.Values{"q": {"milk, eggs"}, "types": {"events"}}.Encode())
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, []string{summary}, eventTitles(found), "search matches the plain text")
	})
}
