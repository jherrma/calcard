//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
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
	// A fixed past DTSTAMP, so the PATCH below always changes it (DTSTAMP has
	// one-second resolution; a PATCH in the creation's second would leave it as is).
	put = regexp.MustCompile(`(?m)^DTSTAMP[^\r\n]*`).ReplaceAllString(put, "DTSTAMP:20200101T000000Z")
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

// TestAlarmRemindersRoundTripTasks (#169 story 12, task part): reminders
// written by REST create and PATCH land as VALARMs with RELATED=END in the DAV
// object, relative to DUE, and an edit keeps every VALARM it does not own byte
// for byte (also on a recurring task and across complete/uncomplete).
func TestAlarmRemindersRoundTripTasks(t *testing.T) {
	email := "alarm-roundtrip-tasks@example.test"
	token, username := registerAndLoginFull(t, email, "alarmRoundT!123", "Alarm Roundtrip Tasks")
	_, appPass := createAppPassword(t, token, "alarm-roundtrip-tasks")
	list := createTaskCalendar(t, token, "Chores", "tasks")
	collection := "/dav/" + username + "/calendars/" + list.Path + "/"

	due := time.Now().UTC().Truncate(time.Second).Add(36 * time.Hour)
	type remindersTask struct {
		ID        string         `json:"id"`
		UID       string         `json:"uid"`
		ETag      string         `json:"etag"`
		Reminders []reminderJSON `json:"reminders"`
	}
	decode := func(raw []byte) remindersTask {
		var r remindersTask
		require.NoErrorf(t, json.Unmarshal(raw, &r), "decode: %s", raw)
		return r
	}

	// --- create: relative to DUE, RELATED=END ----------------------------
	status, raw := restCall(t, http.MethodPost, tasksURL(list.UUID), token, map[string]any{
		"summary": "Pay rent", "due": rfcStamp(due), "reminders": []map[string]any{{"offset_seconds": -1800}},
	})
	require.Equalf(t, http.StatusCreated, status, "create: %s", raw)
	created := decode(raw)
	require.Len(t, created.Reminders, 1)
	require.NotNil(t, created.Reminders[0].Related)
	assert.Equal(t, "end", *created.Reminders[0].Related)
	assert.True(t, created.Reminders[0].Editable)

	object := collection + created.ID + ".ics"
	body, _ := davGet(t, object, email, appPass)
	blocks := valarmBlocks(icalLines(body))
	require.Len(t, blocks, 1)
	assert.Contains(t, blocks[0], "TRIGGER;RELATED=END:-PT30M")
	assert.Contains(t, blocks[0], "ACTION:DISPLAY")

	var triggerAt string
	for _, a := range alarmsOfUID(upcomingUntil(t, token), created.UID) {
		triggerAt = a.TriggerAt
	}
	assert.Equal(t, rfcStamp(due.Add(-30*time.Minute)), triggerAt, "fires at DUE - 30 min")

	// --- all-day task: PT9H -------------------------------------------------
	status, raw = restCall(t, http.MethodPost, tasksURL(list.UUID), token, map[string]any{
		"summary": "All day", "due": "2030-11-05", "due_all_day": true,
		"reminders": []map[string]any{{"offset_seconds": 32400}},
	})
	require.Equalf(t, http.StatusCreated, status, "create all-day: %s", raw)
	allDay := decode(raw)
	body, _ = davGet(t, collection+allDay.ID+".ics", email, appPass)
	assert.Contains(t, icalLines(body), "TRIGGER;RELATED=END:PT9H")

	// --- 400s on create ---------------------------------------------------
	status, raw = restCall(t, http.MethodPost, tasksURL(list.UUID), token, map[string]any{
		"summary": "No due", "reminders": []map[string]any{{"offset_seconds": -60}},
	})
	assertErrorBody(t, http.StatusBadRequest, "A reminder needs a due date", status, raw, "offset without due")

	// --- a DAV client adds foreign VALARMs ---------------------------------
	f1 := []string{"BEGIN:VALARM", "ACTION:EMAIL", "DESCRIPTION:mail", "SUMMARY:mail", "TRIGGER:-PT45M", "END:VALARM"}
	f2 := []string{"BEGIN:VALARM", "ACTION:DISPLAY", "DURATION:PT1M", "REPEAT:2", "TRIGGER;RELATED=END:-PT10M", "END:VALARM"}
	f3 := []string{"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-PT5M", "X-CLIENT;X-PARAM=kept:yes", "END:VALARM"}
	foreign := append(append(append([]string{}, f1...), f2...), f3...)
	put := strings.Replace(body0(t, object, email, appPass), "END:VTODO",
		"RELATED-TO;RELTYPE=PARENT:parent@phone\r\nX-APPLE-SORT-ORDER:731492317\r\n"+strings.Join(foreign, "\r\n")+"\r\nEND:VTODO", 1)
	davPut(t, object, email, appPass, put)
	before, _ := davGet(t, object, email, appPass)
	beforeLines := icalLines(before)
	require.Len(t, valarmBlocks(beforeLines), 4)

	patch := func(body any) (int, []byte) {
		return apiCall(t, http.MethodPatch, taskURL(list.UUID, created.ID), token, body, nil)
	}

	// PATCH without reminders and complete/uncomplete keep every VALARM.
	status, raw = patch(map[string]any{"summary": "Pay the rent"})
	require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
	for _, step := range []string{"/complete", "/uncomplete"} {
		status, raw = restCall(t, http.MethodPost, taskURL(list.UUID, created.ID)+step, token, nil)
		require.Equalf(t, http.StatusOK, status, "%s: %s", step, raw)
	}
	after, _ := davGet(t, object, email, appPass)
	assert.Equal(t, valarmBlocks(beforeLines), valarmBlocks(icalLines(after)))

	// The same list again: nothing written, ETag unchanged.
	_, etagBefore := davGet(t, object, email, appPass)
	status, raw = patch(map[string]any{"reminders": []map[string]any{{"offset_seconds": -1800}}})
	require.Equalf(t, http.StatusOK, status, "same list: %s", raw)
	_, etagSame := davGet(t, object, email, appPass)
	assert.Equal(t, etagBefore, etagSame, "unchanged list keeps the ETag")

	// A reminders-only change: bookkeeping bumps, foreign properties and VALARMs stay.
	before, _ = davGet(t, object, email, appPass)
	beforeLines = icalLines(before)
	status, raw = patch(map[string]any{"reminders": []map[string]any{{"offset_seconds": -1800}, {"offset_seconds": -7200}}})
	require.Equalf(t, http.StatusOK, status, "patch reminders: %s", raw)
	assert.Len(t, decode(raw).Reminders, 5)
	after, etagAfter := davGet(t, object, email, appPass)
	assert.NotEqual(t, etagBefore, etagAfter)
	beforeBlocks, afterBlocks := valarmBlocks(beforeLines), valarmBlocks(icalLines(after))
	require.Len(t, afterBlocks, 5)
	for i := range beforeBlocks {
		assert.Equal(t, beforeBlocks[i], afterBlocks[i], "VALARM %d untouched", i)
	}
	assert.True(t, blockHas(afterBlocks[4], "TRIGGER;RELATED=END:-PT2H"), "appended last: %v", afterBlocks[4])
	afterLines := icalLines(after)
	for _, l := range []string{"RELATED-TO;RELTYPE=PARENT:parent@phone", "X-APPLE-SORT-ORDER:731492317"} {
		assert.Contains(t, afterLines, l)
	}
	stripBookkeeping := func(ls []string) []string {
		var out []string
		in := false
		for _, l := range ls {
			switch {
			case l == "BEGIN:VALARM":
				in = true
			case l == "END:VALARM":
				in = false
			case !in && propName(l) != "DTSTAMP" && propName(l) != "LAST-MODIFIED" && propName(l) != "SEQUENCE":
				out = append(out, l)
			}
		}
		return out
	}
	assert.Equal(t, stripBookkeeping(beforeLines), stripBookkeeping(afterLines))
	assert.Contains(t, afterLines, "SEQUENCE:"+seqAfter(t, beforeLines))

	// An empty list removes the editable ones, never the foreign ones.
	status, raw = patch(map[string]any{"reminders": []any{}})
	require.Equalf(t, http.StatusOK, status, "patch []: %s", raw)
	after, _ = davGet(t, object, email, appPass)
	remaining := valarmBlocks(icalLines(after))
	require.Len(t, remaining, 3)
	assert.Equal(t, f1, remaining[0])
	assert.Equal(t, f2, remaining[1])
	// f3 is START-related: not editable on a task either, so it stays.
	assert.Equal(t, f3, remaining[2])

	// --- due handling ------------------------------------------------------
	status, raw = patch(map[string]any{"reminders": []map[string]any{{"offset_seconds": -600}}})
	require.Equalf(t, http.StatusOK, status, "re-add: %s", raw)
	status, raw = patch(map[string]any{"due": nil})
	require.Equalf(t, http.StatusOK, status, "remove due: %s", raw)
	body, _ = davGet(t, object, email, appPass)
	assert.Len(t, valarmBlocks(icalLines(body)), 4, "removing the due keeps the VALARMs")
	status, raw = patch(map[string]any{"reminders": []map[string]any{{"offset_seconds": -60}}})
	assertErrorBody(t, http.StatusBadRequest, "A reminder needs a due date", status, raw, "offset on a due-less task")
	status, raw = patch(map[string]any{"reminders": []map[string]any{{"at": rfcStamp(due)}}})
	assert.Equalf(t, http.StatusOK, status, "at without due: %s", raw)
}

func body0(t *testing.T, object, email, pass string) string {
	t.Helper()
	b, _ := davGet(t, object, email, pass)
	return b
}

// seqAfter is the SEQUENCE value one above the one in lines.
func seqAfter(t *testing.T, lines []string) string {
	t.Helper()
	for _, l := range lines {
		if v, ok := strings.CutPrefix(l, "SEQUENCE:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			require.NoError(t, err)
			return strconv.Itoa(n + 1)
		}
	}
	t.Fatal("no SEQUENCE")
	return ""
}

// ---------------------------------------------------------------------------
// #169 story 13: all-day reminders and the REST/MCP date shift (D23).
// ---------------------------------------------------------------------------

// mcpSession initialises an MCP session for a token minted from jwt and returns
// a function that calls one tool and decodes its JSON payload.
func mcpSession(t *testing.T, jwt string) func(tool string, args map[string]any) map[string]any {
	t.Helper()
	token := mcpToken(t, jwt, "alarm-shift")
	status, headers, _ := mcpCall(t, token, "",
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"integration","version":"1"}}}`)
	require.Equal(t, http.StatusOK, status)
	session := headers.Get("Mcp-Session-Id")
	require.NotEmpty(t, session)
	mcpCall(t, token, session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	id := 10
	return func(tool string, args map[string]any) map[string]any {
		t.Helper()
		id++
		raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call",
			"params": map[string]any{"name": tool, "arguments": args}})
		require.NoError(t, err)
		status, _, body := mcpCall(t, token, session, string(raw))
		require.Equal(t, http.StatusOK, status)
		payload, isErr := toolResultText(t, body)
		require.Falsef(t, isErr, "%s failed: %v", tool, payload)
		return payload
	}
}

// triggerLines are the TRIGGER lines of the unfolded iCalendar, in order.
func triggerLines(body string) []string {
	var out []string
	for _, l := range icalLines(body) {
		if strings.HasPrefix(l, "TRIGGER") {
			out = append(out, l)
		}
	}
	return out
}

// syncReport runs a sync-collection REPORT with the given token ("" = initial)
// and returns the body and the new token.
func syncReport(t *testing.T, collection, email, pass, token string) (string, string) {
	t.Helper()
	body := strings.Replace(syncCollectionBody, "<D:sync-token/>", "<D:sync-token>"+token+"</D:sync-token>", 1)
	if token == "" {
		body = syncCollectionBody
	}
	status, _, resp := davCall(t, "REPORT", collection, email, pass, body, depthHeader("1"))
	require.Equalf(t, http.StatusMultiStatus, status, "sync-collection: %s", string(resp))
	m := regexp.MustCompile(`<sync-token>([^<]*)</sync-token>`).FindStringSubmatch(string(resp))
	require.NotNilf(t, m, "no sync-token in %s", string(resp))
	return string(resp), m[1]
}

func eventIDByUID(t *testing.T, token, calUUID, uid string) string {
	t.Helper()
	status, raw := restCall(t, http.MethodGet, "/calendars/"+calUUID+"/events?start=2026-01-01T00:00:00Z&end=2028-01-01T00:00:00Z", token, nil)
	require.Equalf(t, http.StatusOK, status, "list events: %s", raw)
	var out struct {
		Events []struct {
			ID  string `json:"id"`
			UID string `json:"uid"`
		} `json:"events"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	for _, e := range out.Events {
		if e.UID == uid {
			return e.ID
		}
	}
	t.Fatalf("event %s not listed: %s", uid, raw)
	return ""
}

func taskIDByUID(t *testing.T, token, calUUID, uid string) string {
	t.Helper()
	status, raw := restCall(t, http.MethodGet, tasksURL(calUUID)+"?status=all", token, nil)
	require.Equalf(t, http.StatusOK, status, "list tasks: %s", raw)
	var out struct {
		Tasks []struct {
			ID  string `json:"id"`
			UID string `json:"uid"`
		} `json:"tasks"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	for _, tk := range out.Tasks {
		if tk.UID == uid {
			return tk.ID
		}
	}
	t.Fatalf("task %s not listed: %s", uid, raw)
	return ""
}

func upcomingWeek(t *testing.T, token string) upcomingBody {
	t.Helper()
	until := time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	status, body, raw := getUpcoming(t, token, "until="+until)
	require.Equalf(t, http.StatusOK, status, "upcoming: %s", raw)
	return body
}

func triggerTimes(body upcomingBody, uid string) []string {
	var out []string
	for _, a := range alarmsOfUID(body, uid) {
		out = append(out, a.TriggerAt)
	}
	sort.Strings(out)
	return out
}

func TestAllDayRemindersMoveWithDate(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	// localNine is 09:00 on the UTC date d (plus days) in Berlin, as the API prints it.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	day := func(n int) time.Time { return today.AddDate(0, 0, n) }
	localAt := func(d time.Time, hour int) string {
		return rfcStamp(time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, berlin))
	}
	dateOnly := func(d time.Time) string { return d.Format("2006-01-02") }
	sortedStrings := func(in ...string) []string { sort.Strings(in); return in }

	password := "allDayShift!123"
	email := "alarm-shift-owner@example.test"
	token, username := registerAndLoginFull(t, email, password, "Alarm All Day")
	_, appPass := createAppPassword(t, token, "alarm-allday")
	setTimezoneDefault(t, token, "Europe/Berlin")
	collection, calUUID := eventCollection(t, token, username)
	eventsURL := "/calendars/" + calUUID + "/events"
	mcp := mcpSession(t, token)

	t.Run("relative event reminders resolve in the owner's zone and move with the date", func(t *testing.T) {
		d := day(2)
		status, raw := restCall(t, http.MethodPost, eventsURL, token, map[string]any{
			"summary": "All day", "start": rfcStamp(d), "end": rfcStamp(d.AddDate(0, 0, 1)), "all_day": true, "timezone": "UTC",
			"reminders": []map[string]any{{"offset_seconds": 32400}, {"offset_seconds": -54000}},
		})
		require.Equalf(t, http.StatusCreated, status, "create: %s", raw)
		ev := decodeReminderEvent(t, raw)

		body, _ := davGet(t, collection+ev.ID+".ics", email, appPass)
		assert.Equal(t, []string{"TRIGGER:PT9H", "TRIGGER:-PT15H"}, triggerLines(body))
		assert.NotContains(t, body, "X-CALCARD", "no anchor property (D23)")
		assert.Equal(t, sortedStrings(localAt(d, 9), localAt(d.AddDate(0, 0, -1), 9)), triggerTimes(upcomingWeek(t, token), ev.UID))

		// REST move by +1 day: both alarms follow, still 09:00 local.
		d2 := day(3)
		status, raw = restCall(t, http.MethodPatch, eventsURL+"/"+ev.ID, token, map[string]any{
			"start": rfcStamp(d2), "end": rfcStamp(d2.AddDate(0, 0, 1)), "all_day": true, "timezone": "UTC"})
		require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
		body, _ = davGet(t, collection+ev.ID+".ics", email, appPass)
		assert.Equal(t, []string{"TRIGGER:PT9H", "TRIGGER:-PT15H"}, triggerLines(body), "relative triggers untouched")
		assert.Equal(t, sortedStrings(localAt(d2, 9), localAt(d2.AddDate(0, 0, -1), 9)), triggerTimes(upcomingWeek(t, token), ev.UID))

		// MCP move by one more day.
		d3 := day(4)
		mcp("update_event", map[string]any{"event_id": ev.ID, "start": rfcStamp(d3), "end": rfcStamp(d3.AddDate(0, 0, 1))})
		assert.Equal(t, sortedStrings(localAt(d3, 9), localAt(d3.AddDate(0, 0, -1), 9)), triggerTimes(upcomingWeek(t, token), ev.UID))
	})

	t.Run("absolute event triggers shift on REST and MCP moves, not on DAV moves", func(t *testing.T) {
		const uid = "alarm169-shift-event"
		object := collection + "shift-event.ics"
		put := func(date, endDate, trigger string) {
			davPut(t, object, email, appPass, vcalendar(vevent(uid,
				"DTSTART;VALUE=DATE:"+date+"\r\n", "DTEND;VALUE=DATE:"+endDate+"\r\n", "SUMMARY:Shift\r\n",
				displayAlarm("TRIGGER;VALUE=DATE-TIME:"+trigger))))
		}
		put("20261105", "20261106", "20261104T080000Z")
		id := eventIDByUID(t, token, calUUID, uid)
		move := func(via, date string) {
			start, err := time.Parse("2006-01-02", date)
			require.NoError(t, err)
			end := start.AddDate(0, 0, 1)
			if via == "rest" {
				status, raw := restCall(t, http.MethodPatch, eventsURL+"/"+id, token, map[string]any{
					"start": rfcStamp(start), "end": rfcStamp(end), "all_day": true, "timezone": "UTC"})
				require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
				return
			}
			mcp("update_event", map[string]any{"event_id": id, "start": rfcStamp(start), "end": rfcStamp(end)})
		}
		trigger := func() (string, string) {
			body, etag := davGet(t, object, email, appPass)
			require.Len(t, triggerLines(body), 1)
			return triggerLines(body)[0], etag
		}

		_, syncToken := syncReport(t, collection, email, appPass, "")
		_, etag0 := trigger()
		move("rest", "2026-11-12")
		got, etag1 := trigger()
		assert.Equal(t, "TRIGGER;VALUE=DATE-TIME:20261111T080000Z", got, "+7 days, CET")
		assert.NotEqual(t, etag0, etag1)
		report, _ := syncReport(t, collection, email, appPass, syncToken)
		assert.Contains(t, report, "shift-event.ics", "sync-collection reports the moved object")

		move("rest", "2027-04-02")
		got, _ = trigger()
		assert.Equal(t, "TRIGGER;VALUE=DATE-TIME:20270401T070000Z", got, "09:00 CEST after the DST change")

		move("mcp", "2027-04-05")
		got, etag2 := trigger()
		assert.Equal(t, "TRIGGER;VALUE=DATE-TIME:20270404T070000Z", got)
		move("mcp", "2026-11-05")
		got, etag3 := trigger()
		assert.Equal(t, "TRIGGER;VALUE=DATE-TIME:20261104T080000Z", got, "back across the DST change")
		assert.NotEqual(t, etag2, etag3)

		// A title-only PATCH shifts nothing.
		status, raw := restCall(t, http.MethodPatch, eventsURL+"/"+id, token, map[string]any{"summary": "Renamed"})
		require.Equalf(t, http.StatusOK, status, "rename: %s", raw)
		got, _ = trigger()
		assert.Equal(t, "TRIGGER;VALUE=DATE-TIME:20261104T080000Z", got)

		// A DAV move leaves the absolute trigger where the client put it.
		put("20270601", "20270602", "20261104T080000Z")
		got, _ = trigger()
		assert.Equal(t, "TRIGGER;VALUE=DATE-TIME:20261104T080000Z", got, "a DAV move does not shift")
	})

	t.Run("an at in the same request is matched and shifted, a new one is written as sent", func(t *testing.T) {
		const uid = "alarm169-shift-skip"
		object := collection + "shift-skip.ics"
		davPut(t, object, email, appPass, vcalendar(vevent(uid,
			"DTSTART;VALUE=DATE:20261105\r\n", "DTEND;VALUE=DATE:20261106\r\n", "SUMMARY:Skip\r\n",
			displayAlarm("TRIGGER;VALUE=DATE-TIME:20261104T080000Z"))))
		id := eventIDByUID(t, token, calUUID, uid)
		status, raw := restCall(t, http.MethodPatch, eventsURL+"/"+id, token, map[string]any{
			"start": "2026-11-12T00:00:00Z", "end": "2026-11-13T00:00:00Z", "all_day": true, "timezone": "UTC",
			"reminders": []map[string]any{{"at": "2026-11-04T08:00:00Z"}, {"at": "2026-11-20T08:00:00Z"}}})
		require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
		body, _ := davGet(t, object, email, appPass)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261111T080000Z", "TRIGGER;VALUE=DATE-TIME:20261120T080000Z"}, triggerLines(body))
	})

	t.Run("a timed event and a floating trigger never shift", func(t *testing.T) {
		const uid = "alarm169-shift-timed"
		object := collection + "shift-timed.ics"
		davPut(t, object, email, appPass, vcalendar(vevent(uid,
			"DTSTART:20261105T100000Z\r\n", "DTEND:20261105T110000Z\r\n", "SUMMARY:Timed\r\n",
			displayAlarm("TRIGGER;VALUE=DATE-TIME:20261104T080000Z"))))
		id := eventIDByUID(t, token, calUUID, uid)
		status, raw := restCall(t, http.MethodPatch, eventsURL+"/"+id, token, map[string]any{
			"start": "2026-11-12T10:00:00Z", "end": "2026-11-12T11:00:00Z"})
		require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
		body, _ := davGet(t, object, email, appPass)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggerLines(body))

		const uid2 = "alarm169-shift-floating"
		object2 := collection + "shift-floating.ics"
		davPut(t, object2, email, appPass, vcalendar(vevent(uid2,
			"DTSTART;VALUE=DATE:20261105\r\n", "DTEND;VALUE=DATE:20261106\r\n", "SUMMARY:Floating\r\n",
			displayAlarm("TRIGGER;VALUE=DATE-TIME:20261104T090000"))))
		id2 := eventIDByUID(t, token, calUUID, uid2)
		status, raw = restCall(t, http.MethodPatch, eventsURL+"/"+id2, token, map[string]any{
			"start": "2026-11-12T00:00:00Z", "end": "2026-11-13T00:00:00Z", "all_day": true, "timezone": "UTC"})
		require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
		body, _ = davGet(t, object2, email, appPass)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T090000"}, triggerLines(body))
	})

	t.Run("a read-write sharee's move shifts in the owner's zone", func(t *testing.T) {
		shareeEmail := "alarm-shift-sharee@example.test"
		shareeToken := registerAndLogin(t, shareeEmail, password, "Alarm Sharee")
		setTimezoneDefault(t, shareeToken, "America/New_York")
		shareCalendarWith(t, token, calUUID, shareeEmail, "read-write")
		const uid = "alarm169-shift-sharee"
		davPut(t, collection+"shift-sharee.ics", email, appPass, vcalendar(vevent(uid,
			"DTSTART;VALUE=DATE:20261105\r\n", "DTEND;VALUE=DATE:20261106\r\n", "SUMMARY:Shared\r\n",
			displayAlarm("TRIGGER;VALUE=DATE-TIME:20261104T080000Z"))))
		id := eventIDByUID(t, token, calUUID, uid)
		status, raw := restCall(t, http.MethodPatch, eventsURL+"/"+id, shareeToken, map[string]any{
			"start": "2027-04-02T00:00:00Z", "end": "2027-04-03T00:00:00Z", "all_day": true, "timezone": "UTC"})
		require.Equalf(t, http.StatusOK, status, "sharee patch: %s", raw)
		body, _ := davGet(t, collection+"shift-sharee.ics", email, appPass)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20270401T070000Z"}, triggerLines(body), "Berlin (owner), not New York")
	})

	t.Run("all-day task reminders resolve at 09:00 on the due date and follow it", func(t *testing.T) {
		list := createTaskCalendar(t, token, "Chores", "tasks")
		d := day(2)
		status, raw := restCall(t, http.MethodPost, tasksURL(list.UUID), token, map[string]any{
			"summary": "All day task", "due": dateOnly(d), "due_all_day": true,
			"reminders": []map[string]any{{"offset_seconds": 32400}}})
		require.Equalf(t, http.StatusCreated, status, "create: %s", raw)
		var created struct {
			ID  string `json:"id"`
			UID string `json:"uid"`
		}
		require.NoError(t, json.Unmarshal(raw, &created))
		object := "/dav/" + username + "/calendars/" + list.Path + "/" + created.ID + ".ics"
		body, _ := davGet(t, object, email, appPass)
		assert.Equal(t, []string{"TRIGGER;RELATED=END:PT9H"}, triggerLines(body))
		assert.Equal(t, []string{localAt(d, 9)}, triggerTimes(upcomingWeek(t, token), created.UID))

		d2 := day(3)
		status, raw = apiCall(t, http.MethodPatch, taskURL(list.UUID, created.ID), token, map[string]any{"due": dateOnly(d2)}, nil)
		require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
		assert.Equal(t, []string{localAt(d2, 9)}, triggerTimes(upcomingWeek(t, token), created.UID))

		d3 := day(4)
		mcp("update_task", map[string]any{"task_id": created.ID, "due": dateOnly(d3)})
		assert.Equal(t, []string{localAt(d3, 9)}, triggerTimes(upcomingWeek(t, token), created.UID))
	})

	t.Run("absolute task triggers shift on REST and MCP due moves", func(t *testing.T) {
		list := createTaskCalendar(t, token, "Shift tasks", "tasks")
		const uid = "alarm169-shift-task"
		object := "/dav/" + username + "/calendars/" + list.Path + "/shift-task.ics"
		put := func(due string) {
			davPut(t, object, email, appPass, vcalendar("BEGIN:VTODO\r\nUID:"+uid+"\r\nDTSTAMP:20260101T000000Z\r\nSUMMARY:Shift\r\n"+
				"DUE;VALUE=DATE:"+due+"\r\n"+displayAlarm("TRIGGER;VALUE=DATE-TIME:20261104T080000Z")+"END:VTODO\r\n"))
		}
		put("20261105")
		id := taskIDByUID(t, token, list.UUID, uid)
		trigger := func() (string, string) {
			body, etag := davGet(t, object, email, appPass)
			require.Len(t, triggerLines(body), 1)
			return triggerLines(body)[0], etag
		}
		_, etag0 := trigger()
		status, raw := apiCall(t, http.MethodPatch, taskURL(list.UUID, id), token, map[string]any{"due": "2026-11-12"}, nil)
		require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
		got, etag1 := trigger()
		assert.Equal(t, "TRIGGER;VALUE=DATE-TIME:20261111T080000Z", got)
		assert.NotEqual(t, etag0, etag1)

		status, raw = apiCall(t, http.MethodPatch, taskURL(list.UUID, id), token, map[string]any{"due": "2027-04-02"}, nil)
		require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
		got, _ = trigger()
		assert.Equal(t, "TRIGGER;VALUE=DATE-TIME:20270401T070000Z", got)

		mcp("update_task", map[string]any{"task_id": id, "due": "2026-11-05"})
		got, _ = trigger()
		assert.Equal(t, "TRIGGER;VALUE=DATE-TIME:20261104T080000Z", got, "MCP, back across the DST change")

		// Same-request `at` entries: matched then shifted, new ones as sent.
		status, raw = apiCall(t, http.MethodPatch, taskURL(list.UUID, id), token, map[string]any{
			"due": "2026-11-12", "reminders": []map[string]any{{"at": "2026-11-04T08:00:00Z"}, {"at": "2026-11-20T08:00:00Z"}}}, nil)
		require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
		body, _ := davGet(t, object, email, appPass)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261111T080000Z", "TRIGGER;VALUE=DATE-TIME:20261120T080000Z"}, triggerLines(body))

		// A DAV move leaves the trigger.
		put("20270601")
		body, _ = davGet(t, object, email, appPass)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20261104T080000Z"}, triggerLines(body))
	})

	t.Run("an owner without a preference shifts whole UTC days", func(t *testing.T) {
		email2 := "alarm-shift-nopref@example.test"
		token2, username2 := registerAndLoginFull(t, email2, password, "Alarm No Pref")
		_, appPass2 := createAppPassword(t, token2, "alarm-allday-nopref")
		collection2, calUUID2 := eventCollection(t, token2, username2)
		const uid = "alarm169-shift-nopref"
		davPut(t, collection2+"nopref.ics", email2, appPass2, vcalendar(vevent(uid,
			"DTSTART;VALUE=DATE:20261105\r\n", "DTEND;VALUE=DATE:20261106\r\n", "SUMMARY:UTC\r\n",
			displayAlarm("TRIGGER;VALUE=DATE-TIME:20261104T080000Z"))))
		id := eventIDByUID(t, token2, calUUID2, uid)
		status, raw := restCall(t, http.MethodPatch, "/calendars/"+calUUID2+"/events/"+id, token2, map[string]any{
			"start": "2027-04-02T00:00:00Z", "end": "2027-04-03T00:00:00Z", "all_day": true, "timezone": "UTC"})
		require.Equalf(t, http.StatusOK, status, "patch: %s", raw)
		body, _ := davGet(t, collection2+"nopref.ics", email2, appPass2)
		assert.Equal(t, []string{"TRIGGER;VALUE=DATE-TIME:20270401T080000Z"}, triggerLines(body), "24 h days in UTC")
	})
}
