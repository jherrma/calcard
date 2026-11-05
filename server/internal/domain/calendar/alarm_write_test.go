package calendar

import (
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func i64(n int64) *int64 { return &n }

func atp(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func off(n int64) ReminderInput  { return ReminderInput{OffsetSeconds: i64(n)} }
func abs(s string) ReminderInput { return ReminderInput{At: atp(s)} }

// writeComp decodes a VEVENT with the given VALARM blocks.
func writeComp(t *testing.T, alarms ...string) *ical.Component {
	t.Helper()
	data := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:u\r\nDTSTAMP:20301101T000000Z\r\n" +
		"DTSTART:20301104T100000Z\r\nDTEND:20301104T110000Z\r\n" + strings.Join(alarms, "") + "END:VEVENT\r\nEND:VCALENDAR\r\n"
	cal, err := ical.NewDecoder(strings.NewReader(data)).Decode()
	require.NoError(t, err)
	return cal.Events()[0].Component
}

func enc(t *testing.T, c *ical.Component) string {
	t.Helper()
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "x")
	cal.Children = append(cal.Children, c)
	var sb strings.Builder
	require.NoError(t, ical.NewEncoder(&sb).Encode(cal))
	return sb.String()
}

func triggerList(c *ical.Component) []string {
	var out []string
	for _, a := range c.Children {
		p := a.Props.Get(ical.PropTrigger)
		s := p.Value
		if v := p.Params.Get(ical.ParamValue); v != "" {
			s = v + ":" + s
		}
		if v := p.Params.Get(ical.ParamRelated); v != "" {
			s = v + ":" + s
		}
		out = append(out, s)
	}
	return out
}

const (
	aM15   = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Soon\r\nTRIGGER;RELATED=START:-PT15M\r\nX-KEEP;P=1:yes\r\nUID:keep-1\r\nEND:VALARM\r\n"
	aP1H   = "BEGIN:VALARM\r\nACTION:AUDIO\r\nTRIGGER:PT1H\r\nEND:VALARM\r\n"
	aEmail = "BEGIN:VALARM\r\nACTION:EMAIL\r\nSUMMARY:s\r\nDESCRIPTION:d\r\nTRIGGER:-PT30M\r\nEND:VALARM\r\n"
	aEnd   = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;RELATED=END:-PT5M\r\nEND:VALARM\r\n"
	aRep   = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT10M\r\nREPEAT:2\r\nDURATION:PT1M\r\nEND:VALARM\r\n"
	aAudio = "BEGIN:VALARM\r\nACTION:AUDIO\r\nTRIGGER:-PT20M\r\nATTACH:http://x/y.wav\r\nEND:VALARM\r\n"
	aAbs   = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20301104T080000Z\r\nACKNOWLEDGED:20301104T080100Z\r\nEND:VALARM\r\n"
	aFloat = "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;VALUE=DATE-TIME:20301104T080000\r\nEND:VALARM\r\n"
)

func TestFormatAlarmDuration(t *testing.T) {
	// 8 days is not whole weeks: P8D.
	for in, want := range map[int64]string{
		0: "PT0S", 32400: "PT9H", -54000: "-PT15H", -140400: "-P1DT15H", -572400: "-P6DT15H",
		-604800: "-P1W", -900: "-PT15M", 1209600: "P2W", 86400: "P1D", 90: "PT1M30S", 3661: "PT1H1M1S",
		-691200: "-P8D", 34560000: "P400D",
	} {
		assert.Equal(t, want, FormatAlarmDuration(in), "%d", in)
		// What the formatter writes parses back to the same offset.
		neg, d, s, err := parseAlarmDuration(FormatAlarmDuration(in))
		require.NoError(t, err)
		got := int64(d)*86400 + s
		if neg {
			got = -got
		}
		assert.Equal(t, in, got)
	}
}

func TestApplyReminders_AppendsAndWritesCanonicalVALARM(t *testing.T) {
	c := writeComp(t, aM15)
	changed, appended := ApplyReminders(c, []ReminderInput{off(-900), off(-3600), abs("2026-11-05T09:00:00+01:00")}, ReminderKindEvent)
	require.True(t, changed)
	require.Len(t, appended, 2)
	assert.Equal(t, []string{"START:-PT15M", "-PT1H", "DATE-TIME:20261105T080000Z"}, triggerList(c))
	assert.Same(t, c.Children[1], appended[0], "appended after the existing VALARM, in request order")
	a := c.Children[1]
	assert.Equal(t, "DISPLAY", a.Props.Get(ical.PropAction).Value)
	assert.Equal(t, "Reminder", a.Props.Get(ical.PropDescription).Value)
	uid := a.Props.Get(ical.PropUID).Value
	assert.Len(t, uid, 36)
	assert.NotEqual(t, uid, c.Children[2].Props.Get(ical.PropUID).Value)
	assert.Empty(t, a.Props.Get(ical.PropTrigger).Params.Get(ical.ParamRelated), "events omit RELATED")
	out := enc(t, c)
	assert.Contains(t, out, "TRIGGER:-PT1H\r\n")
	assert.Contains(t, out, "TRIGGER;VALUE=DATE-TIME:20261105T080000Z\r\n")
}

func TestApplyReminders_TaskWritesRelatedEnd(t *testing.T) {
	c := ical.NewComponent(ical.CompToDo)
	c.Props.SetText(ical.PropUID, "t")
	c.Props.SetDateTime(ical.PropDateTimeStamp, time.Unix(0, 0).UTC())
	changed, _ := ApplyReminders(c, []ReminderInput{off(-900)}, ReminderKindTask)
	require.True(t, changed)
	assert.Equal(t, "END", c.Children[0].Props.Get(ical.PropTrigger).Params.Get(ical.ParamRelated))
	assert.Contains(t, enc(t, c), "TRIGGER;RELATED=END:-PT15M")
}

func TestApplyReminders_MatchedAreUntouchedAndUnchanged(t *testing.T) {
	c := writeComp(t, aM15, aAbs)
	before := enc(t, c)
	changed, appended := ApplyReminders(c,
		[]ReminderInput{abs("2030-11-04T09:00:00+01:00"), off(-900)}, ReminderKindEvent)
	assert.False(t, changed)
	assert.Empty(t, appended)
	assert.Equal(t, before, enc(t, c), "byte for byte")
	assert.Empty(t, UnmatchedReminders(c, []ReminderInput{off(-900)}, ReminderKindEvent))
}

func TestApplyReminders_RemovesUnmatchedEditableKeepsOthers(t *testing.T) {
	c := writeComp(t, aM15, aEmail, aP1H, aEnd, aRep, aAudio, aAbs, aFloat)
	beforeKept := []string{}
	for _, i := range []int{1, 3, 4, 7} { // EMAIL, RELATED=END, REPEAT, floating
		beforeKept = append(beforeKept, enc(t, &ical.Component{Name: "X", Children: []*ical.Component{c.Children[i]}}))
	}
	for _, desired := range [][]ReminderInput{{}, nil} {
		c2 := writeComp(t, aM15, aEmail, aP1H, aEnd, aRep, aAudio, aAbs, aFloat)
		changed, appended := ApplyReminders(c2, desired, ReminderKindEvent)
		require.True(t, changed)
		assert.Empty(t, appended)
		require.Len(t, c2.Children, 4, "-PT15M, PT1H, -PT20M AUDIO and the absolute are editable and gone")
		for i, idx := range []int{1, 3, 4, 7} {
			assert.Equal(t, beforeKept[i], enc(t, &ical.Component{Name: "X", Children: []*ical.Component{c2.Children[i]}}), "non-editable %d untouched", idx)
		}
	}
}

func TestApplyReminders_MultisetAndOrder(t *testing.T) {
	c := writeComp(t, aM15, aM15, aP1H)
	// One -PT15M wanted: the first stays, the second (an unmatched duplicate) goes.
	changed, appended := ApplyReminders(c, []ReminderInput{off(-900), off(3600)}, ReminderKindEvent)
	require.True(t, changed)
	assert.Empty(t, appended)
	assert.Equal(t, []string{"START:-PT15M", "PT1H"}, triggerList(c))

	c = writeComp(t, aM15)
	changed, appended = ApplyReminders(c, []ReminderInput{off(-900), off(-900)}, ReminderKindEvent)
	require.True(t, changed)
	require.Len(t, appended, 1, "duplicates are a multiset: the second is new")
	assert.Equal(t, []string{"START:-PT15M", "-PT15M"}, triggerList(c))
}

func TestApplyReminders_KeptKeepEveryProperty(t *testing.T) {
	c := writeComp(t, aM15, aAbs)
	ApplyReminders(c, []ReminderInput{off(-900), abs("2030-11-04T08:00:00Z"), off(-60)}, ReminderKindEvent)
	out := enc(t, c)
	for _, s := range []string{"X-KEEP;P=1:yes", "UID:keep-1", "ACKNOWLEDGED:20301104T080100Z"} {
		assert.Contains(t, out, s)
	}
}

func TestApplyReminders_AtMatchesToTheSecond(t *testing.T) {
	c := writeComp(t, aAbs)
	changed, _ := ApplyReminders(c, []ReminderInput{abs("2030-11-04T08:00:00.9Z")}, ReminderKindEvent)
	assert.False(t, changed, "sub-second part ignored")
	changed, appended := ApplyReminders(c, []ReminderInput{abs("2030-11-04T08:00:01Z")}, ReminderKindEvent)
	assert.True(t, changed)
	assert.Len(t, appended, 1)
	assert.Equal(t, []string{"DATE-TIME:20301104T080001Z"}, triggerList(c))
}

func TestApplyReminders_RelativeMatchesNominalDays(t *testing.T) {
	c := writeComp(t, "BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-P1DT15H\r\nEND:VALARM\r\n")
	changed, _ := ApplyReminders(c, []ReminderInput{off(-140400)}, ReminderKindEvent)
	assert.False(t, changed)
}

func TestApplyReminders_NilComponent(t *testing.T) {
	changed, appended := ApplyReminders(nil, []ReminderInput{off(1)}, ReminderKindEvent)
	assert.False(t, changed)
	assert.Nil(t, appended)
}

func TestReminderChecks(t *testing.T) {
	assert.NoError(t, CheckReminderShape(make([]ReminderInput, 0)))
	assert.ErrorIs(t, CheckReminderShape(make([]ReminderInput, 11)), ErrReminderCount)
	assert.ErrorIs(t, CheckReminderShape([]ReminderInput{{}}), ErrReminderShape)
	assert.ErrorIs(t, CheckReminderShape([]ReminderInput{{OffsetSeconds: i64(1), At: atp("2030-01-01T00:00:00Z")}}), ErrReminderShape)

	start := time.Date(2030, 11, 4, 10, 0, 0, 0, time.UTC)
	assert.NoError(t, CheckReminderRanges([]ReminderInput{off(400 * 86400), off(-400 * 86400)}, start))
	assert.ErrorIs(t, CheckReminderRanges([]ReminderInput{off(400*86400 + 1)}, start), ErrReminderOffsetRange)
	assert.ErrorIs(t, CheckReminderRanges([]ReminderInput{off(-400*86400 - 1)}, start), ErrReminderOffsetRange)
	assert.NoError(t, CheckReminderRanges([]ReminderInput{{At: &start}}, start))
	far := start.AddDate(0, 0, 401)
	assert.ErrorIs(t, CheckReminderRanges([]ReminderInput{{At: &far}}, start), ErrReminderTimeRange)
	near := start.AddDate(0, 0, -399)
	assert.NoError(t, CheckReminderRanges([]ReminderInput{{At: &near}}, start))
}

func TestUnmatchedRemindersOnlyUnmatched(t *testing.T) {
	c := writeComp(t, aM15, aAbs)
	got := UnmatchedReminders(c, []ReminderInput{off(-900), abs("2030-11-04T08:00:00Z"), off(-60), off(-900)}, ReminderKindEvent)
	require.Len(t, got, 2)
	assert.Equal(t, int64(-60), *got[0].OffsetSeconds)
	assert.Equal(t, int64(-900), *got[1].OffsetSeconds)
}
