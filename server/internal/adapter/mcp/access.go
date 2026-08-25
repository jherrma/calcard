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
	perm, err := s.deps.CalendarRepo.GetUserPermission(cc.ctx, calendarID, cc.userID)
	if err != nil {
		return calendar.PermissionNone
	}
	return perm
}

// eventPermission returns the caller's permission on the calendar holding the
// given event, or PermissionNone if the event is unknown or unreachable.
func (s *Server) eventPermission(cc *callContext, eventUUID string) (*calendar.CalendarObject, calendar.CalendarPermission) {
	obj, err := s.deps.CalendarRepo.GetCalendarObjectByUUID(cc.ctx, eventUUID)
	if err != nil || obj == nil {
		return nil, calendar.PermissionNone
	}
	return obj, s.calendarPermission(cc, obj.CalendarID)
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
