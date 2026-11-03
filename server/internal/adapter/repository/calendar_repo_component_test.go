package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// componentObject stores body (one VEVENT or VTODO, without the VCALENDAR
// wrapper) in cal the way every write path does: denormalized from the
// iCalendar data, then created through the repository.
func componentObject(t *testing.T, repo *repository.CalendarRepository, cal *calendar.Calendar, uid, body string) *calendar.CalendarObject {
	t.Helper()
	objUUID := uuid.New().String()
	obj := &calendar.CalendarObject{
		UUID:       objUUID,
		CalendarID: cal.ID,
		UID:        uid,
		Path:       uid + ".ics",
		ETag:       calendar.NewETag(),
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n" + body +
			"END:VCALENDAR\r\n",
	}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	require.NoError(t, repo.CreateCalendarObject(context.Background(), obj))
	return obj
}

func objectIDs(objs []*calendar.CalendarObject) []uint {
	ids := make([]uint, 0, len(objs))
	for _, o := range objs {
		ids = append(ids, o.ID)
	}
	return ids
}

// TestEventQueriesExcludeTasks is the regression test for #170: a VTODO
// written over CalDAV lives in the same calendar_objects table as the events,
// and ListEvents / SearchEvents used to return it as an event. The task here
// carries a DTSTART inside the window and matches the search text, so only the
// component_type predicate can keep it out.
func TestEventQueriesExcludeTasks(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()
	cal := seedCalendar(t, repo, false)

	event := componentObject(t, repo, cal, "event-170",
		"BEGIN:VEVENT\r\nUID:event-170\r\nDTSTAMP:20300101T000000Z\r\n"+
			"DTSTART:20300310T100000Z\r\nDTEND:20300310T110000Z\r\n"+
			"SUMMARY:Planning review\r\nEND:VEVENT\r\n")
	task := componentObject(t, repo, cal, "task-170",
		"BEGIN:VTODO\r\nUID:task-170\r\nDTSTAMP:20300101T000000Z\r\n"+
			"DTSTART:20300310T090000Z\r\nDUE:20300310T170000Z\r\n"+
			"SUMMARY:Planning review prep\r\nEND:VTODO\r\n")
	recurringTask := componentObject(t, repo, cal, "task-170-weekly",
		"BEGIN:VTODO\r\nUID:task-170-weekly\r\nDTSTAMP:20300101T000000Z\r\n"+
			"DTSTART:20300303T080000Z\r\nDUE:20300303T090000Z\r\nRRULE:FREQ=WEEKLY\r\n"+
			"SUMMARY:Planning review checklist\r\nEND:VTODO\r\n")

	windowStart := time.Date(2030, 3, 10, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2030, 3, 11, 0, 0, 0, 0, time.UTC)

	// Preconditions: the tasks are stored as VTODOs whose denormalized times
	// overlap the window, so the time predicate alone would let them through.
	for _, tk := range []*calendar.CalendarObject{task, recurringTask} {
		require.Equal(t, calendar.ComponentToDo, tk.ComponentType, tk.UID)
		require.NotNil(t, tk.StartTime, tk.UID)
		require.True(t, tk.StartTime.Before(windowEnd), tk.UID)
	}
	require.Equal(t, calendar.ComponentEvent, event.ComponentType)

	t.Run("ListEvents", func(t *testing.T) {
		got, err := repo.ListEvents(ctx, cal.ID, windowStart, windowEnd)
		require.NoError(t, err)
		assert.Equal(t, []uint{event.ID}, objectIDs(got),
			"only the VEVENT may be listed; the VTODOs in the same window are tasks")
	})

	t.Run("SearchEvents", func(t *testing.T) {
		// Pivot before the objects (the "live" query) and after them (the
		// "past" query): both halves of the search must leave the tasks out.
		for _, pivot := range []time.Time{
			time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2030, 6, 1, 0, 0, 0, 0, time.UTC),
		} {
			got, err := repo.SearchEvents(ctx, calendar.EventSearchQuery{
				CalendarIDs: []uint{cal.ID}, Text: "planning review", Pivot: pivot, Limit: 10,
			})
			require.NoError(t, err)
			assert.Equal(t, []uint{event.ID}, objectIDs(got), "pivot %s", pivot)
		}

		// Bounded search over the same window.
		got, err := repo.SearchEvents(ctx, calendar.EventSearchQuery{
			CalendarIDs: []uint{cal.ID}, Text: "planning review",
			Start: &windowStart, End: &windowEnd,
			Pivot: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), Limit: 10,
		})
		require.NoError(t, err)
		assert.Equal(t, []uint{event.ID}, objectIDs(got), "bounded search")
	})

	t.Run("CalDAV reads keep tasks", func(t *testing.T) {
		// GetCalendarObjects backs CalDAV PROPFIND/REPORT, export, the public
		// feed and import duplicate detection: it must still see every object.
		got, err := repo.GetCalendarObjects(ctx, cal.ID)
		require.NoError(t, err)
		assert.ElementsMatch(t, []uint{event.ID, task.ID, recurringTask.ID}, objectIDs(got))
	})
}
