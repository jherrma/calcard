package alarm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAcks is an in-memory AlarmAcknowledgementRepository.
type fakeAcks struct {
	calendar.AlarmAcknowledgementRepository
	rows       map[uint]map[string]time.Time
	lookups    [][]string // keys of every AcknowledgedKeys call
	writes     int
	ackErr     error
	lookupErr  error
	lastWriteN int
}

func newFakeAcks() *fakeAcks { return &fakeAcks{rows: map[uint]map[string]time.Time{}} }

func (f *fakeAcks) Acknowledge(_ context.Context, userID uint, keys []string, at time.Time) error {
	if f.ackErr != nil {
		return f.ackErr
	}
	f.writes++
	f.lastWriteN = len(keys)
	if f.rows[userID] == nil {
		f.rows[userID] = map[string]time.Time{}
	}
	for _, k := range keys {
		if _, ok := f.rows[userID][k]; !ok {
			f.rows[userID][k] = at
		}
	}
	return nil
}

func (f *fakeAcks) AcknowledgedKeys(_ context.Context, userID uint, keys []string) (map[string]bool, error) {
	f.lookups = append(f.lookups, keys)
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	out := map[string]bool{}
	for _, k := range keys {
		if _, ok := f.rows[userID][k]; ok {
			out[k] = true
		}
	}
	return out, nil
}

func hexKey(i int) string { return fmt.Sprintf("%032x", i) }

func newAckUC(f *fakeAcks) *AcknowledgeUseCase {
	return NewAcknowledgeUseCase(f, func() time.Time { return testNow.In(time.FixedZone("x", 5*3600)) })
}

func TestAcknowledge_Validation(t *testing.T) {
	good := hexKey(1)
	many := make([]string, 501)
	for i := range many {
		many[i] = hexKey(i)
	}
	cases := []struct {
		name string
		keys []string
		msg  string
	}{
		{"nil", nil, "keys is required"},
		{"empty", []string{}, "keys is required"},
		{"501 keys", many, "Too many keys: at most 500 per request"},
		{"uppercase", []string{good, strings.ToUpper(hexKey(255))}, "Invalid alarm key at index 1"},
		{"31 chars", []string{hexKey(1)[1:]}, "Invalid alarm key at index 0"},
		{"43 chars", []string{good, good, good + "abcdefghijk"}, "Invalid alarm key at index 2"},
		{"non-hex", []string{good, strings.Repeat("g", 32)}, "Invalid alarm key at index 1"},
		{"first bad index wins", []string{good, "x", "y"}, "Invalid alarm key at index 1"},
		{"empty string", []string{""}, "Invalid alarm key at index 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeAcks()
			err := newAckUC(f).Execute(context.Background(), 1, tc.keys)
			require.Error(t, err)
			assert.Equal(t, tc.msg, err.Error())
			assert.Zero(t, f.writes, "nothing is written for a refused batch")
			assert.Empty(t, f.rows)
		})
	}
}

func TestAcknowledge_WritesForTheCallerOnlyAndCollapsesDuplicates(t *testing.T) {
	f := newFakeAcks()
	uc := newAckUC(f)
	require.NoError(t, uc.Execute(context.Background(), 7, []string{hexKey(1), hexKey(2), hexKey(1)}))
	require.NoError(t, uc.Execute(context.Background(), 7, []string{hexKey(1)})) // idempotent
	assert.Equal(t, 2, len(f.rows[7]))
	assert.Empty(t, f.rows[8])
	at := f.rows[7][hexKey(1)]
	assert.Equal(t, time.UTC, at.Location(), "acknowledged_at is UTC")
	assert.True(t, at.Equal(testNow))
	assert.Equal(t, 1, f.lastWriteN)

	// An unknown key (belongs to nobody's alarm) is accepted: no existence probe.
	require.NoError(t, uc.Execute(context.Background(), 7, []string{hexKey(999)}))
}

func TestAcknowledge_500KeysAreAccepted(t *testing.T) {
	f := newFakeAcks()
	keys := make([]string, 500)
	for i := range keys {
		keys[i] = hexKey(i)
	}
	require.NoError(t, newAckUC(f).Execute(context.Background(), 1, keys))
	assert.Len(t, f.rows[1], 500)
}

func TestAcknowledge_StorageError(t *testing.T) {
	f := newFakeAcks()
	f.ackErr = errors.New("disk full")
	err := newAckUC(f).Execute(context.Background(), 1, []string{hexKey(1)})
	require.Error(t, err)
	assert.ErrorIs(t, err, f.ackErr)
}

func TestUpcoming_AcknowledgedKeysAreDroppedPerUser(t *testing.T) {
	cals := &fakeCalendars{
		readable: []calendar.ReadableCalendar{rc(1, 5, false)},
		objects: []*calendar.CalendarObject{
			timedObject(1, 1, "a", testNow.Add(time.Hour)),
			timedObject(2, 1, "b", testNow.Add(2*time.Hour)),
		},
	}
	acks := newFakeAcks()
	uc := newUCWithAcks(cals, &fakePrefs{}, acks)
	out, err := uc.Execute(context.Background(), 5, "")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, uids(out))
	keyA, keyB := out.Alarms[0].Key, out.Alarms[1].Key

	// Another user acknowledging a's key hides nothing from user 5.
	require.NoError(t, acks.Acknowledge(context.Background(), 6, []string{keyA}, testNow))
	out, err = uc.Execute(context.Background(), 5, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, uids(out))

	require.NoError(t, acks.Acknowledge(context.Background(), 5, []string{keyA}, testNow))
	out, err = uc.Execute(context.Background(), 5, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, uids(out), "exactly the acknowledged key is dropped")
	assert.Equal(t, keyB, out.Alarms[0].Key)
	assert.False(t, out.Truncated)

	// The sharee-side view: user 6 still sees what 5 acknowledged, not what 6 did.
	cals.readable = []calendar.ReadableCalendar{rc(1, 5, false)}
	out, err = uc.Execute(context.Background(), 6, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, uids(out))
}

func TestUpcoming_OneLookupPerFiveHundredKeys(t *testing.T) {
	cals := &fakeCalendars{readable: []calendar.ReadableCalendar{rc(1, 5, false)}}
	for i := 0; i < 1100; i++ {
		cals.objects = append(cals.objects, timedObject(uint(i+1), 1, fmt.Sprintf("uid-%04d", i), testNow.Add(time.Hour+time.Duration(i)*time.Second)))
	}
	acks := newFakeAcks()
	out, err := newUCWithAcks(cals, &fakePrefs{}, acks).Execute(context.Background(), 5, "")
	require.NoError(t, err)
	assert.Len(t, out.Alarms, MaxAlarms)
	assert.True(t, out.Truncated)
	require.Len(t, acks.lookups, 3, "1100 keys: 500 + 500 + 100")
	assert.Len(t, acks.lookups[0], 500)
	assert.Len(t, acks.lookups[2], 100)
}

func TestUpcoming_NoCandidatesNoLookup(t *testing.T) {
	acks := newFakeAcks()
	cals := &fakeCalendars{readable: []calendar.ReadableCalendar{rc(1, 5, false)}}
	_, err := newUCWithAcks(cals, &fakePrefs{}, acks).Execute(context.Background(), 5, "")
	require.NoError(t, err)
	assert.Empty(t, acks.lookups)
}

func TestUpcoming_AcknowledgementLookupError(t *testing.T) {
	acks := newFakeAcks()
	acks.lookupErr = errors.New("boom")
	cals := &fakeCalendars{readable: []calendar.ReadableCalendar{rc(1, 5, false)},
		objects: []*calendar.CalendarObject{timedObject(1, 1, "a", testNow.Add(time.Hour))}}
	_, err := newUCWithAcks(cals, &fakePrefs{}, acks).Execute(context.Background(), 5, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, acks.lookupErr)
}
