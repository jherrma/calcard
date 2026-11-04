package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

func alarmsOf(t *testing.T, valarms ...string) []calendar.AlarmSpec {
	t.Helper()
	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:a\r\nDTSTAMP:20261101T000000Z\r\nDTSTART:20261104T100000Z\r\n"
	for _, v := range valarms {
		body += "BEGIN:VALARM\r\n" + strings.ReplaceAll(v, "|", "\r\n") + "\r\nEND:VALARM\r\n"
	}
	body += "END:VEVENT\r\nEND:VCALENDAR\r\n"
	cal, err := ical.NewDecoder(strings.NewReader(body)).Decode()
	require.NoError(t, err)
	return calendar.ParseAlarms(cal.Children[0])
}

func TestNewReminderResponses(t *testing.T) {
	cases := []struct {
		name     string
		valarm   string
		kind     ReminderKind
		action   string
		related  string // "" = null
		offset   *int64
		at       string // "" = null
		editable bool
	}{
		{"relative start", "ACTION:DISPLAY|TRIGGER:-PT15M", ReminderKindEvent, "display", "start", i64(-900), "", true},
		{"action absent", "TRIGGER:-PT15M", ReminderKindEvent, "display", "start", i64(-900), "", true},
		{"audio", "ACTION:AUDIO|TRIGGER:-PT15M", ReminderKindEvent, "audio", "start", i64(-900), "", true},
		{"nominal day", "ACTION:DISPLAY|TRIGGER:-P1D", ReminderKindEvent, "display", "start", i64(-86400), "", true},
		{"related end on event", "ACTION:DISPLAY|TRIGGER;RELATED=END:PT0S", ReminderKindEvent, "display", "end", i64(0), "", false},
		{"email", "ACTION:EMAIL|TRIGGER:-PT15M", ReminderKindEvent, "email", "start", i64(-900), "", false},
		{"repeat", "ACTION:DISPLAY|TRIGGER:-PT15M|REPEAT:2|DURATION:PT5M", ReminderKindEvent, "display", "start", i64(-900), "", false},
		{"snooze", "ACTION:DISPLAY|TRIGGER:PT5M|RELATED-TO;RELTYPE=SNOOZE:x", ReminderKindEvent, "display", "start", i64(300), "", false},
		{"no trigger", "ACTION:DISPLAY", ReminderKindEvent, "display", "", nil, "", false},
		{"absolute utc", "ACTION:DISPLAY|TRIGGER;VALUE=DATE-TIME:20261104T080000Z", ReminderKindEvent, "display", "", nil, "2026-11-04T08:00:00Z", true},
		{"absolute floating", "ACTION:DISPLAY|TRIGGER;VALUE=DATE-TIME:20261104T080000", ReminderKindEvent, "display", "", nil, "2026-11-04T08:00:00Z", false},
		{"task end", "ACTION:DISPLAY|TRIGGER;RELATED=END:-PT30M", ReminderKindTask, "display", "end", i64(-1800), "", true},
		{"task start", "ACTION:DISPLAY|TRIGGER:-PT30M", ReminderKindTask, "display", "start", i64(-1800), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewReminderResponses(alarmsOf(t, tc.valarm), tc.kind)
			require.Len(t, got, 1)
			r := got[0]
			assert.Equal(t, 0, r.Index)
			assert.Equal(t, tc.action, r.Action)
			if tc.related == "" {
				assert.Nil(t, r.Related)
			} else {
				require.NotNil(t, r.Related)
				assert.Equal(t, tc.related, *r.Related)
			}
			assert.Equal(t, tc.offset, r.OffsetSeconds)
			if tc.at == "" {
				assert.Nil(t, r.At)
			} else {
				require.NotNil(t, r.At)
				assert.Equal(t, tc.at, r.At.Format(time.RFC3339))
			}
			assert.Equal(t, tc.editable, r.Editable)
		})
	}

	t.Run("always an array", func(t *testing.T) {
		raw, err := json.Marshal(NewReminderResponses(nil, ReminderKindEvent))
		require.NoError(t, err)
		assert.Equal(t, "[]", string(raw))
	})

	t.Run("index and wire keys", func(t *testing.T) {
		got := NewReminderResponses(alarmsOf(t, "TRIGGER:-PT5M", "TRIGGER:-PT10M"), ReminderKindEvent)
		require.Len(t, got, 2)
		assert.Equal(t, 1, got[1].Index)
		raw, _ := json.Marshal(got[0])
		assert.JSONEq(t, `{"index":0,"action":"display","related":"start","offset_seconds":-300,"at":null,"editable":true}`, string(raw))
	})
}

func i64(v int64) *int64 { return &v }
