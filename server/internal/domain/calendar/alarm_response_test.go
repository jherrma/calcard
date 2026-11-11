package calendar

import (
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const alarmResponseSeries = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:s\r\nDTSTAMP:20261101T000000Z\r\nDTSTART:20261104T100000Z\r\nDTEND:20261104T110000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:s\r\nDTSTAMP:20261101T000000Z\r\nRECURRENCE-ID:20261105T100000Z\r\nDTSTART:20261105T100000Z\r\nDTEND:20261105T110000Z\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT1H\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestExpandRecurringEvent_AlarmsOverrideVsMaster(t *testing.T) {
	obj := &CalendarObject{UUID: "u", UID: "s", ICalData: alarmResponseSeries}
	from := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	insts, err := ExpandRecurringEvent(obj, from, from.AddDate(0, 0, 5))
	require.NoError(t, err)
	require.Len(t, insts, 3)
	for _, inst := range insts {
		require.Len(t, inst.Alarms, 1)
		want := int64(900)
		if inst.IsException {
			want = 3600
		}
		assert.Equal(t, want, inst.Alarms[0].Offset.Secs, inst.RecurrenceID)
	}
}

func TestEventAlarms(t *testing.T) {
	assert.Nil(t, (&CalendarObject{ICalData: "garbage"}).EventAlarms())
	got := (&CalendarObject{ICalData: alarmResponseSeries}).EventAlarms()
	require.Len(t, got, 1)
	assert.Equal(t, int64(900), got[0].Offset.Secs, "the master's alarms, not the override's")
}

func TestTaskDetails_Alarms(t *testing.T) {
	data := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VTODO\r\nUID:t\r\nDTSTAMP:20261101T000000Z\r\nDUE:20261104T100000Z\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;RELATED=END:-PT30M\r\nEND:VALARM\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	_, err := ical.NewDecoder(strings.NewReader(data)).Decode()
	require.NoError(t, err)
	d := (&CalendarObject{ICalData: data}).TaskDetails()
	require.Len(t, d.Alarms, 1)
	assert.Equal(t, "END", d.Alarms[0].Related)
	assert.Empty(t, (&CalendarObject{ICalData: "x"}).TaskDetails().Alarms)
}
