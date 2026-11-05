package task

import (
	"context"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// stubRepo implements only what the task use cases call. The embedded
// interface is nil, so any other repository call panics: a passing test proves
// the use case made no other query.
type stubRepo struct {
	calendar.CalendarRepository

	objects map[string]*calendar.CalendarObject
	lookErr error

	listed    []*calendar.CalendarObject
	listTotal int64
	listErr   error
	queries   []calendar.TaskQuery

	// readable is what ListReadableCalendarIDs answers; readableUsers
	// records the user of every call.
	readable      []calendar.ReadableCalendar
	readableErr   error
	readableUsers []uint

	// counts is what CountTasks answers; countCalls records the arguments
	// of every call.
	counts     calendar.TaskCounts
	countsErr  error
	countCalls []countCall

	created   []*calendar.CalendarObject
	createErr error

	// updated and deleted record copies of the objects as they were stored, so
	// a later change of the caller's object cannot hide what was written.
	updated     []calendar.CalendarObject
	updateErr   error
	updateErrs  []error
	loadedETags []string
	deleted     []calendar.CalendarObject
	deleteErr   error
}

// UpdateCalendarObjectIfETag records the write, like the repository after its
// conditional UPDATE matched; updateErr stands for a refused one (ErrNotFound,
// ErrETagMismatch, a storage error) and updateErrs, when set, is consumed one
// error per call before updateErr applies. loadedETags records the condition.
func (r *stubRepo) UpdateCalendarObjectIfETag(_ context.Context, obj *calendar.CalendarObject, loadedETag string) error {
	r.loadedETags = append(r.loadedETags, loadedETag)
	if len(r.updateErrs) > 0 {
		err := r.updateErrs[0]
		r.updateErrs = r.updateErrs[1:]
		if err != nil {
			return err
		}
	} else if r.updateErr != nil {
		return r.updateErr
	}
	r.updated = append(r.updated, *obj)
	return nil
}

// HardDeleteCalendarObject records the delete. There is deliberately no
// DeleteCalendarObject: a task DELETE that soft-deleted would call the nil
// embedded interface and panic.
func (r *stubRepo) HardDeleteCalendarObject(_ context.Context, obj *calendar.CalendarObject) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.deleted = append(r.deleted, *obj)
	return nil
}

func (r *stubRepo) GetCalendarObjectByUUID(_ context.Context, uuid string) (*calendar.CalendarObject, error) {
	if r.lookErr != nil {
		return nil, r.lookErr
	}
	obj, ok := r.objects[uuid]
	if !ok {
		return nil, errStubNotFound
	}
	return obj, nil
}

func (r *stubRepo) ListAlarmCandidates(context.Context, []uint, time.Time, time.Time) ([]*calendar.CalendarObject, error) {
	return nil, nil
}

func (r *stubRepo) ListTasks(_ context.Context, q calendar.TaskQuery) ([]*calendar.CalendarObject, int64, error) {
	r.queries = append(r.queries, q)
	return r.listed, r.listTotal, r.listErr
}

func (r *stubRepo) ListReadableCalendarIDs(_ context.Context, userID uint) ([]calendar.ReadableCalendar, error) {
	r.readableUsers = append(r.readableUsers, userID)
	if r.readableErr != nil {
		return nil, r.readableErr
	}
	return r.readable, nil
}

// countCall is one CountTasks call of the stub.
type countCall struct {
	ids  []uint
	asOf time.Time
}

func (r *stubRepo) CountTasks(_ context.Context, calendarIDs []uint, asOf time.Time) (calendar.TaskCounts, error) {
	r.countCalls = append(r.countCalls, countCall{ids: calendarIDs, asOf: asOf})
	if r.countsErr != nil {
		return calendar.TaskCounts{}, r.countsErr
	}
	return r.counts, nil
}

func (r *stubRepo) CreateCalendarObject(_ context.Context, obj *calendar.CalendarObject) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.created = append(r.created, obj)
	return nil
}

// errStubNotFound stands in for the repository's "record not found".
var errStubNotFound = stubError("record not found")

type stubError string

func (e stubError) Error() string { return string(e) }

// stubTask is a stored task object of calendarID as a CalDAV client wrote it.
func stubTask(uuid string, calendarID uint, props ...string) *calendar.CalendarObject {
	data := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VTODO\r\nUID:" + uuid +
		"@x\r\nDTSTAMP:20300101T000000Z\r\nSUMMARY:Task " + uuid + "\r\n"
	for _, p := range props {
		data += p + "\r\n"
	}
	obj := &calendar.CalendarObject{UUID: uuid, CalendarID: calendarID, ICalData: data + "END:VTODO\r\nEND:VCALENDAR\r\n"}
	if err := obj.PopulateDenormFieldsFromICal(); err != nil {
		panic(err)
	}
	return obj
}

// stubEvent is a stored event object of calendarID.
func stubEvent(uuid string, calendarID uint) *calendar.CalendarObject {
	obj := &calendar.CalendarObject{UUID: uuid, CalendarID: calendarID, ICalData: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
		"PRODID:-//test//EN\r\nBEGIN:VEVENT\r\nUID:" + uuid + "@x\r\nDTSTAMP:20300101T000000Z\r\n" +
		"DTSTART:20301105T090000Z\r\nDTEND:20301105T100000Z\r\nRRULE:FREQ=DAILY\r\nSUMMARY:Event\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"}
	if err := obj.PopulateDenormFieldsFromICal(); err != nil {
		panic(err)
	}
	return obj
}
