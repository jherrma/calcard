//go:build integration

package integration_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #169 story 14: how CalCard behaves when alarms come from, or go to, a CalDAV
// client. Everything is asserted through DAV, REST and MCP.
//
// Design decisions pinned here (spec r6):
//   - D18: ACKNOWLEDGED inside a VALARM is never read, and acknowledging over
//     REST writes nothing into the iCalendar (bytes and ETag stay).
//   - D22: an alarm key is (object, recurrence id, trigger instant), never the
//     VALARM index, so reordering or removing VALARMs keeps the other keys and
//     their acknowledgements; VALARMs at one instant are one entry.
//   - D23: a DAV PUT never rewrites a client's data. A relative all-day
//     reminder moves with the date by construction; an absolute trigger stays
//     exactly where the client put it, also when the PUT moves the date.
//
// Known gap: an RDATE-only event (no RRULE) is expanded to its DTSTART
// occurrence only (domain/calendar/event.go, "Key exceptions" / expansion); the
// RDATE instants carry no alarm. The test documents it so a fix flips it.
//
// Real clients (Apple, DAVx5/Tasks.org, Thunderbird) decide what they PUT; this
// file pins CalCard's side only.

// davAlarmUser is a fresh user with an app password and its event collection.
type davAlarmUser struct {
	email, pass, token, username string
	collection, calUUID          string
}

func newDavAlarmUser(t *testing.T, label string) davAlarmUser {
	t.Helper()
	email := "alarm-dav-" + label + "@example.test"
	token, username := registerAndLoginFull(t, email, "alarmDav!123", "Alarm DAV "+label)
	_, app := createAppPassword(t, token, "alarm-dav-"+label)
	collection, calUUID := eventCollection(t, token, username)
	return davAlarmUser{email: email, pass: app, token: token, username: username, collection: collection, calUUID: calUUID}
}

func (u davAlarmUser) put(t *testing.T, name, ical string) {
	t.Helper()
	davPut(t, u.collection+name+".ics", u.email, u.pass, vcalendar(ical))
}

func (u davAlarmUser) get(t *testing.T, name string) (string, string) {
	t.Helper()
	return davGet(t, u.collection+name+".ics", u.email, u.pass)
}

func (u davAlarmUser) alarms(t *testing.T, uid string) []upcomingAlarm {
	t.Helper()
	return alarmsOfUID(upcomingWeek(t, u.token), uid)
}

func (u davAlarmUser) triggers(t *testing.T, uid string) []string {
	t.Helper()
	var out []string
	for _, a := range u.alarms(t, uid) {
		out = append(out, a.TriggerAt)
	}
	return out
}

func keysByTrigger(alarms []upcomingAlarm) map[string]string {
	out := map[string]string{}
	for _, a := range alarms {
		out[a.TriggerAt] = a.Key
	}
	return out
}

// valarmLines is the sorted multiset of every content line inside VALARM blocks.
func valarmLines(body string) []string {
	var out []string
	for _, b := range valarmBlocks(icalLines(body)) {
		out = append(out, b...)
	}
	sort.Strings(out)
	return out
}

func TestAlarmsDAVBehaviour(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	today := now.Truncate(24 * time.Hour)
	day := func(n int) time.Time { return today.AddDate(0, 0, n) }
	dateICS := func(d time.Time) string { return d.Format("20060102") }
	dtstart := func(d time.Time) string { return "DTSTART:" + icsStamp(d) + "\r\n" }
	dtend := func(d time.Time) string { return "DTEND:" + icsStamp(d) + "\r\n" }
	allDay := func(d time.Time) []string {
		return []string{"DTSTART;VALUE=DATE:" + dateICS(d) + "\r\n", "DTEND;VALUE=DATE:" + dateICS(d.AddDate(0, 0, 1)) + "\r\n"}
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)

	t.Run("1 a DAV-written alarm is listed at once and gone after DAV DELETE", func(t *testing.T) {
		u := newDavAlarmUser(t, "list")
		start := now.Add(2 * time.Hour)
		u.put(t, "a", vevent("dav14-list", dtstart(start), dtend(start.Add(time.Hour)), "SUMMARY:Listed\r\n", displayAlarm("TRIGGER:-PT15M")))
		got := u.alarms(t, "dav14-list")
		require.Len(t, got, 1)
		assert.Equal(t, rfcStamp(start.Add(-15*time.Minute)), got[0].TriggerAt)

		status, _, raw := davCall(t, "DELETE", u.collection+"a.ics", u.email, u.pass, "", nil)
		require.Containsf(t, []int{http.StatusNoContent, http.StatusOK}, status, "%s", raw)
		assert.Empty(t, u.alarms(t, "dav14-list"))
	})

	t.Run("2 a DAV time move gives a new key and the old acknowledgement does not hide it", func(t *testing.T) {
		u := newDavAlarmUser(t, "move")
		start := now.Add(2 * time.Hour)
		put := func(s time.Time) {
			u.put(t, "m", vevent("dav14-move", dtstart(s), dtend(s.Add(time.Hour)), displayAlarm("TRIGGER:-PT15M")))
		}
		put(start)
		old := u.alarms(t, "dav14-move")
		require.Len(t, old, 1)
		require.Equal(t, http.StatusNoContent, acknowledge(t, u.token, old[0].Key))
		assert.Empty(t, u.alarms(t, "dav14-move"), "acknowledged alarm is hidden")

		put(start.Add(30 * time.Minute))
		moved := u.alarms(t, "dav14-move")
		require.Len(t, moved, 1, "the moved alarm fires again")
		assert.Equal(t, rfcStamp(start.Add(15*time.Minute)), moved[0].TriggerAt)
		assert.NotEqual(t, old[0].Key, moved[0].Key)
	})

	t.Run("3 D23 a DAV date move of an all-day item keeps absolute triggers as sent", func(t *testing.T) {
		u := newDavAlarmUser(t, "allday")
		setTimezoneDefault(t, u.token, "Europe/Berlin")
		abs := day(1).Add(8 * time.Hour)
		absLine := "TRIGGER;VALUE=DATE-TIME:" + icsStamp(abs)
		alarms := displayAlarm("TRIGGER:PT9H") + displayAlarm(absLine)
		put := func(d time.Time) {
			u.put(t, "ad", vevent("dav14-allday", append(allDay(d), "SUMMARY:Day\r\n", alarms)...))
		}
		localNine := func(d time.Time) string {
			return rfcStamp(time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, berlin))
		}

		put(day(0))
		before := keysByTrigger(u.alarms(t, "dav14-allday"))
		require.Contains(t, before, localNine(day(0)))
		require.Contains(t, before, rfcStamp(abs))

		put(day(6))
		after := keysByTrigger(u.alarms(t, "dav14-allday"))
		require.Contains(t, after, localNine(day(6)), "the relative alarm moves, 09:00 in the owner's zone")
		assert.NotEqual(t, before[localNine(day(0))], after[localNine(day(6))], "new key")
		assert.Equal(t, before[rfcStamp(abs)], after[rfcStamp(abs)], "the absolute alarm is untouched, same key")
		assert.Len(t, after, 2)

		body, _ := u.get(t, "ad")
		assert.Equal(t, []string{"TRIGGER:PT9H", absLine}, triggerLines(body), "no shift, no added property")

		// The same for an all-day VTODO's DUE.
		list := createTaskCalendar(t, u.token, "Dav14 tasks", "tasks")
		object := "/dav/" + u.username + "/calendars/" + list.Path + "/t.ics"
		putTask := func(d time.Time) {
			davPut(t, object, u.email, u.pass, vcalendar("BEGIN:VTODO\r\nUID:dav14-task-allday\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:T\r\n"+
				"DUE;VALUE=DATE:"+dateICS(d)+"\r\n"+
				displayAlarm("TRIGGER;RELATED=END:PT9H")+displayAlarm(absLine)+"END:VTODO\r\n"))
		}
		putTask(day(0))
		tb := keysByTrigger(u.alarms(t, "dav14-task-allday"))
		putTask(day(6))
		ta := keysByTrigger(u.alarms(t, "dav14-task-allday"))
		require.Contains(t, ta, localNine(day(6)))
		assert.NotEqual(t, tb[localNine(day(0))], ta[localNine(day(6))])
		assert.Equal(t, tb[rfcStamp(abs)], ta[rfcStamp(abs)])
		tbody, _ := davGet(t, object, u.email, u.pass)
		assert.Equal(t, []string{"TRIGGER;RELATED=END:PT9H", absLine}, triggerLines(tbody))
	})

	t.Run("4 a DAV PUT never changes a VALARM", func(t *testing.T) {
		u := newDavAlarmUser(t, "norewrite")
		start := now.Add(5 * time.Hour)
		alarms := displayAlarm("TRIGGER:-PT15M") +
			"BEGIN:VALARM\r\nACTION:AUDIO\r\nTRIGGER;RELATED=END:-PT5M\r\nATTACH:http://example.test/ding.wav\r\nX-CLIENT;X-PARAM=kept:v\r\nEND:VALARM\r\n" +
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Rep\r\nTRIGGER:-PT1H\r\nDURATION:PT1M\r\nREPEAT:2\r\nACKNOWLEDGED:" + icsStamp(now) + "\r\nEND:VALARM\r\n" +
			"BEGIN:VALARM\r\nACTION:EMAIL\r\nSUMMARY:s\r\nDESCRIPTION:d\r\nTRIGGER;VALUE=DATE-TIME:" + icsStamp(start.Add(-2*time.Hour)) + "\r\nEND:VALARM\r\n" +
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT2H\r\nEND:VALARM\r\n"
		u.put(t, "e", vevent("dav14-norewrite", dtstart(start), dtend(start.Add(time.Hour)), alarms))
		sent := valarmLines(vcalendar(vevent("x", alarms)))
		got, _ := u.get(t, "e")
		assert.Equal(t, sent, valarmLines(got), "no property added or changed in any VALARM (event)")

		list := createTaskCalendar(t, u.token, "Dav14 norewrite", "tasks")
		object := "/dav/" + u.username + "/calendars/" + list.Path + "/t.ics"
		talarms := displayAlarm("TRIGGER;RELATED=END:-PT30M") + "BEGIN:VALARM\r\nACTION:AUDIO\r\nTRIGGER:-PT5M\r\nX-CLIENT:v\r\nEND:VALARM\r\n"
		davPut(t, object, u.email, u.pass, vcalendar("BEGIN:VTODO\r\nUID:dav14-norewrite-t\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:T\r\nDUE:"+
			icsStamp(start)+"\r\n"+talarms+"END:VTODO\r\n"))
		tgot, _ := davGet(t, object, u.email, u.pass)
		assert.Equal(t, valarmLines(vcalendar(vevent("x", talarms))), valarmLines(tgot), "no property added or changed in any VALARM (task)")
	})

	t.Run("5 D22 keys do not depend on the VALARM order or count", func(t *testing.T) {
		u := newDavAlarmUser(t, "keys")
		start := now.Add(5 * time.Hour)
		put := func(alarms ...string) {
			u.put(t, "k", vevent("dav14-keys", dtstart(start), dtend(start.Add(time.Hour)), strings.Join(alarms, "")))
		}
		a, b := displayAlarm("TRIGGER:-PT15M"), displayAlarm("TRIGGER:-PT30M")
		put(a, b)
		keys := keysByTrigger(u.alarms(t, "dav14-keys"))
		require.Len(t, keys, 2)

		put(b, a)
		assert.Equal(t, keys, keysByTrigger(u.alarms(t, "dav14-keys")), "reordering keeps both keys")

		kb := keys[rfcStamp(start.Add(-30*time.Minute))]
		require.Equal(t, http.StatusNoContent, acknowledge(t, u.token, kb))
		put(b)
		left := u.alarms(t, "dav14-keys")
		assert.Empty(t, left, "removing a VALARM keeps the other's key and its acknowledgement")
		put(a)
		left = u.alarms(t, "dav14-keys")
		require.Len(t, left, 1)
		assert.Equal(t, keys[rfcStamp(start.Add(-15*time.Minute))], left[0].Key)

		put(a, displayAlarm("TRIGGER;VALUE=DATE-TIME:"+icsStamp(start.Add(-15*time.Minute))), a)
		same := u.alarms(t, "dav14-keys")
		require.Len(t, same, 1, "three VALARMs at one instant are one entry")
		assert.Equal(t, left[0].Key, same[0].Key)
	})

	t.Run("6 D18 ACKNOWLEDGED is not read and acknowledging writes nothing into the iCalendar", func(t *testing.T) {
		u := newDavAlarmUser(t, "d18")
		sharee := "alarm-dav-d18-sharee@example.test"
		shareeToken := registerAndLogin(t, sharee, "alarmDav!123", "D18 Sharee")
		shareCalendarWith(t, u.token, u.calUUID, sharee, "read")
		start := now.Add(4 * time.Hour)
		trigger := start.Add(-15 * time.Minute)
		u.put(t, "ack", vevent("dav14-d18", dtstart(start), dtend(start.Add(time.Hour)),
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:x\r\nTRIGGER:-PT15M\r\nACKNOWLEDGED:"+icsStamp(trigger.Add(time.Minute))+"\r\nEND:VALARM\r\n"))
		own := u.alarms(t, "dav14-d18")
		require.Len(t, own, 1)
		assert.Len(t, alarmsOfUID(upcomingWeek(t, shareeToken), "dav14-d18"), 1, "the sharee sees it too")

		body, etag := u.get(t, "ack")
		require.Equal(t, http.StatusNoContent, acknowledge(t, u.token, own[0].Key))
		body2, etag2 := u.get(t, "ack")
		assert.Equal(t, body, body2, "DAV GET bytes unchanged")
		assert.Equal(t, etag, etag2, "ETag unchanged")
		assert.Empty(t, u.alarms(t, "dav14-d18"))
		assert.Len(t, alarmsOfUID(upcomingWeek(t, shareeToken), "dav14-d18"), 1, "the sharee acknowledges on their own")
	})

	t.Run("7 actions and trigger forms", func(t *testing.T) {
		u := newDavAlarmUser(t, "forms")
		start := now.Add(6 * time.Hour)
		put := func(uid string, alarms ...string) {
			u.put(t, uid, vevent(uid, dtstart(start), dtend(start.Add(time.Hour)), strings.Join(alarms, "")))
		}
		valarm := func(lines ...string) string {
			return "BEGIN:VALARM\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VALARM\r\n"
		}

		put("dav14-email", valarm("ACTION:EMAIL", "SUMMARY:s", "DESCRIPTION:d", "TRIGGER:-PT10M"))
		put("dav14-none", valarm("ACTION:NONE", "TRIGGER:-PT10M"))
		assert.Empty(t, u.alarms(t, "dav14-email"))
		assert.Empty(t, u.alarms(t, "dav14-none"))

		put("dav14-repeat", valarm("ACTION:DISPLAY", "DESCRIPTION:r", "TRIGGER:-PT10M", "DURATION:PT1M", "REPEAT:3"))
		assert.Equal(t, []string{rfcStamp(start.Add(-10 * time.Minute))}, u.triggers(t, "dav14-repeat"), "REPEAT is listed once")

		local := start.In(berlin).Add(-20 * time.Minute)
		put("dav14-tzid", valarm("ACTION:DISPLAY", "DESCRIPTION:z", "TRIGGER;VALUE=DATE-TIME;TZID=Europe/Berlin:"+local.Format("20060102T150405")))
		assert.Equal(t, []string{rfcStamp(start.Add(-20 * time.Minute))}, u.triggers(t, "dav14-tzid"), "TZID absolute trigger resolved")

		put("dav14-date", valarm("ACTION:DISPLAY", "DESCRIPTION:d", "TRIGGER;VALUE=DATE:"+dateICS(start)))
		put("dav14-notrigger", valarm("ACTION:DISPLAY", "DESCRIPTION:d"))
		put("dav14-apple", valarm("ACTION:NONE", "TRIGGER;VALUE=DATE-TIME:19760401T005545Z", "X-WR-ALARMUID:apple"),
			valarm("ACTION:DISPLAY", "DESCRIPTION:real", "TRIGGER:-PT5M"))
		assert.Empty(t, u.alarms(t, "dav14-date"))
		assert.Empty(t, u.alarms(t, "dav14-notrigger"))
		assert.Equal(t, []string{rfcStamp(start.Add(-5 * time.Minute))}, u.triggers(t, "dav14-apple"), "Apple's 1976 placeholder is never listed")
	})

	t.Run("8 recurrence written by a DAV client", func(t *testing.T) {
		u := newDavAlarmUser(t, "series")
		d0 := now.Add(-12 * time.Hour)
		d := func(n int) time.Time { return d0.Add(time.Duration(n) * 24 * time.Hour) }
		master := vevent("dav14-series",
			dtstart(d(0)), dtend(d(0).Add(time.Hour)), "RRULE:FREQ=DAILY;COUNT=6\r\n", "EXDATE:"+icsStamp(d(3))+"\r\n",
			displayAlarm("TRIGGER:-PT10M"))
		override := func(rid, start time.Time, alarm string) string {
			return vevent("dav14-series", "RECURRENCE-ID:"+icsStamp(rid)+"\r\n", dtstart(start), dtend(start.Add(time.Hour)), "SUMMARY:o\r\n", alarm)
		}
		u.put(t, "s", master+
			override(d(1), d(1), "")+ // override without a VALARM: no alarm for that occurrence
			override(d(2), d(2).Add(time.Hour), displayAlarm("TRIGGER:-PT5M"))+ // override with its own VALARM
			override(d(4).Add(30*time.Minute), d(4).Add(30*time.Minute), displayAlarm("TRIGGER:-PT7M"))) // stray override
		got := u.triggers(t, "dav14-series")
		assert.Equal(t, []string{
			rfcStamp(d(0).Add(-10 * time.Minute)),
			rfcStamp(d(2).Add(time.Hour - 5*time.Minute)),
			rfcStamp(d(4).Add(-10 * time.Minute)),
			rfcStamp(d(4).Add(30*time.Minute - 7*time.Minute)),
			rfcStamp(d(5).Add(-10 * time.Minute)),
		}, got, "no alarm for the bare override and the EXDATE; the override's own; the stray's own")

		// Documented gap: only the DTSTART occurrence of an RDATE-only event.
		start := now.Add(3 * time.Hour)
		u.put(t, "r", vevent("dav14-rdate", dtstart(start), dtend(start.Add(time.Hour)),
			"RDATE:"+icsStamp(start.Add(24*time.Hour))+"\r\n", displayAlarm("TRIGGER:-PT10M")))
		assert.Equal(t, []string{rfcStamp(start.Add(-10 * time.Minute))}, u.triggers(t, "dav14-rdate"), "RDATE instants carry no alarm (known gap)")
	})

	t.Run("9 tasks written by a DAV client", func(t *testing.T) {
		u := newDavAlarmUser(t, "tasks")
		list := createTaskCalendar(t, u.token, "Dav14 task alarms", "tasks")
		object := func(n string) string { return "/dav/" + u.username + "/calendars/" + list.Path + "/" + n + ".ics" }
		due := now.Add(3 * time.Hour)
		put := func(n, uid, extra, alarm string) {
			davPut(t, object(n), u.email, u.pass, vcalendar("BEGIN:VTODO\r\nUID:"+uid+"\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:T\r\nDUE:"+
				icsStamp(due)+"\r\n"+extra+alarm+"END:VTODO\r\n"))
		}
		put("a", "dav14-todo", "", displayAlarm("TRIGGER;RELATED=END:-PT30M"))
		assert.Equal(t, []string{rfcStamp(due.Add(-30 * time.Minute))}, u.triggers(t, "dav14-todo"))
		put("a", "dav14-todo", "STATUS:COMPLETED\r\n", displayAlarm("TRIGGER;RELATED=END:-PT30M"))
		assert.Empty(t, u.alarms(t, "dav14-todo"), "a DAV completion removes the alarm")

		put("b", "dav14-todo-start", "", displayAlarm("TRIGGER:-PT20M"))
		assert.Equal(t, []string{rfcStamp(due.Add(-20 * time.Minute))}, u.triggers(t, "dav14-todo-start"), "a DUE-only task falls back to DUE")
	})

	t.Run("10 web-written VALARMs read back over DAV and survive a client PUT", func(t *testing.T) {
		u := newDavAlarmUser(t, "web")
		eventsURL := "/calendars/" + u.calUUID + "/events"
		d := day(2)
		status, raw := restCall(t, http.MethodPost, eventsURL, u.token, map[string]any{
			"summary": "Web all day", "start": rfcStamp(d), "end": rfcStamp(d.AddDate(0, 0, 1)), "all_day": true, "timezone": "UTC",
			"reminders": []map[string]any{{"offset_seconds": 32400}, {"offset_seconds": -54000}, {"offset_seconds": -140400}}})
		require.Equalf(t, http.StatusCreated, status, "%s", raw)
		ev := decodeReminderEvent(t, raw)
		timed := now.Add(48 * time.Hour)
		status, raw = restCall(t, http.MethodPost, eventsURL, u.token, map[string]any{
			"summary": "Web timed", "start": rfcStamp(timed), "end": rfcStamp(timed.Add(time.Hour)),
			"reminders": []map[string]any{{"offset_seconds": -900}}})
		require.Equalf(t, http.StatusCreated, status, "%s", raw)
		tev := decodeReminderEvent(t, raw)

		check := func(id string, triggers ...string) string {
			body, _ := davGet(t, u.collection+id+".ics", u.email, u.pass)
			assert.Equal(t, triggers, triggerLines(body))
			blocks := valarmBlocks(icalLines(body))
			require.Len(t, blocks, len(triggers))
			for _, b := range blocks {
				assert.Contains(t, b, "ACTION:DISPLAY")
				assert.Contains(t, b, "DESCRIPTION:Reminder")
				n := 0
				for _, l := range b {
					if strings.HasPrefix(l, "UID:") {
						n++
					}
				}
				assert.Equal(t, 1, n, "a UID per VALARM")
			}
			return body
		}
		allBody := check(ev.ID, "TRIGGER:PT9H", "TRIGGER:-PT15H", "TRIGGER:-P1DT15H")
		timedBody := check(tev.ID, "TRIGGER:-PT15M")

		list := createTaskCalendar(t, u.token, "Dav14 web tasks", "tasks")
		status, raw = restCall(t, http.MethodPost, tasksURL(list.UUID), u.token, map[string]any{
			"summary": "Web task", "due": rfcStamp(timed), "reminders": []map[string]any{{"offset_seconds": -1800}}})
		require.Equalf(t, http.StatusCreated, status, "%s", raw)
		task := decodeReminderEvent(t, raw)
		taskObject := "/dav/" + u.username + "/calendars/" + list.Path + "/" + task.ID + ".ics"
		taskBody, _ := davGet(t, taskObject, u.email, u.pass)
		assert.Equal(t, []string{"TRIGGER;RELATED=END:-PT30M"}, triggerLines(taskBody))

		// A DAV client PUTting the bodies back unchanged keeps every key.
		for _, c := range []struct{ uid, object, body string }{
			{ev.UID, u.collection + ev.ID + ".ics", allBody},
			{tev.UID, u.collection + tev.ID + ".ics", timedBody},
			{task.UID, taskObject, taskBody},
		} {
			before := keysByTrigger(alarmsOfUID(upcomingWeek(t, u.token), c.uid))
			require.NotEmpty(t, before, c.uid)
			davPut(t, c.object, u.email, u.pass, c.body)
			assert.Equal(t, before, keysByTrigger(alarmsOfUID(upcomingWeek(t, u.token), c.uid)), c.uid)
		}
	})

	t.Run("11 REST edits keep a foreign VALARM byte for byte", func(t *testing.T) {
		u := newDavAlarmUser(t, "foreign")
		foreign := "BEGIN:VALARM\r\nACKNOWLEDGED:20301104T080100Z\r\nACTION:AUDIO\r\nATTACH:http://example.test/ding.wav\r\nTRIGGER:-PT20M\r\nUID:foreign-audio\r\nX-CLIENT;X-PARAM=kept:audio value\r\nEND:VALARM\r\n"
		start := now.Add(30 * time.Hour)
		u.put(t, "f", vevent("dav14-foreign", dtstart(start), dtend(start.Add(time.Hour)), "SUMMARY:Foreign\r\n", foreign))
		id := eventIDByUID(t, u.token, u.calUUID, "dav14-foreign")
		want := strings.Split(strings.TrimSuffix(strings.ReplaceAll(foreign, "\r\n", "\n"), "\n"), "\n")
		hasForeign := func(body string) {
			t.Helper()
			var found bool
			for _, b := range valarmBlocks(icalLines(body)) {
				if blockHas(b, "UID:foreign-audio") {
					found = true
					assert.Equal(t, want, b)
				}
			}
			assert.True(t, found, "foreign VALARM present")
		}
		patch := func(body map[string]any) {
			st, raw := restCall(t, http.MethodPatch, "/calendars/"+u.calUUID+"/events/"+id, u.token, body)
			require.Equalf(t, http.StatusOK, st, "%s", raw)
		}
		patch(map[string]any{"summary": "Renamed"})
		b, _ := u.get(t, "f")
		hasForeign(b)
		patch(map[string]any{"reminders": []map[string]any{{"offset_seconds": -1200}, {"offset_seconds": -300}}})
		b, _ = u.get(t, "f")
		hasForeign(b)
		assert.Len(t, valarmBlocks(icalLines(b)), 2)

		list := createTaskCalendar(t, u.token, "Dav14 foreign tasks", "tasks")
		object := "/dav/" + u.username + "/calendars/" + list.Path + "/ft.ics"
		davPut(t, object, u.email, u.pass, vcalendar("BEGIN:VTODO\r\nUID:dav14-foreign-task\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:T\r\nDUE:"+
			icsStamp(start)+"\r\n"+foreign+"END:VTODO\r\n"))
		tid := taskIDByUID(t, u.token, list.UUID, "dav14-foreign-task")
		for _, body := range []map[string]any{{"summary": "Renamed task"}, {"reminders": []map[string]any{{"offset_seconds": -300}}}} {
			st, raw := apiCall(t, http.MethodPatch, taskURL(list.UUID, tid), u.token, body, nil)
			require.Equalf(t, http.StatusOK, st, "%s", raw)
			tb, _ := davGet(t, object, u.email, u.pass)
			hasForeign(tb)
		}
		st, raw := restCall(t, http.MethodPost, taskURL(list.UUID, tid)+"/complete", u.token, nil)
		require.Equalf(t, http.StatusOK, st, "%s", raw)
		tb, _ := davGet(t, object, u.email, u.pass)
		hasForeign(tb)
	})

	t.Run("12 sync reports reminder edits and date shifts, not acknowledgements", func(t *testing.T) {
		u := newDavAlarmUser(t, "sync")
		start := now.Add(30 * time.Hour)
		u.put(t, "sy", vevent("dav14-sync", dtstart(start), dtend(start.Add(time.Hour)), displayAlarm("TRIGGER:-PT15M")))
		id := eventIDByUID(t, u.token, u.calUUID, "dav14-sync")
		eventURL := "/calendars/" + u.calUUID + "/events/" + id

		_, tok := syncReport(t, u.collection, u.email, u.pass, "")
		_, etag0 := u.get(t, "sy")

		st, raw := restCall(t, http.MethodPatch, eventURL, u.token, map[string]any{"reminders": []map[string]any{{"offset_seconds": -900}, {"offset_seconds": -600}}})
		require.Equalf(t, http.StatusOK, st, "%s", raw)
		_, etag1 := u.get(t, "sy")
		assert.NotEqual(t, etag0, etag1)
		report, tok := syncReport(t, u.collection, u.email, u.pass, tok)
		assert.Contains(t, report, "sy.ics", "a reminders edit is synced")

		got := u.alarms(t, "dav14-sync")
		require.Len(t, got, 2)
		require.Equal(t, http.StatusNoContent, acknowledge(t, u.token, got[0].Key))
		_, etag2 := u.get(t, "sy")
		assert.Equal(t, etag1, etag2, "an acknowledgement does not change the ETag")
		report, tok = syncReport(t, u.collection, u.email, u.pass, tok)
		assert.NotContains(t, report, "sy.ics", "an acknowledgement is not synced")

		// A REST date shift of an all-day event with an absolute trigger.
		u.put(t, "sd", vevent("dav14-sync-shift", append(allDay(day(2)), displayAlarm("TRIGGER;VALUE=DATE-TIME:"+icsStamp(day(1).Add(8*time.Hour))))...))
		_, tok = syncReport(t, u.collection, u.email, u.pass, tok)
		_, e0 := u.get(t, "sd")
		sid := eventIDByUID(t, u.token, u.calUUID, "dav14-sync-shift")
		st, raw = restCall(t, http.MethodPatch, "/calendars/"+u.calUUID+"/events/"+sid, u.token, map[string]any{
			"start": rfcStamp(day(3)), "end": rfcStamp(day(4)), "all_day": true, "timezone": "UTC"})
		require.Equalf(t, http.StatusOK, st, "%s", raw)
		_, e1 := u.get(t, "sd")
		assert.NotEqual(t, e0, e1)
		report, _ = syncReport(t, u.collection, u.email, u.pass, tok)
		assert.Contains(t, report, "sd.ics", "a date shift is synced")
	})
}
