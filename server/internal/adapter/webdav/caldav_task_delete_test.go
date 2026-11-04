package webdav

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// seedTaskDeleteUser creates a user name (password "password") with one mixed
// (VEVENT,VTODO) calendar at /dav/<name>/calendars/mixed/.
func seedTaskDeleteUser(t *testing.T, g *gorm.DB, name string) (*user.User, *calendar.Calendar) {
	t.Helper()
	ctx := context.Background()
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	u := &user.User{UUID: name + "-uuid", Email: name + "@example.com", Username: name, PasswordHash: string(hash), IsActive: true}
	require.NoError(t, repository.NewUserRepository(g).Create(ctx, u))
	cal := &calendar.Calendar{UserID: u.ID, UUID: name + "-cal", Path: "mixed", Name: "Mixed", Timezone: "UTC",
		Color: "#336699", SupportedComponents: "VEVENT,VTODO"}
	require.NoError(t, repository.NewCalendarRepository(g).Create(ctx, cal))
	return u, cal
}

func taskDeleteTodo(uid string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//T//EN\r\nBEGIN:VTODO\r\nUID:" + uid +
		"\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:" + uid + "\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
}

// TestCalDAVDeleteTaskIsHard (#170, user decision after story 23): a DAV
// client's DELETE of a task (a VTODO object) removes the row for good, exactly
// like the REST task DELETE — not even an Unscoped read finds it — and records
// the removal the same way: one "deleted" change-log row carrying the
// calendar's new sync token (= ctag), which a sync-collection REPORT answers
// with a 404 for the href. A second DELETE is a 404 and records nothing. An
// event's DELETE keeps the soft delete (a deleted_at tombstone), unchanged.
func TestCalDAVDeleteTaskIsHard(t *testing.T) {
	app, db, _ := setupTestApp(t)
	defer db.Close()
	ctx := context.Background()
	g := db.DB()
	calRepo := repository.NewCalendarRepository(g)
	_, cal := seedTaskDeleteUser(t, g, "taskdel")
	const base = "/dav/taskdel/calendars/mixed/"
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("taskdel@example.com:password"))

	do := func(method, p, depth, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, p, strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		if depth != "" {
			req.Header.Set("Depth", depth)
		}
		if method == "PUT" {
			req.Header.Set("Content-Type", "text/calendar")
		} else if body != "" {
			req.Header.Set("Content-Type", "application/xml")
		}
		resp := davDo(t, app, req)
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	put := func(name, body string) {
		t.Helper()
		status, resp := do("PUT", base+name, "", body)
		require.Equalf(t, http.StatusCreated, status, "PUT %s: %s", name, resp)
	}
	put("task.ics", taskDeleteTodo("task"))
	put("keep.ics", taskDeleteTodo("keep"))
	put("event.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//T//EN\r\nBEGIN:VEVENT\r\nUID:event\r\n"+
		"DTSTAMP:20300101T000000Z\r\nDTSTART:20300301T100000Z\r\nDTEND:20300301T110000Z\r\nSUMMARY:Ev\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")

	// row reads the object at name, soft-deleted rows included.
	row := func(name string) (calendar.CalendarObject, bool) {
		t.Helper()
		var objs []calendar.CalendarObject
		require.NoError(t, g.Unscoped().Where("calendar_id = ? AND path = ?", cal.ID, name).Find(&objs).Error)
		require.LessOrEqual(t, len(objs), 1)
		if len(objs) == 0 {
			return calendar.CalendarObject{}, false
		}
		return objs[0], true
	}
	countChanges := func() int64 {
		t.Helper()
		var n int64
		require.NoError(t, g.Model(&calendar.SyncChangeLog{}).Where("calendar_id = ?", cal.ID).Count(&n).Error)
		return n
	}
	// sync runs a sync-collection REPORT from token and returns the members it
	// reports (last href segment -> reported as removed) and the new token.
	sync := func(token string) (map[string]bool, string) {
		t.Helper()
		status, body := do("REPORT", base, "1", `<?xml version="1.0" encoding="utf-8" ?>
<D:sync-collection xmlns:D="DAV:"><D:sync-token>`+token+`</D:sync-token><D:sync-level>1</D:sync-level><D:prop><D:getetag/></D:prop></D:sync-collection>`)
		require.Equalf(t, http.StatusMultiStatus, status, "sync-collection: %s", body)
		var ms SyncMultiStatus
		require.NoError(t, xml.Unmarshal([]byte(body), &ms))
		out := map[string]bool{}
		for _, r := range ms.Responses {
			href, err := url.PathUnescape(r.Href)
			require.NoError(t, err)
			out[path.Base(href)] = strings.Contains(r.Status, "404")
		}
		return out, ms.SyncToken
	}

	t.Run("a task is deleted for good and the removal recorded once", func(t *testing.T) {
		_, token := sync("")
		before, err := calRepo.GetByID(ctx, cal.ID)
		require.NoError(t, err)
		changesBefore := countChanges()
		obj, found := row("task.ics")
		require.True(t, found)
		require.True(t, obj.IsTask(), "seed: the object is a VTODO")

		status, body := do("DELETE", base+"task.ics", "", "")
		require.Equal(t, http.StatusNoContent, status, body)

		_, found = row("task.ics")
		assert.False(t, found, "no row left, not even a soft-deleted one")
		kept, found := row("keep.ics")
		require.True(t, found)
		assert.False(t, kept.DeletedAt.Valid, "the other task is untouched")

		after, err := calRepo.GetByID(ctx, cal.ID)
		require.NoError(t, err)
		assert.NotEqual(t, before.SyncToken, after.SyncToken, "the sync token advances")
		assert.Equal(t, after.SyncToken, after.CTag)
		assert.Equal(t, changesBefore+1, countChanges(), "one change-log row")
		changes, err := calRepo.GetChangesSinceToken(ctx, cal.ID, before.SyncToken)
		require.NoError(t, err)
		require.Len(t, changes, 1)
		assert.Equal(t, "deleted", changes[0].ChangeType)
		assert.Equal(t, "task.ics", changes[0].ResourcePath)
		assert.Equal(t, "task", changes[0].ResourceUID)
		assert.Equal(t, after.SyncToken, changes[0].SyncToken)

		members, token := sync(token)
		assert.Equal(t, map[string]bool{"task.ics": true}, members, "sync-collection reports the removal as a 404")

		status, _ = do("GET", base+"task.ics", "", "")
		assert.Equal(t, http.StatusNotFound, status)
		status, _ = do("DELETE", base+"task.ics", "", "")
		assert.Equal(t, http.StatusNotFound, status, "a second DELETE")
		assert.Equal(t, changesBefore+1, countChanges(), "the second DELETE records nothing")
		members, _ = sync(token)
		assert.Empty(t, members, "the removal is reported once")

		// A client may create a task at the same href again.
		put("task.ics", taskDeleteTodo("task"))
		again, found := row("task.ics")
		require.True(t, found)
		assert.NotEqual(t, obj.ID, again.ID, "a new row")
	})

	t.Run("an event keeps the soft delete", func(t *testing.T) {
		_, token := sync("")
		changesBefore := countChanges()
		obj, found := row("event.ics")
		require.True(t, found)
		require.True(t, obj.IsEvent(), "seed: the object is a VEVENT")

		status, body := do("DELETE", base+"event.ics", "", "")
		require.Equal(t, http.StatusNoContent, status, body)

		tomb, found := row("event.ics")
		require.True(t, found, "the event's row stays as a tombstone")
		assert.True(t, tomb.DeletedAt.Valid, "soft-deleted: deleted_at is set")
		assert.Equal(t, changesBefore+1, countChanges(), "one change-log row")
		status, _ = do("GET", base+"event.ics", "", "")
		assert.Equal(t, http.StatusNotFound, status)
		members, _ := sync(token)
		assert.Equal(t, map[string]bool{"event.ics": true}, members, "sync-collection reports the removal as a 404")
	})
}

// lostRaceRepo hands the backend the object it looks up, but lets a concurrent
// delete (win) remove it first, so the backend's own delete finds nothing.
type lostRaceRepo struct {
	calendar.CalendarRepository
	win func(obj *calendar.CalendarObject)
}

func (r *lostRaceRepo) GetCalendarObjectByPath(ctx context.Context, calendarID uint, p string) (*calendar.CalendarObject, error) {
	obj, err := r.CalendarRepository.GetCalendarObjectByPath(ctx, calendarID, p)
	if err == nil && obj != nil {
		r.win(obj)
	}
	return obj, err
}

// TestCalDAVDeleteTaskLostRace: a DAV DELETE of a task whose row a concurrent
// delete removed between the lookup and the delete answers 404, as the REST
// task DELETE does, and records no second removal — only the winner's.
func TestCalDAVDeleteTaskLostRace(t *testing.T) {
	_, db, _ := setupTestApp(t)
	defer db.Close()
	ctx := context.Background()
	g := db.DB()
	calRepo := repository.NewCalendarRepository(g)
	u, cal := seedTaskDeleteUser(t, g, "racer")

	obj := &calendar.CalendarObject{UUID: "race-uuid", CalendarID: cal.ID, Path: "race.ics", UID: "race", ETag: "e1",
		ICalData: taskDeleteTodo("race")}
	require.NoError(t, obj.PopulateDenormFieldsFromICal())
	require.True(t, obj.IsTask())
	require.NoError(t, calRepo.CreateCalendarObject(ctx, obj))

	countChanges := func() int64 {
		t.Helper()
		var n int64
		require.NoError(t, g.Model(&calendar.SyncChangeLog{}).Where("calendar_id = ?", cal.ID).Count(&n).Error)
		return n
	}
	changesBefore := countChanges()

	racing := &lostRaceRepo{CalendarRepository: calRepo, win: func(o *calendar.CalendarObject) {
		require.NoError(t, calRepo.HardDeleteCalendarObject(ctx, o), "the concurrent delete wins")
	}}
	backend := NewCalDAVBackend(racing, repository.NewUserRepository(g), repository.NewCalendarShareRepository(g))

	err := backend.DeleteCalendarObject(WithUser(ctx, u), "/dav/racer/calendars/mixed/race.ics")
	require.Error(t, err)
	code, ok := davErrorStatus(err)
	require.True(t, ok, "an HTTP error, not a storage error: %v", err)
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, changesBefore+1, countChanges(), "only the winner's removal is recorded")

	var rows int64
	require.NoError(t, g.Unscoped().Model(&calendar.CalendarObject{}).Where("id = ?", obj.ID).Count(&rows).Error)
	assert.Zero(t, rows)
}
