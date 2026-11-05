package event

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	alarmM15   = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Soon\r\nTRIGGER;RELATED=START:-PT15M\r\nX-CUSTOM;FOO=bar:keep\r\nEND:VALARM\r\n"
	alarmP1H   = "BEGIN:VALARM\r\nACTION:AUDIO\r\nTRIGGER:PT1H\r\nEND:VALARM\r\n"
	alarmEmail = "BEGIN:VALARM\r\nACTION:EMAIL\r\nSUMMARY:s\r\nDESCRIPTION:d\r\nTRIGGER:-PT30M\r\nEND:VALARM\r\n"
	alarmAbs   = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Once\r\nTRIGGER;VALUE=DATE-TIME:20240102T080000Z\r\nEND:VALARM\r\n"
)

func alarmSeries(alarms string, extra string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:alarm-series\r\nDTSTAMP:20240101T000000Z\r\nDTSTART:20240101T100000Z\r\n" +
		"DTEND:20240101T110000Z\r\nRRULE:FREQ=DAILY;COUNT=10\r\nSUMMARY:Daily\r\n" + alarms + "END:VEVENT\r\n" +
		extra + "END:VCALENDAR\r\n"
}

func runUpdate(t *testing.T, ics string, in UpdateEventInput) (*calendar.CalendarObject, *ical.Calendar) {
	t.Helper()
	repo := &stubCalendarRepo{obj: &calendar.CalendarObject{
		UUID: "obj-uuid", UID: "alarm-series", ComponentType: "VEVENT", ICalData: ics,
	}}
	in.UUID = "obj-uuid"
	got, err := NewUpdateEventUseCase(repo).Execute(context.Background(), in)
	require.NoError(t, err)
	cal, err := ical.NewDecoder(strings.NewReader(got.ICalData)).Decode()
	require.NoError(t, err)
	return got, cal
}

func alarmsOf(c *ical.Component) []*ical.Component {
	var out []*ical.Component
	for _, ch := range c.Children {
		if ch.Name == ical.CompAlarm {
			out = append(out, ch)
		}
	}
	return out
}

func encodeComp(t *testing.T, c *ical.Component) string {
	t.Helper()
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "x")
	ev := ical.NewEvent()
	ev.Children = append(ev.Children, c)
	ev.Props.SetText(ical.PropUID, "u")
	ev.Props.SetDateTime(ical.PropDateTimeStamp, time.Unix(0, 0).UTC())
	cal.Children = append(cal.Children, ev.Component)
	var sb strings.Builder
	require.NoError(t, ical.NewEncoder(&sb).Encode(cal))
	return sb.String()
}

func override(cal *ical.Calendar) *ical.Component {
	for _, ev := range cal.Events() {
		if ev.Props.Get(ical.PropRecurrenceID) != nil {
			return ev.Component
		}
	}
	return nil
}

func master(cal *ical.Calendar) *ical.Component {
	for _, ev := range cal.Events() {
		if ev.Props.Get(ical.PropRecurrenceID) == nil {
			return ev.Component
		}
	}
	return nil
}

func TestUpdateEvent_ScopeThisCopiesRelativeAlarms(t *testing.T) {
	ics := alarmSeries(alarmM15+alarmAbs+alarmP1H+alarmEmail, "")
	sum := "Edited"
	got, cal := runUpdate(t, ics, UpdateEventInput{Summary: &sum, Scope: "this", RecurrenceID: "20240103T100000Z"})

	ov := override(cal)
	require.NotNil(t, ov)
	al := alarmsOf(ov)
	require.Len(t, al, 3, got.ICalData)
	// Document order, absolute one left on the master.
	assert.Equal(t, "-PT15M", al[0].Props.Get(ical.PropTrigger).Value)
	assert.Equal(t, "bar", al[0].Props.Get("X-CUSTOM").Params.Get("FOO"))
	assert.Equal(t, "PT1H", al[1].Props.Get(ical.PropTrigger).Value)
	assert.Equal(t, "EMAIL", al[2].Props.Get(ical.PropAction).Value, "classification is by trigger, not action")

	m := master(cal)
	assert.Len(t, alarmsOf(m), 4, "master keeps all its VALARMs")

	// Byte for byte against the source alarm.
	srcCal, err := ical.NewDecoder(strings.NewReader(ics)).Decode()
	require.NoError(t, err)
	assert.Equal(t, encodeComp(t, alarmsOf(master(srcCal))[0]), encodeComp(t, al[0]))
}

func TestUpdateEvent_ScopeThisAlarmKeepsKey(t *testing.T) {
	ics := alarmSeries(alarmM15, "")
	from, until := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)
	before, _, err := calendar.ExpandAlarms(&calendar.CalendarObject{UID: "alarm-series", ComponentType: "VEVENT", ICalData: ics}, from, until, time.UTC)
	require.NoError(t, err)

	sum := "Edited"
	got, _ := runUpdate(t, ics, UpdateEventInput{Summary: &sum, Scope: "this", RecurrenceID: "20240103T100000Z"})
	after, _, err := calendar.ExpandAlarms(&calendar.CalendarObject{UID: "alarm-series", ComponentType: "VEVENT", ICalData: got.ICalData}, from, until, time.UTC)
	require.NoError(t, err)

	keys := func(in []calendar.AlarmInstance) map[string]bool {
		m := map[string]bool{}
		for _, i := range in {
			m[i.Key] = true
		}
		return m
	}
	require.Len(t, after, len(before))
	assert.Equal(t, keys(before), keys(after), "every occurrence, incl. the edited one, keeps its key")
}

func TestUpdateEvent_ScopeThisNewStartMovesAlarm(t *testing.T) {
	ics := alarmSeries(alarmM15, "")
	start := "2024-01-03T12:00:00Z"
	end := "2024-01-03T13:00:00Z"
	got, _ := runUpdate(t, ics, UpdateEventInput{Start: &start, End: &end, Scope: "this", RecurrenceID: "20240103T100000Z"})
	from, until := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 3, 23, 0, 0, 0, time.UTC)
	inst, _, err := calendar.ExpandAlarms(&calendar.CalendarObject{UID: "alarm-series", ComponentType: "VEVENT", ICalData: got.ICalData}, from, until, time.UTC)
	require.NoError(t, err)
	var triggers []time.Time
	for _, i := range inst {
		triggers = append(triggers, i.TriggerAt)
	}
	assert.Contains(t, triggers, time.Date(2024, 1, 3, 11, 45, 0, 0, time.UTC))
	assert.NotContains(t, triggers, time.Date(2024, 1, 3, 9, 45, 0, 0, time.UTC))
}

func TestUpdateEvent_SplitCopiesRelativeAlarms(t *testing.T) {
	ics := alarmSeries(alarmP1H+alarmAbs+alarmM15, "")
	sum := "Future"
	got, cal := runUpdate(t, ics, UpdateEventInput{Summary: &sum, Scope: "this_and_future", RecurrenceID: "20240105T100000Z"})

	var masters []*ical.Component
	for _, ev := range cal.Events() {
		if ev.Props.Get(ical.PropRecurrenceID) == nil {
			masters = append(masters, ev.Component)
		}
	}
	require.Len(t, masters, 2, got.ICalData)
	assert.Len(t, alarmsOf(masters[0]), 3, "old master keeps all VALARMs")
	nm := alarmsOf(masters[1])
	require.Len(t, nm, 2)
	assert.Equal(t, "PT1H", nm[0].Props.Get(ical.PropTrigger).Value)
	assert.Equal(t, "-PT15M", nm[1].Props.Get(ical.PropTrigger).Value)

	// Future occurrences keep their keys.
	from, until := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)
	b, _, _ := calendar.ExpandAlarms(&calendar.CalendarObject{UID: "alarm-series", ComponentType: "VEVENT", ICalData: ics}, from, until, time.UTC)
	a, _, err := calendar.ExpandAlarms(&calendar.CalendarObject{UID: "alarm-series", ComponentType: "VEVENT", ICalData: got.ICalData}, from, until, time.UTC)
	require.NoError(t, err)
	bk := map[string]bool{}
	for _, i := range b {
		if i.TriggerAt.After(time.Date(2024, 1, 5, 8, 0, 0, 0, time.UTC)) {
			bk[i.Key] = true
		}
	}
	for _, i := range a {
		if i.TriggerAt.After(time.Date(2024, 1, 5, 8, 0, 0, 0, time.UTC)) {
			assert.True(t, bk[i.Key], "key %s of a future occurrence must pre-exist", i.Key)
		}
	}
}

func TestUpdateEvent_ExistingOverrideKeepsOwnAlarm(t *testing.T) {
	extra := "BEGIN:VEVENT\r\nUID:alarm-series\r\nDTSTAMP:20240101T000000Z\r\nRECURRENCE-ID:20240103T100000Z\r\n" +
		"DTSTART:20240103T100000Z\r\nDTEND:20240103T110000Z\r\nSUMMARY:Own\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:own\r\nTRIGGER:-PT5M\r\nEND:VALARM\r\nEND:VEVENT\r\n"
	sum := "Edited"
	_, cal := runUpdate(t, alarmSeries(alarmM15, extra), UpdateEventInput{Summary: &sum, Scope: "this", RecurrenceID: "20240103T100000Z"})
	al := alarmsOf(override(cal))
	require.Len(t, al, 1)
	assert.Equal(t, "-PT5M", al[0].Props.Get(ical.PropTrigger).Value)
}

func TestCopyRelativeAlarms_NoSharedState(t *testing.T) {
	cal, err := ical.NewDecoder(strings.NewReader(alarmSeries(alarmM15, ""))).Decode()
	require.NoError(t, err)
	src := cal.Events()[0].Component
	dst := ical.NewEvent().Component
	copyRelativeAlarms(src, dst)
	da := alarmsOf(dst)
	require.Len(t, da, 1)
	da[0].Props.Get(ical.PropTrigger).Value = "-PT99M"
	da[0].Props.Get(ical.PropTrigger).Params.Set(ical.ParamRelated, "END")
	da[0].Props.Get("X-CUSTOM").Params["FOO"][0] = "changed"
	da[0].Props.Set(&ical.Prop{Name: ical.PropDescription, Value: "other"})
	sa := alarmsOf(src)[0]
	assert.Equal(t, "-PT15M", sa.Props.Get(ical.PropTrigger).Value)
	assert.Equal(t, "START", sa.Props.Get(ical.PropTrigger).Params.Get(ical.ParamRelated))
	assert.Equal(t, "bar", sa.Props.Get("X-CUSTOM").Params.Get("FOO"))
	assert.Equal(t, "Soon", sa.Props.Get(ical.PropDescription).Value)
}
