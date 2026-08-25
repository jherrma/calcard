package mcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreeSlotsMergesOverlappingBusyBlocks(t *testing.T) {
	day := func(hour int) time.Time {
		return time.Date(2026, 3, 6, hour, 0, 0, 0, time.UTC)
	}

	// Two overlapping meetings, 09:00–11:00 and 10:00–12:00. Without merging,
	// the naive scan would report a phantom opening between 11:00 and 10:00.
	busy := []interval{
		{Start: day(9), End: day(11)},
		{Start: day(10), End: day(12)},
	}

	slots := freeSlots(busy, day(8), day(17), time.Hour)
	require.Len(t, slots, 2)
	assert.Equal(t, day(8).Format(time.RFC3339), slots[0].Start)
	assert.Equal(t, day(9).Format(time.RFC3339), slots[0].End)
	assert.Equal(t, day(12).Format(time.RFC3339), slots[1].Start)
	assert.Equal(t, day(17).Format(time.RFC3339), slots[1].End)
	assert.Equal(t, 300, slots[1].Minutes)
}

func TestFreeSlotsSkipsGapsShorterThanTheDuration(t *testing.T) {
	day := func(hour, minute int) time.Time {
		return time.Date(2026, 3, 6, hour, minute, 0, 0, time.UTC)
	}
	busy := []interval{
		{Start: day(9, 0), End: day(10, 0)},
		{Start: day(10, 30), End: day(12, 0)}, // only a 30-minute gap
	}

	slots := freeSlots(busy, day(9, 0), day(12, 0), time.Hour)
	assert.Empty(t, slots, "a 30-minute gap must not satisfy a 60-minute request")

	slots = freeSlots(busy, day(9, 0), day(12, 0), 30*time.Minute)
	require.Len(t, slots, 1)
	assert.Equal(t, 30, slots[0].Minutes)
}

func TestFreeSlotsClipsBusyBlocksToTheWindow(t *testing.T) {
	at := func(d, hour int) time.Time {
		return time.Date(2026, 3, d, hour, 0, 0, 0, time.UTC)
	}
	// A meeting that starts the day before and ends inside the window.
	busy := []interval{{Start: at(5, 22), End: at(6, 10)}}

	slots := freeSlots(busy, at(6, 9), at(6, 17), time.Hour)
	require.Len(t, slots, 1)
	assert.Equal(t, at(6, 10).Format(time.RFC3339), slots[0].Start,
		"the overhanging block must consume the start of the window")
}

func TestFindFreeSlotsAcrossCalendars(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	work := e.newCalendar(alice.ID, "Work")
	personal := e.newCalendar(alice.ID, "Personal")

	mustCreate(t, e, alice.ID, work.UUID, "Standup", "2026-03-06T09:00:00Z", "2026-03-06T10:00:00Z")
	mustCreate(t, e, alice.ID, personal.UUID, "Dentist", "2026-03-06T11:00:00Z", "2026-03-06T12:00:00Z")

	payload, isErr := e.call(alice.ID, "find_free_slots", map[string]interface{}{
		"start":            "2026-03-06T09:00:00Z",
		"end":              "2026-03-06T13:00:00Z",
		"duration_minutes": 60,
	})
	require.False(t, isErr, "find_free_slots failed: %v", payload["error"])

	slots := payload["free_slots"].([]interface{})
	require.Len(t, slots, 2, "both calendars must count as busy")
	assert.Equal(t, "2026-03-06T10:00:00Z", slots[0].(map[string]interface{})["start"])
	assert.Equal(t, "2026-03-06T12:00:00Z", slots[1].(map[string]interface{})["start"])
	assert.Len(t, payload["calendars_consulted"], 2)
}

func TestFindFreeSlotsIgnoresAllDayByDefault(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	cal := e.newCalendar(alice.ID, "Work")

	created, isErr := e.call(alice.ID, "create_event", map[string]interface{}{
		"calendar_id": cal.UUID,
		"title":       "Someone's birthday",
		"start":       "2026-03-06T00:00:00Z",
		"end":         "2026-03-07T00:00:00Z",
		"all_day":     true,
	})
	require.False(t, isErr, "create_event failed: %v", created["error"])

	payload, _ := e.call(alice.ID, "find_free_slots", map[string]interface{}{
		"start":            "2026-03-06T09:00:00Z",
		"end":              "2026-03-06T17:00:00Z",
		"duration_minutes": 60,
	})
	require.Len(t, payload["free_slots"], 1,
		"a birthday must not wipe out the whole day")

	payload, _ = e.call(alice.ID, "find_free_slots", map[string]interface{}{
		"start":            "2026-03-06T09:00:00Z",
		"end":              "2026-03-06T17:00:00Z",
		"duration_minutes": 60,
		"include_all_day":  true,
	})
	assert.Empty(t, payload["free_slots"],
		"include_all_day must make the all-day event block the window")
}

func TestFindFreeSlotsRestrictedToNamedCalendars(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	work := e.newCalendar(alice.ID, "Work")
	personal := e.newCalendar(alice.ID, "Personal")

	mustCreate(t, e, alice.ID, personal.UUID, "Dentist", "2026-03-06T11:00:00Z", "2026-03-06T12:00:00Z")

	payload, _ := e.call(alice.ID, "find_free_slots", map[string]interface{}{
		"start":            "2026-03-06T09:00:00Z",
		"end":              "2026-03-06T13:00:00Z",
		"duration_minutes": 60,
		"calendar_ids":     []string{work.UUID},
	})
	slots := payload["free_slots"].([]interface{})
	require.Len(t, slots, 1, "the personal calendar was excluded, so the window is wide open")
	assert.Equal(t, 240, int(slots[0].(map[string]interface{})["minutes"].(float64)))
	assert.Equal(t, []interface{}{"Work"}, payload["calendars_consulted"])
}

func TestFindFreeSlotsRejectsUnreadableCalendarIDs(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")
	bob := e.newUser("bob@example.com")
	e.newCalendar(alice.ID, "Work")
	bobs := e.newCalendar(bob.ID, "Bob private")

	// Answering "the whole window is free" here would be a confidently wrong
	// answer built on having consulted nothing.
	result := e.callRaw(alice.ID, "find_free_slots", map[string]interface{}{
		"start":            "2026-03-06T09:00:00Z",
		"end":              "2026-03-06T13:00:00Z",
		"duration_minutes": 60,
		"calendar_ids":     []string{bobs.UUID},
	})
	require.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "readable")
}

func TestFindFreeSlotsValidatesTheWindow(t *testing.T) {
	e := newTestEnv(t)
	alice := e.newUser("alice@example.com")

	for name, args := range map[string]map[string]interface{}{
		"end before start": {
			"start": "2026-03-06T13:00:00Z", "end": "2026-03-06T09:00:00Z", "duration_minutes": 60,
		},
		"zero duration": {
			"start": "2026-03-06T09:00:00Z", "end": "2026-03-06T13:00:00Z", "duration_minutes": 0,
		},
		"duration exceeds window": {
			"start": "2026-03-06T09:00:00Z", "end": "2026-03-06T10:00:00Z", "duration_minutes": 120,
		},
		"window too wide": {
			"start": "2026-03-06T09:00:00Z", "end": "2027-03-06T09:00:00Z", "duration_minutes": 60,
		},
	} {
		result := e.callRaw(alice.ID, "find_free_slots", args)
		assert.True(t, result.IsError, "%s must be refused", name)
	}
}
