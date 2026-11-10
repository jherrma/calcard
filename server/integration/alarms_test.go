//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #169 story 09: a REST scope=this edit of a DAV-written series keeps the
// series' relative VALARM on the new override and leaves the absolute one with
// the master.
func TestRestScopeThisKeepsRelativeAlarm(t *testing.T) {
	email := "alarm-split@example.test"
	password := "alarmSplit!123"
	token, username := registerAndLoginFull(t, email, password, "Alarm Split")
	_, appPassword := createAppPassword(t, token, "alarm-split")

	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok)
	collection := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/"

	series := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Integration Tests//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:alarm169-series\r\nDTSTAMP:20340101T000000Z\r\n" +
		"DTSTART:20340102T090000Z\r\nDTEND:20340102T100000Z\r\nRRULE:FREQ=WEEKLY;COUNT=6\r\nSUMMARY:Weekly sync\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Soon\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Once\r\nTRIGGER;VALUE=DATE-TIME:20340103T080000Z\r\nEND:VALARM\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	status, _, body := davCall(t, "PUT", collection+"alarm169-series.ics", email, appPassword, series,
		map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent}, status, "PUT: %s", string(body))

	var list struct {
		Events []struct {
			ID  string `json:"id"`
			UID string `json:"uid"`
		} `json:"events"`
	}
	code := doJSONRaw(t, http.MethodGet, "/calendars/"+personal.UUID+"/events/?start=2034-01-01T00:00:00Z&end=2034-03-01T00:00:00Z&expand=false", token, nil, &list)
	require.Equal(t, http.StatusOK, code)
	id := ""
	for _, ev := range list.Events {
		if ev.UID == "alarm169-series" {
			id = ev.ID
		}
	}
	require.NotEmpty(t, id)

	st, raw := restCall(t, http.MethodPatch,
		"/calendars/"+personal.UUID+"/events/"+id+"?scope=this&recurrence_id=20340109T090000Z", token,
		map[string]any{"summary": "Edited once"})
	require.Equal(t, http.StatusOK, st, string(raw))

	st, _, got := davCall(t, "GET", collection+"alarm169-series.ics", email, appPassword, "", nil)
	require.Equal(t, http.StatusOK, st)
	ics := strings.ReplaceAll(string(got), "\r\n ", "")
	assert.Equal(t, 3, strings.Count(ics, "BEGIN:VALARM"))
	assert.Equal(t, 2, strings.Count(ics, "TRIGGER:-PT15M"), "override carries the relative alarm")
	assert.Equal(t, 1, strings.Count(ics, "VALUE=DATE-TIME"), "absolute alarm stays with the master only")
}

// #169 story 10: a DAV-written event with a relative, an absolute and an EMAIL
// VALARM reports them as `reminders` on GET one and in the expanded list.
func TestRestEventRemindersFromDav(t *testing.T) {
	email := "alarm-read@example.test"
	password := "alarmRead!123"
	token, username := registerAndLoginFull(t, email, password, "Alarm Read")
	_, appPassword := createAppPassword(t, token, "alarm-read")

	idx := listCalendarsIndex(t, token)
	personal, ok := idx["Personal"]
	require.True(t, ok)
	collection := "/dav/" + username + "/calendars/" + personal.UUID + ".ics/"

	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Integration Tests//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:alarm169-read\r\nDTSTAMP:20340101T000000Z\r\n" +
		"DTSTART:20340104T090000Z\r\nDTEND:20340104T100000Z\r\nSUMMARY:Reminders\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20340104T080000Z\r\nEND:VALARM\r\n" +
		"BEGIN:VALARM\r\nACTION:EMAIL\r\nTRIGGER:-P1D\r\nEND:VALARM\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	status, _, body := davCall(t, "PUT", collection+"alarm169-read.ics", email, appPassword, ics,
		map[string]string{"Content-Type": "text/calendar; charset=utf-8"})
	require.Containsf(t, []int{http.StatusCreated, http.StatusNoContent}, status, "PUT: %s", string(body))

	want := `[{"index":0,"action":"display","related":"start","offset_seconds":-900,"at":null,"editable":true},
	{"index":1,"action":"display","related":null,"offset_seconds":null,"at":"2034-01-04T08:00:00Z","editable":true},
	{"index":2,"action":"email","related":"start","offset_seconds":-86400,"at":null,"editable":false}]`

	type listed struct {
		Events []struct {
			ID        string          `json:"id"`
			UID       string          `json:"uid"`
			Reminders json.RawMessage `json:"reminders"`
		} `json:"events"`
	}
	for _, expand := range []string{"true", "false"} {
		var list listed
		code := doJSONRaw(t, http.MethodGet, "/calendars/"+personal.UUID+"/events/?start=2034-01-01T00:00:00Z&end=2034-02-01T00:00:00Z&expand="+expand, token, nil, &list)
		require.Equal(t, http.StatusOK, code)
		id := ""
		for _, ev := range list.Events {
			if ev.UID == "alarm169-read" {
				id = ev.ID
				assert.JSONEq(t, want, string(ev.Reminders), "list expand="+expand)
			}
		}
		require.NotEmpty(t, id)

		var one struct {
			Reminders json.RawMessage `json:"reminders"`
		}
		code = doJSONRaw(t, http.MethodGet, "/calendars/"+personal.UUID+"/events/"+id, token, nil, &one)
		require.Equal(t, http.StatusOK, code)
		assert.JSONEq(t, want, string(one.Reminders), "get")
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/alarms/upcoming (#169 story 06)
// ---------------------------------------------------------------------------

type upcomingAlarm struct {
	Key           string  `json:"key"`
	TriggerAt     string  `json:"trigger_at"`
	UID           string  `json:"uid"`
	RecurrenceID  *string `json:"recurrence_id"`
	AlarmIndex    int     `json:"alarm_index"`
	Action        string  `json:"action"`
	Description   *string `json:"description"`
	ObjectType    string  `json:"object_type"`
	ObjectID      string  `json:"object_id"`
	CalendarID    uint    `json:"calendar_id"`
	CalendarUUID  string  `json:"calendar_uuid"`
	CalendarName  string  `json:"calendar_name"`
	CalendarColor string  `json:"calendar_color"`
	Summary       string  `json:"summary"`
	Location      string  `json:"location"`
	AllDay        bool    `json:"all_day"`
	Start         *string `json:"start"`
	StartDate     *string `json:"start_date"`
	End           *string `json:"end"`
	Due           *string `json:"due"`
	DueDate       *string `json:"due_date"`
}

type upcomingBody struct {
	Alarms     []upcomingAlarm `json:"alarms"`
	Count      int             `json:"count"`
	From       string          `json:"from"`
	Until      string          `json:"until"`
	ServerTime string          `json:"server_time"`
	Truncated  bool            `json:"truncated"`
}

// getUpcoming calls GET /alarms/upcoming with the raw query string.
func getUpcoming(t *testing.T, token, query string) (int, upcomingBody, []byte) {
	t.Helper()
	path := "/alarms/upcoming"
	if query != "" {
		path += "?" + query
	}
	status, raw := restCall(t, http.MethodGet, path, token, nil)
	var out upcomingBody
	if status == http.StatusOK {
		require.NoErrorf(t, json.Unmarshal(raw, &out), "decode: %s", raw)
	}
	return status, out, raw
}

// upcomingUntil asks for alarms until now+3d (RFC 3339 in UTC, no "+").
func upcomingUntil(t *testing.T, token string) upcomingBody {
	t.Helper()
	until := time.Now().UTC().Add(72 * time.Hour).Format(time.RFC3339)
	status, body, raw := getUpcoming(t, token, "until="+until)
	require.Equalf(t, http.StatusOK, status, "upcoming: %s", raw)
	return body
}

func alarmsOfUID(body upcomingBody, uid string) []upcomingAlarm {
	var out []upcomingAlarm
	for _, a := range body.Alarms {
		if a.UID == uid {
			out = append(out, a)
		}
	}
	return out
}

func icsStamp(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

func rfcStamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func displayAlarm(trigger string) string {
	return "BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Reminder\r\n" + trigger + "\r\nEND:VALARM\r\n"
}

func vevent(uid string, props ...string) string {
	return "BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20260101T000000Z\r\n" + strings.Join(props, "") + "END:VEVENT\r\n"
}

// eventCollection is the DAV collection of the user's "Personal" calendar.
func eventCollection(t *testing.T, token, username string) (collection, calUUID string) {
	t.Helper()
	personal, ok := listCalendarsIndex(t, token)["Personal"]
	require.True(t, ok)
	return "/dav/" + username + "/calendars/" + personal.UUID + ".ics/", personal.UUID
}

func setTimezoneDefault(t *testing.T, token, zone string) {
	t.Helper()
	status, raw := restCall(t, http.MethodPut, "/users/me/preferences/timezone-default", token, map[string]string{"timezone": zone})
	require.Equalf(t, http.StatusOK, status, "timezone-default: %s", raw)
}

func TestAlarmsUpcoming(t *testing.T) {
	email := "alarm-upcoming@example.test"
	token, username := registerAndLoginFull(t, email, "alarmUpcoming!123", "Alarm Upcoming")
	_, appPassword := createAppPassword(t, token, "alarm-upcoming")
	collection, calUUID := eventCollection(t, token, username)
	put := func(name, ical string) {
		davPut(t, collection+name+".ics", email, appPassword, vcalendar(ical))
	}
	now := time.Now().UTC().Truncate(time.Second)

	t.Run("a relative alarm is listed with every key", func(t *testing.T) {
		start := now.Add(time.Hour)
		put("basic", vevent("alarm169-basic",
			"DTSTART:"+icsStamp(start)+"\r\n", "DTEND:"+icsStamp(start.Add(30*time.Minute))+"\r\n",
			"SUMMARY:Team sync\r\n", "LOCATION:Room 2\r\n", displayAlarm("TRIGGER:-PT15M")))
		status, body, raw := getUpcoming(t, token, "")
		require.Equalf(t, http.StatusOK, status, "%s", raw)

		got := alarmsOfUID(body, "alarm169-basic")
		require.Len(t, got, 1)
		a := got[0]
		assert.Equal(t, rfcStamp(start.Add(-15*time.Minute)), a.TriggerAt)
		assert.Regexp(t, `^[0-9a-f]{32}$`, a.Key)
		assert.Nil(t, a.RecurrenceID)
		assert.Equal(t, 0, a.AlarmIndex)
		assert.Equal(t, "display", a.Action)
		require.NotNil(t, a.Description)
		assert.Equal(t, "Reminder", *a.Description)
		assert.Equal(t, "event", a.ObjectType)
		assert.NotEmpty(t, a.ObjectID)
		assert.NotZero(t, a.CalendarID)
		assert.Equal(t, calUUID, a.CalendarUUID)
		assert.Equal(t, "Personal", a.CalendarName)
		assert.NotEmpty(t, a.CalendarColor)
		assert.Equal(t, "Team sync", a.Summary)
		assert.Equal(t, "Room 2", a.Location)
		assert.False(t, a.AllDay)
		require.NotNil(t, a.Start)
		assert.Equal(t, rfcStamp(start), *a.Start)
		require.NotNil(t, a.End)
		assert.Equal(t, rfcStamp(start.Add(30*time.Minute)), *a.End)
		assert.Nil(t, a.StartDate)
		assert.Nil(t, a.Due)
		assert.Nil(t, a.DueDate)
		assert.Equal(t, body.Count, len(body.Alarms))

		// Every nullable key is present on the wire, even when null.
		var wire struct {
			Alarms []map[string]json.RawMessage `json:"alarms"`
		}
		require.NoError(t, json.Unmarshal(raw, &wire))
		for _, key := range []string{"key", "trigger_at", "uid", "recurrence_id", "alarm_index", "action", "description",
			"object_type", "object_id", "calendar_id", "calendar_uuid", "calendar_name", "calendar_color", "summary",
			"location", "all_day", "start", "start_date", "end", "due", "due_date"} {
			assert.Containsf(t, wire.Alarms[0], key, "key %q is absent", key)
		}
		assertNoInternalIDs(t, "GET /alarms/upcoming", raw)
	})

	t.Run("the window and until", func(t *testing.T) {
		status, body, raw := getUpcoming(t, token, "")
		require.Equalf(t, http.StatusOK, status, "%s", raw)
		server, err := time.Parse(time.RFC3339, body.ServerTime)
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now(), server, 5*time.Second)
		assert.Equal(t, rfcStamp(server.Add(-24*time.Hour)), body.From)
		assert.Equal(t, rfcStamp(server.Add(24*time.Hour)), body.Until)

		// Beyond 7 days it is clamped and the answer shows the value used.
		far := now.Add(30 * 24 * time.Hour).Format(time.RFC3339)
		status, body, raw = getUpcoming(t, token, "until="+far)
		require.Equalf(t, http.StatusOK, status, "%s", raw)
		server, _ = time.Parse(time.RFC3339, body.ServerTime)
		assert.Equal(t, rfcStamp(server.Add(7*24*time.Hour)), body.Until)

		// Before from: an empty list, not an error.
		past := now.Add(-72 * time.Hour).Format(time.RFC3339)
		status, _, raw = getUpcoming(t, token, "until="+past)
		require.Equal(t, http.StatusOK, status)
		assert.Contains(t, string(raw), `"alarms":[]`)
		assert.Contains(t, string(raw), `"count":0`)

		// An offset is accepted when it is encoded.
		berlin := now.In(time.FixedZone("x", 3600)).Add(2 * time.Hour).Format(time.RFC3339)
		status, _, raw = getUpcoming(t, token, "until="+url.QueryEscape(berlin))
		assert.Equalf(t, http.StatusOK, status, "%s", raw)
	})

	t.Run("a bad until is a 400", func(t *testing.T) {
		for _, q := range []string{
			"until=2034-01-01T10:00:00",       // no offset
			"until=2034-01-01",                // a date
			"until=tomorrow",                  // nonsense
			"until=2034-01-01T10:00:00+01:00", // unencoded "+" arrives as a space
		} {
			status, _, raw := getUpcoming(t, token, q)
			assert.Equalf(t, http.StatusBadRequest, status, "%s: %s", q, raw)
			assert.Equal(t, "Invalid 'until' value", errorMessage(raw), q)
		}
	})

	t.Run("a daily series with an override and an EXDATE", func(t *testing.T) {
		d0 := now.Add(-12 * time.Hour)
		d1, d2, d3 := d0.Add(24*time.Hour), d0.Add(48*time.Hour), d0.Add(72*time.Hour)
		put("series", vevent("alarm169-series2",
			"DTSTART:"+icsStamp(d0)+"\r\n", "DTEND:"+icsStamp(d0.Add(time.Hour))+"\r\n",
			"RRULE:FREQ=DAILY;COUNT=5\r\n", "EXDATE:"+icsStamp(d1)+"\r\n", "SUMMARY:Daily\r\n",
			displayAlarm("TRIGGER:-PT10M"))+
			vevent("alarm169-series2",
				"RECURRENCE-ID:"+icsStamp(d2)+"\r\n",
				"DTSTART:"+icsStamp(d2.Add(time.Hour))+"\r\n", "DTEND:"+icsStamp(d2.Add(2*time.Hour))+"\r\n",
				"SUMMARY:Daily moved\r\n", displayAlarm("TRIGGER:-PT5M")))
		body := upcomingUntil(t, token)
		got := alarmsOfUID(body, "alarm169-series2")
		var triggers []string
		for _, a := range got {
			triggers = append(triggers, a.TriggerAt)
			require.NotNil(t, a.RecurrenceID, "an occurrence carries its recurrence id")
		}
		assert.Equal(t, []string{
			rfcStamp(d0.Add(-10 * time.Minute)),
			rfcStamp(d2.Add(time.Hour - 5*time.Minute)), // the override's own alarm
			rfcStamp(d3.Add(-10 * time.Minute)),
		}, triggers, "one per occurrence, none for the EXDATE")
		assert.Equal(t, "Daily moved", got[1].Summary)
	})

	t.Run("a task alarm follows completion", func(t *testing.T) {
		list := createTaskCalendar(t, token, "Alarm tasks", "tasks")
		due := now.Add(2 * time.Hour)
		davPut(t, "/dav/"+username+"/calendars/"+list.Path+"/alarm-task.ics", email, appPassword, vcalendar(ics(
			"BEGIN:VTODO", "UID:alarm169-task", "DTSTAMP:20260101T000000Z", "SUMMARY:Pay rent",
			"DUE:"+icsStamp(due),
			"BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:Due soon", "TRIGGER;RELATED=END:-PT30M", "END:VALARM",
			"END:VTODO")))
		got := alarmsOfUID(upcomingUntil(t, token), "alarm169-task")
		require.Len(t, got, 1)
		assert.Equal(t, rfcStamp(due.Add(-30*time.Minute)), got[0].TriggerAt)
		assert.Equal(t, "task", got[0].ObjectType)
		require.NotNil(t, got[0].Due)
		assert.Equal(t, rfcStamp(due), *got[0].Due)
		assert.Nil(t, got[0].Start)

		task := findTaskByUID(t, token, list.UUID, "alarm169-task")
		status, raw := restCall(t, http.MethodPost, taskURL(list.UUID, task.ID)+"/complete", token, nil)
		require.Equalf(t, http.StatusOK, status, "%s", raw)
		assert.Empty(t, alarmsOfUID(upcomingUntil(t, token), "alarm169-task"), "a completed task has no alarm")

		status, raw = restCall(t, http.MethodPost, taskURL(list.UUID, task.ID)+"/uncomplete", token, nil)
		require.Equalf(t, http.StatusOK, status, "%s", raw)
		assert.Len(t, alarmsOfUID(upcomingUntil(t, token), "alarm169-task"), 1, "an uncompleted task is back")
	})

	t.Run("same instant is one alarm, ACKNOWLEDGED stays, EMAIL is dropped", func(t *testing.T) {
		start := now.Add(3 * time.Hour)
		put("dup", vevent("alarm169-dup",
			"DTSTART:"+icsStamp(start)+"\r\n", "DTEND:"+icsStamp(start.Add(time.Hour))+"\r\n",
			displayAlarm("TRIGGER:-PT15M"),
			displayAlarm("TRIGGER;VALUE=DATE-TIME:"+icsStamp(start.Add(-15*time.Minute))),
			"BEGIN:VALARM\r\nACTION:EMAIL\r\nSUMMARY:x\r\nDESCRIPTION:x\r\nTRIGGER:-PT45M\r\nEND:VALARM\r\n",
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Seen\r\nTRIGGER:-PT1H\r\nACKNOWLEDGED:"+icsStamp(now)+"\r\nEND:VALARM\r\n"))
		got := alarmsOfUID(upcomingUntil(t, token), "alarm169-dup")
		var triggers []string
		for _, a := range got {
			triggers = append(triggers, a.TriggerAt)
		}
		assert.Equal(t, []string{rfcStamp(start.Add(-time.Hour)), rfcStamp(start.Add(-15 * time.Minute))}, triggers)
	})

	t.Run("the same UID in two readable calendars is listed once, from the lower id", func(t *testing.T) {
		_, secondUUID := createCalendar(t, token, "Second", "#112233")
		secondPath := calendarPath(t, token, secondUUID)
		start := now.Add(4 * time.Hour)
		ev := vcalendar(vevent("alarm169-twice",
			"DTSTART:"+icsStamp(start)+"\r\n", "DTEND:"+icsStamp(start.Add(time.Hour))+"\r\n", displayAlarm("TRIGGER:-PT10M")))
		davPut(t, "/dav/"+username+"/calendars/"+secondPath+"/twice.ics", email, appPassword, ev)
		put("twice", vevent("alarm169-twice",
			"DTSTART:"+icsStamp(start)+"\r\n", "DTEND:"+icsStamp(start.Add(time.Hour))+"\r\n", displayAlarm("TRIGGER:-PT10M")))
		got := alarmsOfUID(upcomingUntil(t, token), "alarm169-twice")
		require.Len(t, got, 1)
		assert.Equal(t, "Personal", got[0].CalendarName, "Personal was created first, so it has the lower id")
	})

	t.Run("an object over the 500 instance cap sets truncated", func(t *testing.T) {
		start := now.Add(time.Hour)
		put("flood", vevent("alarm169-flood",
			"DTSTART:"+icsStamp(start)+"\r\n", "DTEND:"+icsStamp(start.Add(time.Minute))+"\r\n",
			"RRULE:FREQ=MINUTELY;INTERVAL=5\r\n", displayAlarm("TRIGGER:-PT1M")))
		body := upcomingUntil(t, token)
		assert.True(t, body.Truncated)
		assert.LessOrEqual(t, len(body.Alarms), 500)
		assert.NotEmpty(t, alarmsOfUID(body, "alarm169-flood"))
	})
}

func TestAlarmsUpcomingRequiresAuth(t *testing.T) {
	status, _ := restCall(t, http.MethodGet, "/alarms/upcoming", "", nil)
	assert.Equal(t, http.StatusUnauthorized, status)
}

// An all-day event with TRIGGER:PT9H resolves in the calendar owner's zone:
// UTC until the owner has a time zone, then the zone.
func TestAlarmsUpcomingAllDayOwnerZone(t *testing.T) {
	email := "alarm-allday@example.test"
	token, username := registerAndLoginFull(t, email, "alarmAllDay!123", "Alarm All Day")
	_, appPassword := createAppPassword(t, token, "alarm-allday")
	collection, _ := eventCollection(t, token, username)

	day := time.Now().UTC().AddDate(0, 0, 1)
	date := day.Format("20060102")
	davPut(t, collection+"allday.ics", email, appPassword, vcalendar(vevent("alarm169-allday",
		"DTSTART;VALUE=DATE:"+date+"\r\n", "DTEND;VALUE=DATE:"+day.AddDate(0, 0, 1).Format("20060102")+"\r\n",
		"SUMMARY:Holiday\r\n", displayAlarm("TRIGGER:PT9H"))))

	got := alarmsOfUID(upcomingUntil(t, token), "alarm169-allday")
	require.Len(t, got, 1)
	assert.Equal(t, time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, time.UTC).Format(time.RFC3339), got[0].TriggerAt)
	assert.True(t, got[0].AllDay)
	require.NotNil(t, got[0].StartDate)
	assert.Equal(t, day.Format(time.DateOnly), *got[0].StartDate)
	assert.Nil(t, got[0].Start)
	oldKey := got[0].Key

	setTimezoneDefault(t, token, "Europe/Berlin")
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	got = alarmsOfUID(upcomingUntil(t, token), "alarm169-allday")
	require.Len(t, got, 1)
	assert.Equal(t, time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, berlin).UTC().Format(time.RFC3339), got[0].TriggerAt)
	assert.NotEqual(t, oldKey, got[0].Key, "a zone-dependent alarm has a new key when its instant moves")
}

func TestAlarmsSharing(t *testing.T) {
	password := "alarmSharing!123"
	ownerEmail := "alarm-share-owner@example.test"
	ownerToken, ownerName := registerAndLoginFull(t, ownerEmail, password, "Alarm Share Owner")
	_, ownerApp := createAppPassword(t, ownerToken, "alarm-share-owner")
	readerEmail, rwEmail := "alarm-share-reader@example.test", "alarm-share-rw@example.test"
	readerToken := registerAndLogin(t, readerEmail, password, "Alarm Reader")
	rwToken := registerAndLogin(t, rwEmail, password, "Alarm Writer")
	outsiderToken := registerAndLogin(t, "alarm-share-out@example.test", password, "Alarm Outsider")
	setTimezoneDefault(t, ownerToken, "Europe/Berlin")
	setTimezoneDefault(t, readerToken, "America/New_York")

	_, calUUID := createCalendar(t, ownerToken, "Team", "#445566")
	collection := "/dav/" + ownerName + "/calendars/" + calendarPath(t, ownerToken, calUUID) + "/"
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(2 * time.Hour)
	day := now.AddDate(0, 0, 1)
	davPut(t, collection+"timed.ics", ownerEmail, ownerApp, vcalendar(vevent("alarm169-shared-timed",
		"DTSTART:"+icsStamp(start)+"\r\n", "DTEND:"+icsStamp(start.Add(time.Hour))+"\r\n", displayAlarm("TRIGGER:-PT15M"))))
	davPut(t, collection+"allday.ics", ownerEmail, ownerApp, vcalendar(vevent("alarm169-shared-allday",
		"DTSTART;VALUE=DATE:"+day.Format("20060102")+"\r\n", "DTEND;VALUE=DATE:"+day.AddDate(0, 0, 1).Format("20060102")+"\r\n",
		displayAlarm("TRIGGER:PT9H"))))

	share := func(email, permission string) string {
		var resp struct {
			ID string `json:"id"`
		}
		code := doJSONRaw(t, http.MethodPost, "/calendars/"+calUUID+"/shares", ownerToken,
			map[string]string{"user_identifier": email, "permission": permission}, &resp)
		require.Equal(t, http.StatusCreated, code)
		return resp.ID
	}
	readerShare := share(readerEmail, "read")
	share(rwEmail, "read-write")

	ownerBody := upcomingUntil(t, ownerToken)
	ownerTimed := alarmsOfUID(ownerBody, "alarm169-shared-timed")
	ownerAllDay := alarmsOfUID(ownerBody, "alarm169-shared-allday")
	require.Len(t, ownerTimed, 1)
	require.Len(t, ownerAllDay, 1)
	berlin, _ := time.LoadLocation("Europe/Berlin")
	assert.Equal(t, time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, berlin).UTC().Format(time.RFC3339), ownerAllDay[0].TriggerAt)

	for name, token := range map[string]string{"read": readerToken, "read-write": rwToken} {
		body := upcomingUntil(t, token)
		timed := alarmsOfUID(body, "alarm169-shared-timed")
		allDay := alarmsOfUID(body, "alarm169-shared-allday")
		require.Lenf(t, timed, 1, name)
		require.Lenf(t, allDay, 1, name)
		assert.Equal(t, ownerTimed[0].Key, timed[0].Key, name)
		assert.Equal(t, ownerTimed[0].TriggerAt, timed[0].TriggerAt, name)
		assert.Equal(t, ownerAllDay[0].Key, allDay[0].Key, "%s: the owner's zone, not the sharee's", name)
		assert.Equal(t, ownerAllDay[0].TriggerAt, allDay[0].TriggerAt, name)
		_, _, raw := getUpcoming(t, token, "")
		assertNoInternalIDs(t, "sharee upcoming "+name, raw)
	}

	outsider := upcomingUntil(t, outsiderToken)
	assert.Empty(t, alarmsOfUID(outsider, "alarm169-shared-timed"))
	assert.Empty(t, alarmsOfUID(outsider, "alarm169-shared-allday"))

	// Acknowledgements are per user (D18): the owner's does not hide the alarm
	// from a sharee and a sharee's does not hide it from the owner.
	require.Equal(t, http.StatusNoContent, acknowledge(t, ownerToken, ownerTimed[0].Key))
	assert.Empty(t, alarmsOfUID(upcomingUntil(t, ownerToken), "alarm169-shared-timed"), "the owner dismissed it")
	assert.Len(t, alarmsOfUID(upcomingUntil(t, readerToken), "alarm169-shared-timed"), 1, "the reader still has it")
	assert.Len(t, alarmsOfUID(upcomingUntil(t, rwToken), "alarm169-shared-timed"), 1, "the writer still has it")
	require.Equal(t, http.StatusNoContent, acknowledge(t, readerToken, ownerAllDay[0].Key))
	assert.Empty(t, alarmsOfUID(upcomingUntil(t, readerToken), "alarm169-shared-allday"))
	assert.Len(t, alarmsOfUID(upcomingUntil(t, ownerToken), "alarm169-shared-allday"), 1, "the owner is unaffected by the reader")
	assert.Len(t, alarmsOfUID(upcomingUntil(t, rwToken), "alarm169-shared-allday"), 1)

	status, raw := restCall(t, http.MethodDelete, "/calendars/"+calUUID+"/shares/"+readerShare, ownerToken, nil)
	require.Containsf(t, []int{http.StatusOK, http.StatusNoContent}, status, "%s", raw)
	assert.Empty(t, alarmsOfUID(upcomingUntil(t, readerToken), "alarm169-shared-timed"), "a revoked share stops the alarms")
	assert.NotEmpty(t, alarmsOfUID(upcomingUntil(t, rwToken), "alarm169-shared-timed"), "the other sharee keeps them")
}

func TestAlarmsSubscribedCalendarContributesNothing(t *testing.T) {
	password := "alarmSubscribed!123"
	ownerToken := registerAndLogin(t, "alarm-sub-owner@example.test", password, "Alarm Sub Owner")
	shareeEmail := "alarm-sub-sharee@example.test"
	shareeToken := registerAndLogin(t, shareeEmail, password, "Alarm Sub Sharee")

	start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	feed := newTestFeed(t)
	feed.set("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//CalCard Test//EN\r\n" +
		vevent("alarm169-feed@feed.test", "DTSTART:"+icsStamp(start)+"\r\n", "DTEND:"+icsStamp(start.Add(time.Hour))+"\r\n",
			"SUMMARY:Feed\r\n", displayAlarm("TRIGGER:-PT15M")) + "END:VCALENDAR\r\n")
	var created subscriptionPayload
	require.Equal(t, http.StatusCreated, doJSONRaw(t, http.MethodPost, "/calendar-subscriptions/", ownerToken,
		map[string]any{"url": feed.URL, "refresh_interval": "1h"}, &created))
	require.NotEmpty(t, eventSummaries(t, ownerToken, created.CalendarID), "the feed's event is mirrored")

	assert.Empty(t, alarmsOfUID(upcomingUntil(t, ownerToken), "alarm169-feed@feed.test"), "the owner")

	code := doJSONRaw(t, http.MethodPost, "/calendars/"+created.CalendarID+"/shares", ownerToken,
		map[string]string{"user_identifier": shareeEmail, "permission": "read"}, nil)
	if code == http.StatusCreated {
		require.NotEmpty(t, eventSummaries(t, shareeToken, created.CalendarID), "the sharee can read the mirror")
		assert.Empty(t, alarmsOfUID(upcomingUntil(t, shareeToken), "alarm169-feed@feed.test"), "the sharee")
	} else {
		t.Logf("a subscribed calendar cannot be shared (HTTP %d): only the owner's side is checked", code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/v1/alarms/acknowledge (#169 story 07)
// ---------------------------------------------------------------------------

// acknowledge posts the keys and returns the HTTP status.
func acknowledge(t *testing.T, token string, keys ...string) int {
	t.Helper()
	status, _ := restCall(t, http.MethodPost, "/alarms/acknowledge", token, map[string][]string{"keys": keys})
	return status
}

// loginAgain returns a second access token of the same user ("another device").
func loginAgain(t *testing.T, email, password string) string {
	t.Helper()
	var login struct {
		AccessToken string `json:"access_token"`
	}
	require.Equal(t, http.StatusOK, doJSON(t, http.MethodPost, "/auth/login", "", map[string]string{"email": email, "password": password}, &login))
	require.NotEmpty(t, login.AccessToken)
	return login.AccessToken
}

func userIDByEmail(t *testing.T, email string) uint {
	t.Helper()
	var id uint
	require.NoError(t, testDB.Raw("SELECT id FROM users WHERE email = ?", email).Scan(&id).Error)
	require.NotZero(t, id)
	return id
}

func ackRows(t *testing.T, userID uint) []calendar.AlarmAcknowledgement {
	t.Helper()
	var rows []calendar.AlarmAcknowledgement
	require.NoError(t, testDB.Where("user_id = ?", userID).Order("alarm_key").Find(&rows).Error)
	return rows
}

func hexKeyN(i int) string { return strings.Repeat("0", 24) + hex8(i) }

func hex8(i int) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 8)
	for p := 7; p >= 0; p-- {
		out[p] = digits[i&0xf]
		i >>= 4
	}
	return string(out)
}

func TestAlarmsAcknowledge(t *testing.T) {
	password := "alarmAck!123"
	email := "alarm-ack@example.test"
	token, username := registerAndLoginFull(t, email, password, "Alarm Ack")
	_, appPassword := createAppPassword(t, token, "alarm-ack")
	otherEmail := "alarm-ack-other@example.test"
	otherToken := registerAndLogin(t, otherEmail, password, "Alarm Ack Other")
	collection, _ := eventCollection(t, token, username)
	userID, otherID := userIDByEmail(t, email), userIDByEmail(t, otherEmail)
	now := time.Now().UTC().Truncate(time.Second)

	start := now.Add(2 * time.Hour)
	put := func(start time.Time) {
		davPut(t, collection+"ack.ics", email, appPassword, vcalendar(vevent("alarm169-ack",
			"DTSTART:"+icsStamp(start)+"\r\n", "DTEND:"+icsStamp(start.Add(time.Hour))+"\r\n", displayAlarm("TRIGGER:-PT15M"))))
	}
	put(start)
	first := alarmsOfUID(upcomingUntil(t, token), "alarm169-ack")
	require.Len(t, first, 1)
	key := first[0].Key

	t.Run("204, idempotent, one row with three columns in UTC", func(t *testing.T) {
		assert.Equal(t, http.StatusNoContent, acknowledge(t, token, key))
		assert.Equal(t, http.StatusNoContent, acknowledge(t, token, key, key), "a repeat and a duplicate are still 204")
		rows := ackRows(t, userID)
		require.Len(t, rows, 1)
		assert.Equal(t, key, rows[0].AlarmKey)
		assert.WithinDuration(t, time.Now(), rows[0].AcknowledgedAt, time.Minute)
		// (UTC storage is pinned by the repository test; PostgreSQL hands timestamptz back in the host's zone.)
		cols, err := testDB.Migrator().ColumnTypes(&calendar.AlarmAcknowledgement{})
		require.NoError(t, err)
		var names []string
		for _, c := range cols {
			names = append(names, c.Name())
		}
		assert.ElementsMatch(t, []string{"user_id", "alarm_key", "acknowledged_at"}, names)
	})

	t.Run("hidden after a reload and on another device", func(t *testing.T) {
		assert.Empty(t, alarmsOfUID(upcomingUntil(t, token), "alarm169-ack"))
		assert.Empty(t, alarmsOfUID(upcomingUntil(t, loginAgain(t, email, password)), "alarm169-ack"))
	})

	t.Run("another user's POST of the same key hides nothing from this user", func(t *testing.T) {
		start2 := now.Add(5 * time.Hour)
		davPut(t, collection+"ack2.ics", email, appPassword, vcalendar(vevent("alarm169-ack2",
			"DTSTART:"+icsStamp(start2)+"\r\n", "DTEND:"+icsStamp(start2.Add(time.Hour))+"\r\n", displayAlarm("TRIGGER:-PT15M"))))
		k2 := alarmsOfUID(upcomingUntil(t, token), "alarm169-ack2")
		require.Len(t, k2, 1)
		assert.Equal(t, http.StatusNoContent, acknowledge(t, otherToken, k2[0].Key), "a key of nobody's alarm is accepted")
		assert.Len(t, alarmsOfUID(upcomingUntil(t, token), "alarm169-ack2"), 1)
		// The same key for two users both persist.
		assert.Equal(t, http.StatusNoContent, acknowledge(t, token, k2[0].Key))
		assert.Len(t, ackRows(t, otherID), 1)
		assert.Len(t, ackRows(t, userID), 2)
		assert.Empty(t, alarmsOfUID(upcomingUntil(t, token), "alarm169-ack2"))
	})

	t.Run("a DAV move lists the alarm again under a new key, the old row stays", func(t *testing.T) {
		put(start.Add(30 * time.Minute))
		moved := alarmsOfUID(upcomingUntil(t, token), "alarm169-ack")
		require.Len(t, moved, 1)
		assert.NotEqual(t, key, moved[0].Key)
		assert.Equal(t, rfcStamp(start.Add(15*time.Minute)), moved[0].TriggerAt)
		var n int64
		require.NoError(t, testDB.Model(&calendar.AlarmAcknowledgement{}).Where("user_id = ? AND alarm_key = ?", userID, key).Count(&n).Error)
		assert.EqualValues(t, 1, n, "the old row ages out with the retention job")
	})

	t.Run("400 texts, the first bad index, nothing written", func(t *testing.T) {
		before := len(ackRows(t, userID))
		good := hexKeyN(1)
		many := make([]string, 501)
		for i := range many {
			many[i] = hexKeyN(i)
		}
		for name, tc := range map[string]struct {
			body any
			msg  string
		}{
			"invalid json":  {`{"keys":`, "Invalid request body"},
			"missing keys":  {map[string]any{}, "keys is required"},
			"empty keys":    {map[string][]string{"keys": {}}, "keys is required"},
			"501 keys":      {map[string][]string{"keys": many}, "Too many keys: at most 500 per request"},
			"uppercase":     {map[string][]string{"keys": {good, strings.ToUpper(hexKeyN(171))}}, "Invalid alarm key at index 1"},
			"31 characters": {map[string][]string{"keys": {good[1:]}}, "Invalid alarm key at index 0"},
			"43 characters": {map[string][]string{"keys": {good, good, good + "abcdefghijk"}}, "Invalid alarm key at index 2"},
			"non-hex":       {map[string][]string{"keys": {good, strings.Repeat("z", 32)}}, "Invalid alarm key at index 1"},
		} {
			status, raw := restCall(t, http.MethodPost, "/alarms/acknowledge", token, tc.body)
			assert.Equalf(t, http.StatusBadRequest, status, "%s: %s", name, raw)
			assert.Containsf(t, string(raw), tc.msg, name)
		}
		assert.Len(t, ackRows(t, userID), before, "a refused batch writes nothing")
	})

	t.Run("500 keys are accepted", func(t *testing.T) {
		before := len(ackRows(t, userID))
		keys := make([]string, 500)
		for i := range keys {
			keys[i] = hexKeyN(100000 + i)
		}
		assert.Equal(t, http.StatusNoContent, acknowledge(t, token, keys...))
		assert.Len(t, ackRows(t, userID), before+500)
	})

	t.Run("401 without a token", func(t *testing.T) {
		status, _ := restCall(t, http.MethodPost, "/alarms/acknowledge", "", map[string][]string{"keys": {key}})
		assert.Equal(t, http.StatusUnauthorized, status)
	})
}

func TestAlarmAcknowledgementsPurgedWithAccount(t *testing.T) {
	password := "alarmPurge!123"
	goneEmail, keptEmail := "alarm-purge-gone@example.test", "alarm-purge-kept@example.test"
	goneToken := registerAndLogin(t, goneEmail, password, "Alarm Purge Gone")
	keptToken := registerAndLogin(t, keptEmail, password, "Alarm Purge Kept")
	goneID, keptID := userIDByEmail(t, goneEmail), userIDByEmail(t, keptEmail)
	require.Equal(t, http.StatusNoContent, acknowledge(t, goneToken, hexKeyN(1), hexKeyN(2)))
	require.Equal(t, http.StatusNoContent, acknowledge(t, keptToken, hexKeyN(1)))
	require.Len(t, ackRows(t, goneID), 2)

	status, raw := restCall(t, http.MethodDelete, "/users/me", goneToken, map[string]string{"password": password, "confirmation": "DELETE"})
	require.Equalf(t, http.StatusNoContent, status, "%s", raw)

	assert.Empty(t, ackRows(t, goneID), "the deleted account's acknowledgements are gone")
	assert.Len(t, ackRows(t, keptID), 1, "another account's rows stay")
}
