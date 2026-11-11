package mcp

import (
	"context"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// callContext is everything a tool needs about the caller. It carries the
// authenticated user id rather than a session, because authorization is
// per-user: two sessions of the same user are indistinguishable to a tool.
//
// Now is injected so scheduling tools ("upcoming", "free slots from now") are
// deterministic under test.
type callContext struct {
	ctx    context.Context
	userID uint
	now    time.Time
}

// resolveCalendar maps a calendar UUID — the canonical external identifier
// (#52) — to its numeric id plus the caller's effective permission.
//
// This mirrors EventHandler.resolveCalendarID/calendarPermission exactly, and
// deliberately calls the same repository method: ownership and shares (#53)
// must resolve identically over MCP and REST, or a tool becomes a way around
// the sharing model. An unresolvable UUID and an inaccessible calendar both
// yield PermissionNone, so tools cannot be used to probe which ids exist.
func (s *Server) resolveCalendar(cc *callContext, uuid string) (uint, calendar.CalendarPermission) {
	cal, err := s.deps.CalendarRepo.GetByUUID(cc.ctx, uuid)
	if err != nil || cal == nil {
		return 0, calendar.PermissionNone
	}
	return cal.ID, s.calendarPermission(cc, cal.ID)
}

// calendarPermission returns the caller's permission on a calendar by numeric
// id, failing closed on repository errors.
func (s *Server) calendarPermission(cc *callContext, calendarID uint) calendar.CalendarPermission {
	_, perm := s.calendarAccess(cc, calendarID)
	return perm
}

// calendarAccess is calendarPermission plus the calendar it loaded: nil and
// PermissionNone when the caller cannot read it or a lookup fails.
func (s *Server) calendarAccess(cc *callContext, calendarID uint) (*calendar.Calendar, calendar.CalendarPermission) {
	perm, err := s.deps.CalendarRepo.GetUserPermission(cc.ctx, calendarID, cc.userID)
	if err != nil || perm == calendar.PermissionNone {
		return nil, calendar.PermissionNone
	}
	// Same cap the REST handler applies: a subscribed calendar (story 100) is
	// a read-only mirror of a remote feed, so an assistant may read it and may
	// not write to it, whoever it is acting for.
	cal, err := s.deps.CalendarRepo.GetByID(cc.ctx, calendarID)
	if err != nil || cal == nil {
		return nil, calendar.PermissionNone
	}
	return cal, calendar.EffectivePermission(cal, perm)
}

// loadCalendar maps a calendar UUID to the calendar and the caller's effective
// permission on it, the mirror of the REST handlers' resolveCalendar plus
// calendarPermission: an unknown UUID, a failed lookup and a calendar the
// caller cannot read are all (nil, PermissionNone). create_task hands the
// calendar to the use case, which needs it whole (its component set).
func (s *Server) loadCalendar(cc *callContext, calendarUUID string) (*calendar.Calendar, calendar.CalendarPermission) {
	cal, err := s.deps.CalendarRepo.GetByUUID(cc.ctx, calendarUUID)
	if err != nil || cal == nil {
		return nil, calendar.PermissionNone
	}
	return s.calendarAccess(cc, cal.ID)
}

// eventPermission returns the caller's permission on the calendar holding the
// given event, or PermissionNone if the event is unknown or unreachable.
//
// Like EventHandler.eventPermission, an object that is not an event (a task,
// #170) is an unknown event, checked before the permission, so the event tools
// give a task UUID the same "No event with id …" answer as an unknown one,
// also on a calendar shared read-only.
func (s *Server) eventPermission(cc *callContext, eventUUID string) (*calendar.CalendarObject, calendar.CalendarPermission) {
	obj, err := s.deps.CalendarRepo.GetCalendarObjectByUUID(cc.ctx, eventUUID)
	if err != nil || !obj.IsEvent() {
		return nil, calendar.PermissionNone
	}
	return obj, s.calendarPermission(cc, obj.CalendarID)
}

// taskPermission is eventPermission for the task tools (#170): the task, the
// calendar that holds it and the caller's effective permission there. An
// object that is not a task (CalendarObject.IsTask; an event's UUID), an
// unknown UUID and a task in a calendar the caller cannot read are all (nil,
// nil, PermissionNone), checked before anything else, so every one of them
// gets the same "No task with id …" answer — also on a read-only share. The
// calendar is the task's own: the task use cases re-check that the object is
// a task of it (taskuc's loadTask), as the REST routes do with the URL's.
func (s *Server) taskPermission(cc *callContext, taskUUID string) (*calendar.CalendarObject, *calendar.Calendar, calendar.CalendarPermission) {
	obj, err := s.deps.CalendarRepo.GetCalendarObjectByUUID(cc.ctx, taskUUID)
	if err != nil || !obj.IsTask() {
		return nil, nil, calendar.PermissionNone
	}
	cal, perm := s.calendarAccess(cc, obj.CalendarID)
	if perm == calendar.PermissionNone {
		return nil, nil, calendar.PermissionNone
	}
	return obj, cal, perm
}

// canWriteCalendar reports whether a calendar permission grants writes.
func canWriteCalendar(p calendar.CalendarPermission) bool {
	return p == calendar.PermissionOwner || p == calendar.PermissionReadWrite
}

// resolveAddressBook maps an address-book UUID to its numeric id plus the
// caller's permission, with the same fail-closed contract as resolveCalendar.
func (s *Server) resolveAddressBook(cc *callContext, uuid string) (uint, addressbook.AddressBookPermission) {
	ab, err := s.deps.AddressBookRepo.GetByUUID(cc.ctx, uuid)
	if err != nil || ab == nil {
		return 0, addressbook.PermissionNone
	}
	return ab.ID, s.addressBookPermission(cc, ab.ID)
}

// resolveOwnedAddressBook maps an address-book UUID to the book, but only for
// one the caller OWNS — the address-book collection tools (update/delete) are
// owner-only, like their REST routes.
//
// This mirrors AddressBookHandler.resolveOwnedAddressBookID exactly: the same
// repository lookup and the same ownership comparison, with an unknown UUID, a
// lookup failure, another user's book and a book merely shared with the caller
// all reported as "not found" (F05), so a tool cannot be used to learn that
// someone else's book exists. Ownership is compared on UserID rather than via
// GetUserPermission because that is what REST does; the update and delete use
// cases then check it again themselves.
func (s *Server) resolveOwnedAddressBook(cc *callContext, uuid string) (*addressbook.AddressBook, bool) {
	ab, err := s.deps.AddressBookRepo.GetByUUID(cc.ctx, uuid)
	if err != nil || ab == nil || ab.UserID != cc.userID {
		return nil, false
	}
	return ab, true
}

// addressBookPermission returns the caller's permission on a book by numeric
// id, failing closed on repository errors.
func (s *Server) addressBookPermission(cc *callContext, abID uint) addressbook.AddressBookPermission {
	perm, err := s.deps.AddressBookRepo.GetUserPermission(cc.ctx, abID, cc.userID)
	if err != nil {
		return addressbook.PermissionNone
	}
	return perm
}

// contactPermission resolves a contact UUID to its stored object plus the
// caller's permission on the book that holds it.
func (s *Server) contactPermission(cc *callContext, contactUUID string) (uint, addressbook.AddressBookPermission) {
	obj, err := s.deps.AddressBookRepo.GetObjectByUUID(cc.ctx, contactUUID)
	if err != nil || obj == nil {
		return 0, addressbook.PermissionNone
	}
	return obj.AddressBookID, s.addressBookPermission(cc, obj.AddressBookID)
}
