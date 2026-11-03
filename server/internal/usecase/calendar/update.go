package calendar

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// UpdateCalendarRequest represents the request to update a calendar
type UpdateCalendarRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	Timezone    *string `json:"timezone"`
	// Kind may only WIDEN what the calendar stores (#170): "both" turns an
	// event calendar or a task list into a mixed collection. The calendar's
	// current kind is a no-op; any kind that would drop a component it stores
	// is calendar.ErrConflict, and an unknown kind is calendar.ErrInvalidInput,
	// as on create. Empty (absent) leaves the component set alone.
	Kind string `json:"kind"`
}

// UpdateCalendarUseCase handles calendar updates
type UpdateCalendarUseCase struct {
	repo calendar.CalendarRepository
}

// NewUpdateCalendarUseCase creates a new use case
func NewUpdateCalendarUseCase(repo calendar.CalendarRepository) *UpdateCalendarUseCase {
	return &UpdateCalendarUseCase{repo: repo}
}

// errNarrowComponentSet is the refusal of every kind change that would drop a
// component (#170).
func errNarrowComponentSet() error {
	return calendar.Conflictf("Cannot narrow a calendar's component set")
}

// Execute updates a calendar. Every field is validated, and a kind change
// checked, before anything is written, so a refused request (400 or 409)
// changes nothing.
func (uc *UpdateCalendarUseCase) Execute(ctx context.Context, userID uint, calendarUUID string, req UpdateCalendarRequest) (*calendar.Calendar, error) {
	// Get existing calendar. Missing and not owned are the same ErrNotFound.
	cal, err := LoadOwnedCalendar(ctx, uc.repo, userID, calendarUUID)
	if err != nil {
		return nil, err
	}

	// Update fields if provided
	if req.Name != nil {
		if err := calendar.ValidateName(*req.Name); err != nil {
			return nil, err
		}
		cal.Name = *req.Name
	}

	if req.Description != nil {
		if err := calendar.ValidateDescription(*req.Description); err != nil {
			return nil, err
		}
		cal.Description = *req.Description
	}

	if req.Color != nil {
		if err := calendar.ValidateHexColor(*req.Color); err != nil {
			return nil, err
		}
		cal.Color = *req.Color
	}

	if req.Timezone != nil {
		if err := calendar.ValidateTimezone(*req.Timezone); err != nil {
			return nil, err
		}
		cal.Timezone = *req.Timezone
	}

	widenedSet, widen, err := widenComponents(cal, req.Kind)
	if err != nil {
		return nil, err
	}

	// Persist the rename and advance the sync token (backed by a change-log
	// anchor row) atomically in one transaction. Doing the Save and RecordChange
	// separately let a concurrent object PUT's fresh token be overwritten by the
	// stale one loaded above, and a RecordChange failure after the Save committed
	// left the CTag un-bumped so clients never saw the new displayname.
	//
	// A request that names a kind and no other field writes no metadata: the
	// calendar's own kind is then a true no-op (no ctag bump), and a widening
	// bumps the ctag once. Without a kind this runs as it always has, an empty
	// body included.
	metadata := req.Name != nil || req.Description != nil || req.Color != nil || req.Timezone != nil
	if metadata || req.Kind == "" {
		if err := uc.repo.UpdateMetadata(ctx, cal); err != nil {
			return nil, fmt.Errorf("failed to update calendar: %w", err)
		}
	}

	// UpdateMetadata cannot write the component set; this has its own
	// transaction, which also advances the ctag and sync token. A widening
	// that comes with a rename is two transactions: should the second fail,
	// the request is a 500 and repeating it is safe.
	if widen {
		if err := uc.repo.UpdateSupportedComponents(ctx, cal.ID, widenedSet); err != nil {
			return nil, fmt.Errorf("failed to update calendar component set: %w", err)
		}
		// The response reports the new kind.
		cal.SupportedComponents = widenedSet
	}

	return cal, nil
}

// widenComponents resolves a PATCH kind against what cal stores (#170). An
// empty kind changes nothing. Otherwise the kind must be one of the closed enum
// (calendar.SupportedComponentsForKind, ErrInvalidInput otherwise), and it may
// only add components: if cal stores a component the kind does not (both to
// events or tasks, events to tasks and back) the change is refused with
// ErrConflict, whether or not the calendar holds objects of that component —
// that would orphan them, and narrowing is left to the one-shot migration.
// widen is true when the kind adds a component; set is then the value to store.
// The calendar's current kind is no change at all, so a legacy "" row (both
// components) is never rewritten by a PATCH to "both".
func widenComponents(cal *calendar.Calendar, kind string) (set string, widen bool, err error) {
	if kind == "" {
		return "", false, nil
	}
	set, err = calendar.SupportedComponentsForKind(kind)
	if err != nil {
		return "", false, err
	}
	requested := &calendar.Calendar{SupportedComponents: set}
	for _, comp := range []string{calendar.ComponentEvent, calendar.ComponentToDo} {
		stored, wanted := calendar.SupportsComponent(cal, comp), calendar.SupportsComponent(requested, comp)
		if stored && !wanted {
			return "", false, errNarrowComponentSet()
		}
		if wanted && !stored {
			widen = true
		}
	}
	if !widen {
		return "", false, nil
	}
	return set, true, nil
}
