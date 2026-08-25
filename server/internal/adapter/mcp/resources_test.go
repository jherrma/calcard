package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readResourceJSON reads a resource through the dispatcher and decodes its body.
func readResourceJSON(t *testing.T, e *testEnv, userID uint, uri string) map[string]interface{} {
	t.Helper()
	resp := e.rpc(userID, MethodResourcesRead, map[string]interface{}{"uri": uri})
	require.Nil(t, resp.Error, "resources/read(%s) failed: %v", uri, resp.Error)

	contents := resp.Result.(resourcesReadResult).Contents
	require.Len(t, contents, 1)
	assert.Equal(t, uri, contents[0].URI)
	assert.Equal(t, resourceMIMEType, contents[0].MIMEType)

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(contents[0].Text), &payload))
	return payload
}

func TestResourcesListEnumeratesPerCalendarEventLists(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")

	resp := e.rpc(alice.ID, MethodResourcesList, nil)
	require.Nil(t, resp.Error)
	resources := resp.Result.(resourcesListResult).Resources

	uris := map[string]bool{}
	for _, r := range resources {
		uris[r.URI] = true
		assert.NotEmpty(t, r.Name)
	}
	assert.True(t, uris[uriCalendarList])
	assert.True(t, uris[uriContactList])
	// Enumerated, not merely offered as a template: a client that cannot expand
	// templates would otherwise never discover it.
	assert.True(t, uris[calendarURIStem+cal.UUID+eventsURISuffix])
}

func TestResourceTemplatesAreAdvertised(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	resp := e.rpc(alice.ID, MethodResourcesTemplateList, nil)
	require.Nil(t, resp.Error)
	templates := resp.Result.(resourceTemplatesListResult).ResourceTemplates
	require.Len(t, templates, 2)
	for _, tmpl := range templates {
		assert.Contains(t, tmpl.URITemplate, "{")
	}
}

func TestReadCalendarListResource(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	e.newCalendar(alice.ID, "Work")

	payload := readResourceJSON(t, e, alice.ID, uriCalendarList)
	require.Len(t, payload["calendars"], 1)
}

func TestReadCalendarEventsResource(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")

	// fixedNow is 2026-03-04; the resource window is 30 days forward.
	mustCreate(t, e, alice.ID, cal.UUID, "Inside window", "2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z")
	mustCreate(t, e, alice.ID, cal.UUID, "Outside window", "2026-06-06T09:00:00Z", "2026-06-06T10:00:00Z")

	payload := readResourceJSON(t, e, alice.ID, calendarURIStem+cal.UUID+eventsURISuffix)
	events := payload["events"].([]interface{})
	require.Len(t, events, 1, "the resource window is fixed at 30 days; use get_events for anything else")
	assert.Equal(t, "Inside window", events[0].(map[string]interface{})["title"])
}

func TestReadContactResource(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	ab := e.newAddressBook(alice.ID, "Personal")
	contactID := mustCreateContact(t, e, alice.ID, ab.UUID, "Jane", "Smith")

	payload := readResourceJSON(t, e, alice.ID, contactsURIStem+contactID)
	assert.Equal(t, "Jane Smith", payload["name"])
	assert.Equal(t, ab.UUID, payload["address_book_id"])
}

func TestResourceReadEnforcesPermissions(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	bobsCal := e.newCalendar(bob.ID, "Bob private")
	bobsBook := e.newAddressBook(bob.ID, "Bob private")
	bobsContact := mustCreateContact(t, e, bob.ID, bobsBook.UUID, "Secret", "Person")

	// A resource URI is guessable, so it would be the one door into the data
	// that skips the permission check if it were not re-checked here.
	for _, uri := range []string{
		calendarURIStem + bobsCal.UUID + eventsURISuffix,
		contactsURIStem + bobsContact,
	} {
		resp := e.rpc(alice.ID, MethodResourcesRead, map[string]interface{}{"uri": uri})
		require.NotNil(t, resp.Error, "reading %s as another user must fail", uri)
		assert.Equal(t, CodeResourceNotFound, resp.Error.Code)
	}
}

func TestResourceReadRejectsUnknownURI(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	resp := e.rpc(alice.ID, MethodResourcesRead, map[string]interface{}{"uri": "wat://nope"})
	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeResourceNotFound, resp.Error.Code)

	resp = e.rpc(alice.ID, MethodResourcesRead, map[string]interface{}{})
	require.NotNil(t, resp.Error)
	assert.Equal(t, CodeInvalidParams, resp.Error.Code)
}
