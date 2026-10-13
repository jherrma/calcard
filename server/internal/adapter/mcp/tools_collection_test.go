package mcp

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- calendars --------------------------------------------------------------

func TestCreateCalendarThenUseIt(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	created, isErr := e.call(alice.ID, "create_calendar", map[string]interface{}{
		"name":        "Climbing",
		"description": "Gym sessions",
		"color":       "#10b981",
		"timezone":    "Europe/Berlin",
	})
	require.False(t, isErr, "create_calendar failed: %v", created["error"])
	assert.Equal(t, true, created["created"])

	view := created["calendar"].(map[string]interface{})
	calID := view["id"].(string)
	require.NotEmpty(t, calID, "the new calendar must carry the id every other tool needs")
	assert.Equal(t, "Climbing", view["name"])
	assert.Equal(t, "Gym sessions", view["description"])
	assert.Equal(t, "#10b981", view["color"])
	assert.Equal(t, "Europe/Berlin", view["timezone"])
	assert.Equal(t, "owner", view["permission"])
	assert.Equal(t, false, view["shared"])
	assert.Equal(t, float64(0), view["event_count"], "a brand-new calendar is empty")

	// Stored through the use case, owned by the caller.
	stored, err := e.calRepo.GetByUUID(e.ctx(), calID)
	require.NoError(t, err)
	assert.Equal(t, alice.ID, stored.UserID)
	assert.Equal(t, calID+".ics", stored.Path, "the use case, not MCP, derives the DAV path")

	listed, _ := e.call(alice.ID, "list_calendars", nil)
	assert.Contains(t, calendarIDs(listed), calID)

	// And it is immediately usable by the event tools.
	mustCreate(t, e, alice.ID, calID, "Bouldering", "2026-03-06T18:00:00Z", "2026-03-06T20:00:00Z")
}

func TestCreateCalendarAppliesTheUseCaseDefaults(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	created, isErr := e.call(alice.ID, "create_calendar", map[string]interface{}{"name": "Plain"})
	require.False(t, isErr, "create_calendar failed: %v", created["error"])
	view := created["calendar"].(map[string]interface{})
	assert.Regexp(t, regexp.MustCompile(`^#[0-9a-fA-F]{6}$`), view["color"], "a colour is picked when none is given")
	assert.Equal(t, "UTC", view["timezone"])
}

func TestCreateCalendarValidationErrorsAreTheUseCaseMessages(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	for _, tc := range []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"missing name", map[string]interface{}{}, "name cannot be empty"},
		{"empty name", map[string]interface{}{"name": ""}, "name cannot be empty"},
		{"long name", map[string]interface{}{"name": strings.Repeat("n", 256)}, "name cannot exceed 255 characters"},
		{"long description", map[string]interface{}{"name": "x", "description": strings.Repeat("d", 1001)},
			"description cannot exceed 1000 characters"},
		{"colour name", map[string]interface{}{"name": "x", "color": "blue"}, "invalid hex color format, expected #RRGGBB"},
		{"short hex", map[string]interface{}{"name": "x", "color": "#fff"}, "invalid hex color format, expected #RRGGBB"},
		{"unknown zone", map[string]interface{}{"name": "x", "timezone": "Mars/Olympus_Mons"}, "invalid timezone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := e.callRaw(alice.ID, "create_calendar", tc.args)
			require.True(t, result.IsError)
			// The bare message REST answers with a 400 — no "Failed to …"
			// prefix, nothing internal.
			assert.True(t, strings.HasPrefix(result.Content[0].Text, tc.want),
				"want %q, got %q", tc.want, result.Content[0].Text)
		})
	}

	listed, _ := e.call(alice.ID, "list_calendars", nil)
	assert.Empty(t, calendarIDs(listed), "a refused create must not store anything")
}

func TestUpdateCalendarChangesOnlyThePassedFields(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")
	cal.Description = "Day job"
	cal.Timezone = "Europe/Berlin"
	require.NoError(t, e.calRepo.UpdateMetadata(e.ctx(), cal))

	updated, isErr := e.call(alice.ID, "update_calendar", map[string]interface{}{
		"calendar_id": cal.UUID,
		"name":        "Office",
	})
	require.False(t, isErr, "update_calendar failed: %v", updated["error"])
	assert.Equal(t, true, updated["updated"])
	view := updated["calendar"].(map[string]interface{})
	assert.Equal(t, cal.UUID, view["id"])
	assert.Equal(t, "Office", view["name"])
	assert.Equal(t, "owner", view["permission"])
	assert.NotContains(t, view, "event_count", "update does not count events, so it must not claim a count")

	stored, err := e.calRepo.GetByUUID(e.ctx(), cal.UUID)
	require.NoError(t, err)
	assert.Equal(t, "Office", stored.Name)
	assert.Equal(t, "Day job", stored.Description, "an omitted field keeps its value")
	assert.Equal(t, "#3b82f6", stored.Color, "an omitted field keeps its value")
	assert.Equal(t, "Europe/Berlin", stored.Timezone, "an omitted field keeps its value")

	// Another partial update, then clearing the description explicitly.
	_, isErr = e.call(alice.ID, "update_calendar", map[string]interface{}{
		"calendar_id": cal.UUID,
		"color":       "#ef4444",
		"timezone":    "America/New_York",
		"description": "",
	})
	require.False(t, isErr)
	stored, err = e.calRepo.GetByUUID(e.ctx(), cal.UUID)
	require.NoError(t, err)
	assert.Equal(t, "Office", stored.Name, "the earlier rename survives a later partial update")
	assert.Equal(t, "#ef4444", stored.Color)
	assert.Equal(t, "America/New_York", stored.Timezone)
	assert.Equal(t, "", stored.Description, "an explicit empty string clears the description")
}

func TestUpdateCalendarValidation(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")

	for _, tc := range []struct {
		args map[string]interface{}
		want string
	}{
		{map[string]interface{}{"name": ""}, "name cannot be empty"},
		{map[string]interface{}{"color": "red"}, "invalid hex color format, expected #RRGGBB"},
		{map[string]interface{}{"timezone": "Nowhere/Special"}, "invalid timezone"},
		{map[string]interface{}{"description": strings.Repeat("d", 1001)}, "description cannot exceed 1000 characters"},
	} {
		tc.args["calendar_id"] = cal.UUID
		result := e.callRaw(alice.ID, "update_calendar", tc.args)
		require.True(t, result.IsError)
		assert.True(t, strings.HasPrefix(result.Content[0].Text, tc.want),
			"want %q, got %q", tc.want, result.Content[0].Text)
	}

	stored, err := e.calRepo.GetByUUID(e.ctx(), cal.UUID)
	require.NoError(t, err)
	assert.Equal(t, "Work", stored.Name)
	assert.Equal(t, "#3b82f6", stored.Color)
	assert.Equal(t, "UTC", stored.Timezone)
}

// TestCalendarCollectionToolsAreOwnerOnly is the F05/F12 rule over MCP: an
// unknown calendar, another user's, and one merely SHARED with the caller (even
// read-write) all get the same answer, and nothing changes.
func TestCalendarCollectionToolsAreOwnerOnly(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	e.newCalendar(alice.ID, "Alice 1")
	e.newCalendar(alice.ID, "Alice 2") // so the last-calendar guard is never what answers

	private := e.newCalendar(bob.ID, "Bob private")
	sharedRW := e.newCalendar(bob.ID, "Bob shared rw")
	e.shareCalendar(sharedRW, alice.ID, "read-write")
	sharedRO := e.newCalendar(bob.ID, "Bob shared ro")
	e.shareCalendar(sharedRO, alice.ID, "read")

	targets := map[string]string{
		"unknown":           uuid.New().String(),
		"another user's":    private.UUID,
		"shared read-write": sharedRW.UUID,
		"shared read":       sharedRO.UUID,
	}

	var reference string
	for label, id := range targets {
		for _, call := range []struct {
			tool string
			args map[string]interface{}
		}{
			{"update_calendar", map[string]interface{}{"calendar_id": id, "name": "Hijacked"}},
			// Invalid input must not change the answer either: ownership is
			// resolved before any field is looked at.
			{"update_calendar", map[string]interface{}{"calendar_id": id, "color": "not-a-colour"}},
			{"delete_calendar", map[string]interface{}{"calendar_id": id, "confirmation": "DELETE"}},
			{"delete_calendar", map[string]interface{}{"calendar_id": id, "confirmation": "nope"}},
		} {
			result := e.callRaw(alice.ID, call.tool, call.args)
			require.True(t, result.IsError, "%s on a %s calendar must be refused", call.tool, label)
			text := strings.ReplaceAll(result.Content[0].Text, id, "<id>")
			if reference == "" {
				reference = text
			}
			assert.Equal(t, reference, text,
				"%s on a %s calendar must answer exactly like every other non-owned calendar", call.tool, label)
		}
	}
	assert.Contains(t, reference, "is owned by you")

	for _, cal := range []*calendar.Calendar{private, sharedRW, sharedRO} {
		stored, err := e.calRepo.GetByUUID(e.ctx(), cal.UUID)
		require.NoError(t, err, "%s must still exist", cal.Name)
		assert.Equal(t, cal.Name, stored.Name, "%s must be unchanged", cal.Name)
	}
}

// TestSubscribedCalendarCanBeRenamedAndDeletedByItsOwner: a subscription is a
// read-only mirror for its EVENTS, but the collection is still the owner's —
// the same split REST and CalDAV make.
func TestSubscribedCalendarCanBeRenamedAndDeletedByItsOwner(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	e.newCalendar(alice.ID, "Personal")

	sub := &calendar.Calendar{
		UUID:                uuid.New().String(),
		UserID:              alice.ID,
		Path:                "holidays.ics",
		Name:                "Holidays",
		Color:               "#f59e0b",
		Timezone:            "UTC",
		SupportedComponents: "VEVENT",
		Subscribed:          true,
	}
	require.NoError(t, e.calRepo.Create(e.ctx(), sub))
	require.NoError(t, e.db.Create(&calendar.CalendarSubscription{
		UUID:            uuid.New().String(),
		CalendarID:      sub.ID,
		UserID:          alice.ID,
		URL:             "https://feeds.example.com/holidays.ics",
		RefreshInterval: time.Hour,
		NextSyncAt:      fixedNow,
		Enabled:         true,
	}).Error)

	// Control: its events stay read-only, so the cap is really in force here.
	refused := e.callRaw(alice.ID, "create_event", map[string]interface{}{
		"calendar_id": sub.UUID, "title": "x",
		"start": "2026-03-06T09:00:00Z", "end": "2026-03-06T10:00:00Z",
	})
	require.True(t, refused.IsError, "a subscribed calendar must refuse event writes")

	renamed, isErr := e.call(alice.ID, "update_calendar", map[string]interface{}{
		"calendar_id": sub.UUID, "name": "Public holidays", "color": "#000000",
	})
	require.False(t, isErr, "the owner must be able to rename a subscription: %v", renamed["error"])
	stored, err := e.calRepo.GetByUUID(e.ctx(), sub.UUID)
	require.NoError(t, err)
	assert.Equal(t, "Public holidays", stored.Name)
	assert.True(t, stored.Subscribed, "renaming must not turn the mirror into a writable calendar")

	deleted, isErr := e.call(alice.ID, "delete_calendar", map[string]interface{}{
		"calendar_id": sub.UUID, "confirmation": "DELETE",
	})
	require.False(t, isErr, "the owner must be able to delete a subscription: %v", deleted["error"])
	_, err = e.calRepo.GetByUUID(e.ctx(), sub.UUID)
	assert.True(t, errors.Is(err, calendar.ErrNotFound), "the calendar must be gone, got %v", err)

	var subs int64
	require.NoError(t, e.db.Model(&calendar.CalendarSubscription{}).Where("calendar_id = ?", sub.ID).Count(&subs).Error)
	assert.Zero(t, subs, "the subscription goes with its calendar, or the worker keeps fetching for nothing")
}

func TestDeleteCalendarTakesItsEventsAndShares(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	keep := e.newCalendar(alice.ID, "Keep")
	doomed := e.newCalendar(alice.ID, "Doomed")
	e.shareCalendar(doomed, bob.ID, "read-write")

	eventID := mustCreate(t, e, alice.ID, doomed.UUID, "Quarterly zebra review", "2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z")
	mustCreate(t, e, alice.ID, keep.UUID, "Zebra feeding", "2026-03-07T09:00:00Z", "2026-03-07T10:00:00Z")

	bobsList, _ := e.call(bob.ID, "list_calendars", nil)
	require.Contains(t, calendarIDs(bobsList), doomed.UUID, "control: bob sees the share before the delete")

	deleted, isErr := e.call(alice.ID, "delete_calendar", map[string]interface{}{
		"calendar_id": doomed.UUID, "confirmation": "DELETE",
	})
	require.False(t, isErr, "delete_calendar failed: %v", deleted["error"])
	assert.Equal(t, true, deleted["deleted"])
	assert.Equal(t, doomed.UUID, deleted["calendar_id"])

	listed, _ := e.call(alice.ID, "list_calendars", nil)
	assert.NotContains(t, calendarIDs(listed), doomed.UUID)
	assert.Contains(t, calendarIDs(listed), keep.UUID, "only the named calendar is deleted")

	// Its events are gone from every surface that could reach them.
	assert.True(t, e.callRaw(alice.ID, "get_events", map[string]interface{}{"calendar_id": doomed.UUID}).IsError)
	assert.True(t, e.callRaw(alice.ID, "update_event", map[string]interface{}{"event_id": eventID, "title": "x"}).IsError,
		"an event of a deleted calendar must not be writable")
	found, isErr := e.call(alice.ID, "search_events", map[string]interface{}{"query": "zebra"})
	require.False(t, isErr)
	var titles []string
	for _, raw := range found["matches"].([]interface{}) {
		titles = append(titles, raw.(map[string]interface{})["event"].(map[string]interface{})["title"].(string))
	}
	assert.Equal(t, []string{"Zebra feeding"}, titles, "only the event of the surviving calendar is still found")

	// And the share went with it.
	bobsList, _ = e.call(bob.ID, "list_calendars", nil)
	assert.NotContains(t, calendarIDs(bobsList), doomed.UUID, "a sharee must lose a deleted calendar")
}

func TestDeleteCalendarNeedsTheConfirmation(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	e.newCalendar(alice.ID, "Keep")
	cal := e.newCalendar(alice.ID, "Work")

	for _, confirmation := range []interface{}{nil, "", "delete", "yes"} {
		args := map[string]interface{}{"calendar_id": cal.UUID}
		if confirmation != nil {
			args["confirmation"] = confirmation
		}
		result := e.callRaw(alice.ID, "delete_calendar", args)
		require.True(t, result.IsError, "confirmation %v must not delete", confirmation)
		assert.Equal(t, "please type DELETE to confirm calendar deletion", result.Content[0].Text)
	}

	_, err := e.calRepo.GetByUUID(e.ctx(), cal.UUID)
	assert.NoError(t, err, "the calendar must survive every refused delete")
}

func TestDeleteLastCalendarIsRefused(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	only := e.newCalendar(alice.ID, "Only")

	result := e.callRaw(alice.ID, "delete_calendar", map[string]interface{}{
		"calendar_id": only.UUID, "confirmation": "DELETE",
	})
	require.True(t, result.IsError)
	assert.Equal(t, "cannot delete your last calendar", result.Content[0].Text)

	_, err := e.calRepo.GetByUUID(e.ctx(), only.UUID)
	assert.NoError(t, err)
}

// --- address books ----------------------------------------------------------

func TestCreateAddressBookThenUseIt(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	created, isErr := e.call(alice.ID, "create_address_book", map[string]interface{}{
		"name":        "Climbing partners",
		"description": "Belay buddies",
	})
	require.False(t, isErr, "create_address_book failed: %v", created["error"])
	assert.Equal(t, true, created["created"])
	view := created["address_book"].(map[string]interface{})
	abID := view["id"].(string)
	require.NotEmpty(t, abID)
	assert.Equal(t, "Climbing partners", view["name"])
	assert.Equal(t, "Belay buddies", view["description"])
	assert.Equal(t, "owner", view["permission"])
	assert.Equal(t, false, view["shared"])

	stored, err := e.abRepo.GetByUUID(e.ctx(), abID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, alice.ID, stored.UserID)

	listed, _ := e.call(alice.ID, "list_address_books", nil)
	assert.Contains(t, addressBookIDs(listed), abID)

	contact, isErr := e.call(alice.ID, "create_contact", map[string]interface{}{
		"address_book_id": abID, "first_name": "Lynn",
	})
	require.False(t, isErr, "the new book must take contacts: %v", contact["error"])
}

func TestCreateAddressBookRequiresAName(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	for _, args := range []map[string]interface{}{{}, {"name": ""}, {"description": "no name"}} {
		result := e.callRaw(alice.ID, "create_address_book", args)
		require.True(t, result.IsError)
		assert.Equal(t, "name is required", result.Content[0].Text)
	}
	listed, _ := e.call(alice.ID, "list_address_books", nil)
	assert.Empty(t, addressBookIDs(listed))
}

func TestUpdateAddressBookChangesOnlyThePassedFields(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	ab := e.newAddressBook(alice.ID, "Friends")
	ab.Description = "Close friends"
	require.NoError(t, e.abRepo.UpdateMetadata(e.ctx(), ab))

	updated, isErr := e.call(alice.ID, "update_address_book", map[string]interface{}{
		"address_book_id": ab.UUID, "name": "Mates",
	})
	require.False(t, isErr, "update_address_book failed: %v", updated["error"])
	assert.Equal(t, true, updated["updated"])
	view := updated["address_book"].(map[string]interface{})
	assert.Equal(t, ab.UUID, view["id"])
	assert.Equal(t, "Mates", view["name"])
	assert.Equal(t, "Close friends", view["description"], "an omitted field keeps its value")

	_, isErr = e.call(alice.ID, "update_address_book", map[string]interface{}{
		"address_book_id": ab.UUID, "description": "",
	})
	require.False(t, isErr)
	stored, err := e.abRepo.GetByUUID(e.ctx(), ab.UUID)
	require.NoError(t, err)
	assert.Equal(t, "Mates", stored.Name, "the earlier rename survives a later partial update")
	assert.Equal(t, "", stored.Description, "an explicit empty string clears the description")
}

func TestAddressBookCollectionToolsAreOwnerOnly(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	e.newAddressBook(alice.ID, "Alice 1")
	e.newAddressBook(alice.ID, "Alice 2")

	private := e.newAddressBook(bob.ID, "Bob private")
	sharedRW := e.newAddressBook(bob.ID, "Bob shared rw")
	e.shareAddressBook(sharedRW, alice.ID, "read-write")
	sharedRO := e.newAddressBook(bob.ID, "Bob shared ro")
	e.shareAddressBook(sharedRO, alice.ID, "read")

	targets := map[string]string{
		"unknown":           uuid.New().String(),
		"another user's":    private.UUID,
		"shared read-write": sharedRW.UUID,
		"shared read":       sharedRO.UUID,
	}

	var reference string
	for label, id := range targets {
		for _, call := range []struct {
			tool string
			args map[string]interface{}
		}{
			{"update_address_book", map[string]interface{}{"address_book_id": id, "name": "Hijacked"}},
			{"delete_address_book", map[string]interface{}{"address_book_id": id, "confirmation": "DELETE"}},
			// Ownership is resolved before the confirmation, as over REST.
			{"delete_address_book", map[string]interface{}{"address_book_id": id, "confirmation": "nope"}},
		} {
			result := e.callRaw(alice.ID, call.tool, call.args)
			require.True(t, result.IsError, "%s on a %s address book must be refused", call.tool, label)
			text := strings.ReplaceAll(result.Content[0].Text, id, "<id>")
			if reference == "" {
				reference = text
			}
			assert.Equal(t, reference, text,
				"%s on a %s address book must answer exactly like every other non-owned book", call.tool, label)
		}
	}
	assert.Contains(t, reference, "is owned by you")

	for _, ab := range []string{private.UUID, sharedRW.UUID, sharedRO.UUID} {
		stored, err := e.abRepo.GetByUUID(e.ctx(), ab)
		require.NoError(t, err)
		require.NotNil(t, stored, "%s must still exist", ab)
		assert.NotEqual(t, "Hijacked", stored.Name)
	}
}

func TestDeleteAddressBookTakesItsContactsAndShares(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	keep := e.newAddressBook(alice.ID, "Keep")
	doomed := e.newAddressBook(alice.ID, "Doomed")
	e.shareAddressBook(doomed, bob.ID, "read")

	created, isErr := e.call(alice.ID, "create_contact", map[string]interface{}{
		"address_book_id": doomed.UUID, "first_name": "Quentin", "last_name": "Zebrowski",
	})
	require.False(t, isErr)
	contactID := created["contact"].(map[string]interface{})["id"].(string)
	_, isErr = e.call(alice.ID, "create_contact", map[string]interface{}{
		"address_book_id": keep.UUID, "first_name": "Zelda", "last_name": "Zebrowski",
	})
	require.False(t, isErr)

	deleted, isErr := e.call(alice.ID, "delete_address_book", map[string]interface{}{
		"address_book_id": doomed.UUID, "confirmation": "DELETE",
	})
	require.False(t, isErr, "delete_address_book failed: %v", deleted["error"])
	assert.Equal(t, true, deleted["deleted"])
	assert.Equal(t, doomed.UUID, deleted["address_book_id"])

	listed, _ := e.call(alice.ID, "list_address_books", nil)
	assert.NotContains(t, addressBookIDs(listed), doomed.UUID)
	assert.Contains(t, addressBookIDs(listed), keep.UUID)

	assert.True(t, e.callRaw(alice.ID, "get_contacts", map[string]interface{}{"address_book_id": doomed.UUID}).IsError)
	assert.True(t, e.callRaw(alice.ID, "update_contact", map[string]interface{}{"contact_id": contactID, "notes": "x"}).IsError,
		"a contact of a deleted book must not be writable")
	found, isErr := e.call(alice.ID, "search_contacts", map[string]interface{}{"query": "zebrowski"})
	require.False(t, isErr)
	var names []string
	for _, raw := range found["matches"].([]interface{}) {
		names = append(names, raw.(map[string]interface{})["contact"].(map[string]interface{})["name"].(string))
	}
	assert.Equal(t, []string{"Zelda Zebrowski"}, names, "only the contact of the surviving book is still found")

	bobsList, _ := e.call(bob.ID, "list_address_books", nil)
	assert.NotContains(t, addressBookIDs(bobsList), doomed.UUID, "a sharee must lose a deleted book")
}

func TestDeleteAddressBookNeedsTheConfirmation(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	e.newAddressBook(alice.ID, "Keep")
	ab := e.newAddressBook(alice.ID, "Friends")

	for _, confirmation := range []interface{}{nil, "", "delete"} {
		args := map[string]interface{}{"address_book_id": ab.UUID}
		if confirmation != nil {
			args["confirmation"] = confirmation
		}
		result := e.callRaw(alice.ID, "delete_address_book", args)
		require.True(t, result.IsError)
		assert.Equal(t, "please type DELETE to confirm address book deletion", result.Content[0].Text)
	}
	stored, err := e.abRepo.GetByUUID(e.ctx(), ab.UUID)
	require.NoError(t, err)
	assert.NotNil(t, stored)
}

func TestDeleteLastAddressBookIsRefused(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	only := e.newAddressBook(alice.ID, "Only")

	result := e.callRaw(alice.ID, "delete_address_book", map[string]interface{}{
		"address_book_id": only.UUID, "confirmation": "DELETE",
	})
	require.True(t, result.IsError)
	assert.Equal(t, "cannot delete your last address book", result.Content[0].Text)

	stored, err := e.abRepo.GetByUUID(e.ctx(), only.UUID)
	require.NoError(t, err)
	assert.NotNil(t, stored)
}

// --- shared -----------------------------------------------------------------

// TestCollectionToolsDoNotLeakStorageErrors breaks the schema under the use
// cases (the F28 technique) and checks the model gets a generic sentence, not
// the driver's text.
func TestCollectionToolsDoNotLeakStorageErrors(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	e.newCalendar(alice.ID, "Keep")
	cal := e.newCalendar(alice.ID, "Work")
	e.newAddressBook(alice.ID, "Keep")
	ab := e.newAddressBook(alice.ID, "Friends")

	for _, tc := range []struct {
		tool    string
		trigger string
		args    map[string]interface{}
	}{
		{"create_calendar", "BEFORE INSERT ON calendars", map[string]interface{}{"name": "New"}},
		{"update_calendar", "BEFORE UPDATE ON calendars", map[string]interface{}{"calendar_id": cal.UUID, "name": "New"}},
		{"delete_calendar", "BEFORE UPDATE ON calendars", map[string]interface{}{"calendar_id": cal.UUID, "confirmation": "DELETE"}},
		{"create_address_book", "BEFORE INSERT ON address_books", map[string]interface{}{"name": "New"}},
		{"update_address_book", "BEFORE UPDATE ON address_books", map[string]interface{}{"address_book_id": ab.UUID, "name": "New"}},
		{"delete_address_book", "BEFORE UPDATE ON address_books", map[string]interface{}{"address_book_id": ab.UUID, "confirmation": "DELETE"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			require.NoError(t, e.db.Exec("CREATE TRIGGER mcp_injected_failure "+tc.trigger+
				" BEGIN SELECT RAISE(ABORT, 'injected storage failure'); END").Error)
			defer func() { require.NoError(t, e.db.Exec("DROP TRIGGER mcp_injected_failure").Error) }()

			result := e.callRaw(alice.ID, tc.tool, tc.args)
			require.True(t, result.IsError)
			text := result.Content[0].Text
			assert.NotContains(t, text, "injected", "a storage error must not reach the model")
			assert.Contains(t, text, "internal server error")
			assert.NotContains(t, text, "is owned by you", "a storage failure is not a missing collection")
		})
	}
}

func calendarIDs(listed map[string]interface{}) []string {
	var ids []string
	for _, raw := range listed["calendars"].([]interface{}) {
		ids = append(ids, raw.(map[string]interface{})["id"].(string))
	}
	return ids
}

func addressBookIDs(listed map[string]interface{}) []string {
	var ids []string
	for _, raw := range listed["address_books"].([]interface{}) {
		ids = append(ids, raw.(map[string]interface{})["id"].(string))
	}
	return ids
}
