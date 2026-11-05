//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reminderJSON struct {
	Index         int     `json:"index"`
	Action        string  `json:"action"`
	Related       *string `json:"related"`
	OffsetSeconds *int64  `json:"offset_seconds"`
	At            *string `json:"at"`
	Editable      bool    `json:"editable"`
}

type reminderEventJSON struct {
	ID        string         `json:"id"`
	UID       string         `json:"uid"`
	Reminders []reminderJSON `json:"reminders"`
}

func decodeReminderEvent(t *testing.T, raw []byte) reminderEventJSON {
	t.Helper()
	var e reminderEventJSON
	require.NoErrorf(t, json.Unmarshal(raw, &e), "decode: %s", raw)
	return e
}

// valarmBlocks returns every VALARM block of an unfolded iCalendar, in order.
func valarmBlocks(lines []string) [][]string {
	var out [][]string
	for i := 0; i < len(lines); i++ {
		if lines[i] != "BEGIN:VALARM" {
			continue
		}
		for j := i; j < len(lines); j++ {
			if lines[j] == "END:VALARM" {
				out = append(out, lines[i:j+1])
				i = j
				break
			}
		}
	}
	return out
}

func blockHas(b []string, line string) bool {
	for _, l := range b {
		if l == line {
			return true
		}
	}
	return false
}

// TestAlarmRemindersRoundTrip (#169 story 11, event part): reminders written by
// REST create and PATCH land as VALARMs in the DAV object, and an edit keeps
// every VALARM it does not own byte for byte.
func TestAlarmRemindersRoundTrip(t *testing.T) {
	email := "alarm-roundtrip@example.test"
	token, username := registerAndLoginFull(t, email, "alarmRound!123", "Alarm Roundtrip")
	_, appPass := createAppPassword(t, token, "alarm-roundtrip")
	collection, calUUID := eventCollection(t, token, username)
	eventsURL := "/calendars/" + calUUID + "/events"

	start := time.Now().UTC().Truncate(time.Second).Add(36 * time.Hour)
	at := start.Add(-time.Hour)
	atLocal := at.In(time.FixedZone("", 3600)).Format(time.RFC3339)

	// --- create ---------------------------------------------------------
	status, raw := restCall(t, http.MethodPost, eventsURL, token, map[string]any{
		"summary": "Roundtrip", "start": rfcStamp(start), "end": rfcStamp(start.Add(time.Hour)),
		"reminders": []map[string]any{{"offset_seconds": -900}, {"at": atLocal}},
	})
	require.Equalf(t, http.StatusCreated, status, "create: %s", raw)
	created := decodeReminderEvent(t, raw)
	require.Len(t, created.Reminders, 2)
	assert.Equal(t, int64(-900), *created.Reminders[0].OffsetSeconds)
	assert.True(t, created.Reminders[0].Editable)
	assert.Equal(t, rfcStamp(at), *created.Reminders[1].At)
	assert.True(t, created.Reminders[1].Editable)

	object := collection + created.ID + ".ics"
	body, _ := davGet(t, object, email, appPass)
	lines := icalLines(body)
	blocks := valarmBlocks(lines)
	require.Len(t, blocks, 2)
	for _, want := range []string{"ACTION:DISPLAY", "DESCRIPTION:Reminder", "TRIGGER:-PT15M"} {
		assert.Contains(t, blocks[0], want)
	}
	assert.Contains(t, blocks[1], "TRIGGER;VALUE=DATE-TIME:"+icsStamp(at))
	for _, b := range blocks {
		uids := 0
		for _, l := range b {
			if strings.HasPrefix(l, "UID:") {
				uids++
			}
		}
		assert.Equal(t, 1, uids, "each VALARM has a UID: %v", b)
	}

	keysOf := func() map[string]string { // trigger instant -> key
		out := map[string]string{}
		for _, a := range alarmsOfUID(upcomingUntil(t, token), created.UID) {
			out[a.TriggerAt] = a.Key
		}
		return out
	}
	keysBefore := keysOf()
	require.Len(t, keysBefore, 2)

	// --- a DAV client adds VALARMs of its own ------------------------------
	f1 := []string{"BEGIN:VALARM", "ACTION:AUDIO", "ACKNOWLEDGED:20301104T080100Z", "ATTACH:http://example.test/ding.wav",
		"TRIGGER:-PT20M", "UID:foreign-audio", "X-CLIENT;X-PARAM=kept:audio value", "END:VALARM"}
	f2 := []string{"BEGIN:VALARM", "ACTION:EMAIL", "DESCRIPTION:mail", "SUMMARY:mail", "TRIGGER:-PT30M", "END:VALARM"}
	f3 := []string{"BEGIN:VALARM", "ACTION:DISPLAY", "DURATION:PT1M", "REPEAT:2", "TRIGGER:-PT10M", "END:VALARM"}
	f4 := []string{"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER;RELATED=END:-PT5M", "X-END:yes", "END:VALARM"}
	foreign := append(append(append(append([]string{}, f1...), f2...), f3...), f4...)
	put := strings.Replace(body, "END:VEVENT", strings.Join(foreign, "\r\n")+"\r\nEND:VEVENT", 1)
	davPut(t, object, email, appPass, put)
	before, etagBefore := davGet(t, object, email, appPass)
	beforeLines := icalLines(before)
	require.Len(t, valarmBlocks(beforeLines), 6)

	patch := func(body any) (int, []byte) {
		return restCall(t, http.MethodPatch, eventsURL+"/"+created.ID, token, body)
	}

	// --- PATCH without reminders: every VALARM byte-identical --------------
	status, raw = patch(map[string]any{"summary": "Renamed"})
	require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
	after, _ := davGet(t, object, email, appPass)
	assert.Equal(t, valarmBlocks(beforeLines), valarmBlocks(icalLines(after)))
	var diffs []string
	afterLines := icalLines(after)
	require.Len(t, afterLines, len(beforeLines))
	for i := range beforeLines {
		if beforeLines[i] != afterLines[i] {
			diffs = append(diffs, propName(beforeLines[i]))
		}
	}
	assert.ElementsMatch(t, []string{"SUMMARY", "DTSTAMP"}, diffs, "only the edited property and DTSTAMP change")
	before, etagBefore = davGet(t, object, email, appPass)
	beforeLines = icalLines(before)

	// --- PATCH reminders: keep -PT15M and the AUDIO one (matched), drop the
	// absolute one, add -PT1H -----------------------------------------------
	status, raw = patch(map[string]any{"reminders": []map[string]any{
		{"offset_seconds": -900}, {"offset_seconds": -1200}, {"offset_seconds": -3600}}})
	require.Equalf(t, http.StatusOK, status, "patch reminders: %s", raw)
	resp := decodeReminderEvent(t, raw)
	require.Len(t, resp.Reminders, 6)
	after, _ = davGet(t, object, email, appPass)
	afterBlocks := valarmBlocks(icalLines(after))
	beforeBlocks := valarmBlocks(beforeLines)
	require.Len(t, afterBlocks, 6)
	assert.Equal(t, beforeBlocks[0], afterBlocks[0], "kept -PT15M untouched, same place")
	assert.Equal(t, beforeBlocks[2], afterBlocks[1], "matched AUDIO VALARM keeps ATTACH, X-, ACKNOWLEDGED, UID")
	assert.Equal(t, beforeBlocks[3], afterBlocks[2], "EMAIL untouched")
	assert.Equal(t, beforeBlocks[4], afterBlocks[3], "REPEAT untouched")
	assert.Equal(t, beforeBlocks[5], afterBlocks[4], "RELATED=END untouched")
	assert.True(t, blockHas(afterBlocks[5], "TRIGGER:-PT1H"), "new one appended last: %v", afterBlocks[5])
	assert.True(t, blockHas(afterBlocks[5], "DESCRIPTION:Reminder"))
	for _, b := range afterBlocks {
		assert.False(t, blockHas(b, "TRIGGER;VALUE=DATE-TIME:"+icsStamp(at)), "the absolute reminder is gone")
	}
	// Outside the VALARMs only DTSTAMP changed.
	strip := func(ls []string) []string {
		var out []string
		in := false
		for _, l := range ls {
			switch {
			case l == "BEGIN:VALARM":
				in = true
			case l == "END:VALARM":
				in = false
			case !in && propName(l) != "DTSTAMP":
				out = append(out, l)
			}
		}
		return out
	}
	assert.Equal(t, strip(beforeLines), strip(icalLines(after)))

	// The kept reminder's alarm key is unchanged; the dropped one has gone.
	keysAfter := keysOf()
	for instant, key := range keysBefore {
		if instant == rfcStamp(start.Add(-15*time.Minute)) {
			assert.Equal(t, key, keysAfter[instant], "the kept reminder keeps its key")
		}
	}

	// --- the same reminders again: nothing written, ETag unchanged ---------
	_, etagBefore = davGet(t, object, email, appPass)
	same := []map[string]any{{"offset_seconds": -900}, {"offset_seconds": -1200}, {"offset_seconds": -3600}}
	status, raw = patch(map[string]any{"reminders": same})
	require.Equalf(t, http.StatusOK, status, "patch same: %s", raw)
	_, etagAfter := davGet(t, object, email, appPass)
	assert.Equal(t, etagBefore, etagAfter, "a reminders list equal to the stored one writes nothing")

	// --- 400s write nothing -----------------------------------------------
	for body, msg := range map[string]string{
		`{"reminders":[{}]}`:                            "a reminder needs exactly one of offset_seconds and at",
		`{"reminders":[{"offset_seconds":99999999}]}`:   "reminder offset out of range",
		`{"reminders":[{"at":"tomorrow"}]}`:             "Invalid reminder time",
		`{"reminders":[{"at":"2099-01-01T00:00:00Z"}]}`: "reminder time out of range",
	} {
		var m any
		require.NoError(t, json.Unmarshal([]byte(body), &m))
		status, raw = patch(m)
		assertErrorBody(t, http.StatusBadRequest, msg, status, raw, body)
	}
	_, etagAfter = davGet(t, object, email, appPass)
	assert.Equal(t, etagBefore, etagAfter)

	// --- [] removes every editable VALARM, the others stay -----------------
	before, _ = davGet(t, object, email, appPass)
	status, raw = patch(map[string]any{"reminders": []any{}})
	require.Equalf(t, http.StatusOK, status, "patch []: %s", raw)
	resp = decodeReminderEvent(t, raw)
	require.Len(t, resp.Reminders, 3)
	for _, r := range resp.Reminders {
		assert.False(t, r.Editable)
	}
	after, _ = davGet(t, object, email, appPass)
	afterBlocks = valarmBlocks(icalLines(after))
	require.Len(t, afterBlocks, 3)
	bb := valarmBlocks(icalLines(before))
	assert.Equal(t, [][]string{bb[2], bb[3], bb[4]}, afterBlocks, "EMAIL, REPEAT and RELATED=END stay byte for byte")

	// --- null does the same ------------------------------------------------
	status, _ = patch(map[string]any{"reminders": []map[string]any{{"offset_seconds": -60}}})
	require.Equal(t, http.StatusOK, status)
	status, raw = restCall(t, http.MethodPatch, eventsURL+"/"+created.ID, token, json.RawMessage(`{"reminders":null}`))
	require.Equalf(t, http.StatusOK, status, "patch null: %s", raw)
	assert.Len(t, decodeReminderEvent(t, raw).Reminders, 3)
	after, _ = davGet(t, object, email, appPass)
	assert.Len(t, valarmBlocks(icalLines(after)), 3)
}

// TestAlarmRemindersSharing: a read-write sharee may send reminders, a
// read-only one gets the existing 403.
func TestAlarmRemindersSharing(t *testing.T) {
	password := "alarmRemShare!123"
	ownerToken, _ := registerAndLoginFull(t, "alarm-rem-owner@example.test", password, "Alarm Rem Owner")
	rwEmail, roEmail := "alarm-rem-rw@example.test", "alarm-rem-ro@example.test"
	rwToken := registerAndLogin(t, rwEmail, password, "Alarm Rem RW")
	roToken := registerAndLogin(t, roEmail, password, "Alarm Rem RO")
	_, calUUID := createCalendar(t, ownerToken, "Reminder Team", "#112233")
	shareCalendarWith(t, ownerToken, calUUID, rwEmail, "read-write")
	shareCalendarWith(t, ownerToken, calUUID, roEmail, "read")

	start := time.Now().UTC().Truncate(time.Second).Add(48 * time.Hour)
	eventsURL := "/calendars/" + calUUID + "/events"
	status, raw := restCall(t, http.MethodPost, eventsURL, rwToken, map[string]any{
		"summary": "Shared", "start": rfcStamp(start), "end": rfcStamp(start.Add(time.Hour)),
		"reminders": []map[string]any{{"offset_seconds": -600}},
	})
	require.Equalf(t, http.StatusCreated, status, "rw create: %s", raw)
	ev := decodeReminderEvent(t, raw)
	require.Len(t, ev.Reminders, 1)

	status, raw = restCall(t, http.MethodPatch, eventsURL+"/"+ev.ID, rwToken, map[string]any{"reminders": []any{}})
	require.Equalf(t, http.StatusOK, status, "rw patch: %s", raw)
	assert.Empty(t, decodeReminderEvent(t, raw).Reminders)

	status, _ = restCall(t, http.MethodPatch, eventsURL+"/"+ev.ID, roToken, map[string]any{"reminders": []map[string]any{{"offset_seconds": -60}}})
	assert.Equal(t, http.StatusForbidden, status)
	status, _ = restCall(t, http.MethodPost, eventsURL, roToken, map[string]any{
		"summary": "Nope", "start": rfcStamp(start), "end": rfcStamp(start.Add(time.Hour)),
		"reminders": []map[string]any{{"offset_seconds": -60}},
	})
	assert.Equal(t, http.StatusForbidden, status)
}
