package alarm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/usecase/ownerzone"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// fakeCalendars implements the two reads the use case makes; every other method
// panics (nil embedded interface), which proves nothing else is called.
type fakeCalendars struct {
	calendar.CalendarRepository
	readable     []calendar.ReadableCalendar
	objects      []*calendar.CalendarObject
	queriedIDs   [][]uint
	queriedFrom  time.Time
	queriedUntil time.Time
	readErr      error
	candidateErr error
}

func (f *fakeCalendars) ListReadableCalendarIDs(context.Context, uint) ([]calendar.ReadableCalendar, error) {
	return f.readable, f.readErr
}

func (f *fakeCalendars) ListAlarmCandidates(_ context.Context, ids []uint, from, until time.Time) ([]*calendar.CalendarObject, error) {
	f.queriedIDs = append(f.queriedIDs, ids)
	f.queriedFrom, f.queriedUntil = from, until
	if f.candidateErr != nil {
		return nil, f.candidateErr
	}
	var out []*calendar.CalendarObject
	for _, o := range f.objects {
		for _, id := range ids {
			if o.CalendarID == id {
				out = append(out, o)
			}
		}
	}
	return out, nil
}

type fakePrefs struct {
	user.UserPreferenceRepository
	zones map[uint]string
	calls [][]uint
}

func (f *fakePrefs) ListByKey(_ context.Context, ids []uint, key string) ([]user.UserPreference, error) {
	f.calls = append(f.calls, ids)
	var out []user.UserPreference
	for _, id := range ids {
		if z, ok := f.zones[id]; ok {
			out = append(out, user.UserPreference{UserID: id, Key: key, Value: z})
		}
	}
	return out, nil
}

type fakeUsers struct {
	user.UserRepository
}

func (fakeUsers) GetByID(_ context.Context, id uint) (*user.User, error) {
	return &user.User{UUID: fmt.Sprintf("uuid-of-%d", id), Email: fmt.Sprintf("secret%d@example.test", id)}, nil
}

func newUC(cals *fakeCalendars, prefs *fakePrefs) *UpcomingUseCase {
	return newUCWithAcks(cals, prefs, newFakeAcks())
}

func newUCWithAcks(cals *fakeCalendars, prefs *fakePrefs, acks *fakeAcks) *UpcomingUseCase {
	return NewUpcomingUseCase(cals, acks, ownerzone.New(prefs, fakeUsers{}), func() time.Time { return testNow })
}

func stamp(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

// timedObject is an event starting at start with a TRIGGER:-PT15M alarm.
func timedObject(id, calID uint, uid string, start time.Time) *calendar.CalendarObject {
	return &calendar.CalendarObject{ID: id, UUID: fmt.Sprintf("obj-%d", id), CalendarID: calID, UID: uid, ComponentType: "VEVENT",
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:" + stamp(start) +
			"\r\nDTEND:" + stamp(start.Add(time.Hour)) + "\r\nSUMMARY:Meeting\r\nBEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"}
}

// allDayObject is an all-day event on 2026-11-05 with TRIGGER:PT9H.
func allDayObject(id, calID uint, uid string) *calendar.CalendarObject {
	return &calendar.CalendarObject{ID: id, UUID: fmt.Sprintf("obj-%d", id), CalendarID: calID, UID: uid, ComponentType: "VEVENT", IsAllDay: true,
		ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTAMP:20260101T000000Z\r\nDTSTART;VALUE=DATE:20261105\r\nDTEND;VALUE=DATE:20261106\r\nBEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:PT9H\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"}
}

func rc(id, owner uint, subscribed bool) calendar.ReadableCalendar {
	return calendar.ReadableCalendar{ID: id, UUID: fmt.Sprintf("cal-%d", id), Name: fmt.Sprintf("Cal %d", id), Color: "#123456",
		SupportedComponents: "VEVENT,VTODO", OwnerID: owner, Subscribed: subscribed}
}

func uids(out *UpcomingOutput) []string {
	var r []string
	for _, a := range out.Alarms {
		r = append(r, a.UID)
	}
	return r
}

func TestUpcoming_OwnedAndSharedButNeverSubscribed(t *testing.T) {
	cals := &fakeCalendars{
		// 1 owned, 2 shared by user 7, 3 a subscribed mirror (owned), 4 a
		// subscribed mirror shared by user 7. Calendar 9 belongs to a stranger
		// and is not in the readable set.
		readable: []calendar.ReadableCalendar{rc(1, 5, false), rc(2, 7, false), rc(3, 5, true), rc(4, 7, true)},
		objects: []*calendar.CalendarObject{
			timedObject(1, 1, "own", testNow.Add(time.Hour)),
			timedObject(2, 2, "shared", testNow.Add(2*time.Hour)),
			timedObject(3, 3, "feed-own", testNow.Add(3*time.Hour)),
			timedObject(4, 4, "feed-shared", testNow.Add(4*time.Hour)),
			timedObject(5, 9, "stranger", testNow.Add(5*time.Hour)),
		},
	}
	out, err := newUC(cals, &fakePrefs{}).Execute(context.Background(), 5, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"own", "shared"}, uids(out))
	require.Len(t, cals.queriedIDs, 1)
	assert.Equal(t, []uint{1, 2}, cals.queriedIDs[0], "subscribed and unreadable calendars are never queried")
	assert.Equal(t, "Cal 2", out.Alarms[1].Calendar.Name)
	assert.False(t, out.Truncated)
}

func TestUpcoming_Window(t *testing.T) {
	cals := &fakeCalendars{readable: []calendar.ReadableCalendar{rc(1, 5, false)}}
	uc := newUC(cals, &fakePrefs{})

	out, err := uc.Execute(context.Background(), 5, "")
	require.NoError(t, err)
	assert.Equal(t, testNow.Add(-24*time.Hour), out.From)
	assert.Equal(t, testNow.Add(24*time.Hour), out.Until)
	assert.Equal(t, testNow, out.ServerTime)
	assert.Equal(t, out.From, cals.queriedFrom)

	out, err = uc.Execute(context.Background(), 5, testNow.Add(30*24*time.Hour).Format(time.RFC3339))
	require.NoError(t, err)
	assert.Equal(t, testNow.Add(7*24*time.Hour), out.Until, "clamped to now + 7 d")

	out, err = uc.Execute(context.Background(), 5, testNow.Add(2*time.Hour).In(time.FixedZone("x", 7200)).Format(time.RFC3339))
	require.NoError(t, err)
	assert.Equal(t, testNow.Add(2*time.Hour), out.Until, "an offset is normalised to UTC")

	before := len(cals.queriedIDs)
	out, err = uc.Execute(context.Background(), 5, testNow.Add(-48*time.Hour).Format(time.RFC3339))
	require.NoError(t, err)
	assert.NotNil(t, out.Alarms)
	assert.Empty(t, out.Alarms)
	assert.Equal(t, before, len(cals.queriedIDs), "until < from answers without a query")

	for _, bad := range []string{"2026-11-05T10:00:00", "2026-11-05", "tomorrow", "2026-11-05T10:00:00 01:00"} {
		_, err = uc.Execute(context.Background(), 5, bad)
		assert.ErrorIs(t, err, ErrInvalidUntil, bad)
	}
}

func TestUpcoming_NoReadableCalendar(t *testing.T) {
	cals := &fakeCalendars{readable: []calendar.ReadableCalendar{rc(3, 5, true)}}
	out, err := newUC(cals, &fakePrefs{}).Execute(context.Background(), 5, "")
	require.NoError(t, err)
	assert.Empty(t, out.Alarms)
	assert.Empty(t, cals.queriedIDs, "only a subscribed calendar: nothing to query")
}

func TestUpcoming_DedupeKeepsTheLowestCalendarID(t *testing.T) {
	start := testNow.Add(time.Hour)
	cals := &fakeCalendars{
		readable: []calendar.ReadableCalendar{rc(3, 5, false), rc(8, 7, false)},
		// The row of the higher calendar comes first.
		objects: []*calendar.CalendarObject{timedObject(1, 8, "same", start), timedObject(2, 3, "same", start)},
	}
	out, err := newUC(cals, &fakePrefs{}).Execute(context.Background(), 5, "")
	require.NoError(t, err)
	require.Len(t, out.Alarms, 1)
	assert.Equal(t, uint(3), out.Alarms[0].Calendar.ID)
}

func TestUpcoming_SortAndTruncate(t *testing.T) {
	cals := &fakeCalendars{readable: []calendar.ReadableCalendar{rc(1, 5, false)}}
	// 502 alarms one second apart, offered in reverse order.
	for i := 501; i >= 0; i-- {
		cals.objects = append(cals.objects, timedObject(uint(i+1), 1, fmt.Sprintf("u%03d", i), testNow.Add(time.Hour+time.Duration(i)*time.Second)))
	}
	out, err := newUC(cals, &fakePrefs{}).Execute(context.Background(), 5, "")
	require.NoError(t, err)
	require.Len(t, out.Alarms, MaxAlarms)
	assert.True(t, out.Truncated)
	assert.Equal(t, "u000", out.Alarms[0].UID)
	assert.Equal(t, "u499", out.Alarms[499].UID)
	for i := 1; i < len(out.Alarms); i++ {
		assert.False(t, out.Alarms[i].TriggerAt.Before(out.Alarms[i-1].TriggerAt))
	}
}

func TestUpcoming_SameInstantSortsByKey(t *testing.T) {
	start := testNow.Add(time.Hour)
	cals := &fakeCalendars{readable: []calendar.ReadableCalendar{rc(1, 5, false)}}
	for i := 0; i < 20; i++ {
		cals.objects = append(cals.objects, timedObject(uint(i+1), 1, fmt.Sprintf("tie%d", i), start))
	}
	out, err := newUC(cals, &fakePrefs{}).Execute(context.Background(), 5, "")
	require.NoError(t, err)
	require.Len(t, out.Alarms, 20)
	for i := 1; i < len(out.Alarms); i++ {
		assert.Less(t, out.Alarms[i-1].Key, out.Alarms[i].Key)
	}
}

func TestUpcoming_UndecodableObjectIsSkippedAndLogged(t *testing.T) {
	logs := captureLogs(t)
	bad := timedObject(1, 1, "bad", testNow.Add(time.Hour))
	bad.ICalData = "this is not iCalendar"
	cals := &fakeCalendars{
		readable: []calendar.ReadableCalendar{rc(1, 5, false)},
		objects:  []*calendar.CalendarObject{bad, timedObject(2, 1, "good", testNow.Add(2*time.Hour))},
	}
	out, err := newUC(cals, &fakePrefs{}).Execute(context.Background(), 5, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"good"}, uids(out))
	assert.Contains(t, logs.String(), "level=WARN")
	assert.Contains(t, logs.String(), "obj-1", "the WARN names the object UUID")
	assert.NotContains(t, logs.String(), "not iCalendar", "never its content")
}

func TestUpcoming_ObjectOverTheCapSetsTruncated(t *testing.T) {
	logs := captureLogs(t)
	obj := timedObject(1, 1, "flood", testNow.Add(-20*time.Hour))
	obj.ICalData = strings.Replace(obj.ICalData, "SUMMARY:Meeting", "RRULE:FREQ=MINUTELY;COUNT=2000\r\nSUMMARY:Meeting", 1)
	cals := &fakeCalendars{readable: []calendar.ReadableCalendar{rc(1, 5, false)}, objects: []*calendar.CalendarObject{obj}}
	out, err := newUC(cals, &fakePrefs{}).Execute(context.Background(), 5, "")
	require.NoError(t, err)
	assert.True(t, out.Truncated)
	assert.LessOrEqual(t, len(out.Alarms), calendar.AlarmInstanceCap)
	assert.Contains(t, logs.String(), "instance cap")
}

func TestUpcoming_OwnerZones(t *testing.T) {
	cals := &fakeCalendars{
		// User 5 reads calendar 1 (own, Berlin) and calendars 2 and 3 shared by
		// users 7 (New York) and 8 (no preference).
		readable: []calendar.ReadableCalendar{rc(1, 5, false), rc(2, 7, false), rc(3, 8, false)},
		objects:  []*calendar.CalendarObject{allDayObject(1, 1, "berlin"), allDayObject(2, 2, "newyork"), allDayObject(3, 3, "utc")},
	}
	prefs := &fakePrefs{zones: map[uint]string{5: "Europe/Berlin", 7: "America/New_York"}}
	until := testNow.Add(3 * 24 * time.Hour).Format(time.RFC3339)
	out, err := newUC(cals, prefs).Execute(context.Background(), 5, until)
	require.NoError(t, err)

	got := map[string]time.Time{}
	for _, a := range out.Alarms {
		got[a.UID] = a.TriggerAt
	}
	assert.Equal(t, time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC), got["berlin"])
	assert.Equal(t, time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC), got["newyork"])
	assert.Equal(t, time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC), got["utc"])
	require.Len(t, prefs.calls, 1, "one ListByKey for every owner")
	assert.ElementsMatch(t, []uint{5, 7, 8}, prefs.calls[0])
}

func TestUpcoming_FallbackIsLoggedOncePerOwnerAndOnlyWhenTheZoneMattered(t *testing.T) {
	logs := captureLogs(t)
	cals := &fakeCalendars{
		// Owner 8 has no preference and an all-day alarm: zone dependent. Owner 9
		// has none either, but only a timed alarm: the zone did not matter.
		readable: []calendar.ReadableCalendar{rc(3, 8, false), rc(4, 9, false)},
		objects:  []*calendar.CalendarObject{allDayObject(1, 3, "allday"), timedObject(2, 4, "timed", testNow.Add(time.Hour))},
	}
	uc := newUC(cals, &fakePrefs{})
	until := testNow.Add(3 * 24 * time.Hour).Format(time.RFC3339)
	for i := 0; i < 3; i++ {
		out, err := uc.Execute(context.Background(), 5, until)
		require.NoError(t, err)
		require.Len(t, out.Alarms, 2)
	}
	text := logs.String()
	assert.Equal(t, 1, strings.Count(text, ownerzone.FallbackMessage), "once per owner per process:\n%s", text)
	assert.Contains(t, text, "uuid-of-8", "the owner's user UUID")
	assert.NotContains(t, text, "uuid-of-9")
	assert.NotContains(t, text, "secret", "never the e-mail")
}

func TestUpcoming_StorageErrors(t *testing.T) {
	boom := errors.New("boom")
	_, err := newUC(&fakeCalendars{readErr: boom}, &fakePrefs{}).Execute(context.Background(), 5, "")
	assert.ErrorIs(t, err, boom)
	_, err = newUC(&fakeCalendars{readable: []calendar.ReadableCalendar{rc(1, 5, false)}, candidateErr: boom}, &fakePrefs{}).Execute(context.Background(), 5, "")
	assert.ErrorIs(t, err, boom)
}

type logBuffer struct {
	mu sync.Mutex
	sb strings.Builder
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.String()
}

// captureLogs routes slog's default logger into a buffer for the test.
func captureLogs(t *testing.T) *logBuffer {
	t.Helper()
	buf := &logBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}
