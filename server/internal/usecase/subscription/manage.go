package subscription

import (
	"context"
	"fmt"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// View is a subscription plus the parts of its calendar the client needs, so
// the settings page does not have to cross-reference GET /calendars.
type View struct {
	Subscription *calendar.CalendarSubscription
	Calendar     *calendar.Calendar
	EventCount   int64
}

// ListUseCase lists a user's subscriptions.
type ListUseCase struct {
	subRepo calendar.CalendarSubscriptionRepository
	calRepo calendar.CalendarRepository
}

func NewListUseCase(subRepo calendar.CalendarSubscriptionRepository, calRepo calendar.CalendarRepository) *ListUseCase {
	return &ListUseCase{subRepo: subRepo, calRepo: calRepo}
}

func (uc *ListUseCase) Execute(ctx context.Context, userID uint) ([]*View, error) {
	subs, err := uc.subRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]*View, 0, len(subs))
	for _, sub := range subs {
		views = append(views, uc.view(ctx, sub))
	}
	return views, nil
}

// view assembles the response for one subscription. A missing event count is
// reported as zero rather than failing the whole list: the count is decoration,
// the feed's status is the point.
func (uc *ListUseCase) view(ctx context.Context, sub *calendar.CalendarSubscription) *View {
	count, err := uc.calRepo.GetEventCount(ctx, sub.CalendarID)
	if err != nil {
		count = 0
	}
	cal := sub.Calendar
	return &View{Subscription: sub, Calendar: &cal, EventCount: count}
}

// GetUseCase retrieves one subscription.
type GetUseCase struct {
	subRepo calendar.CalendarSubscriptionRepository
	calRepo calendar.CalendarRepository
}

func NewGetUseCase(subRepo calendar.CalendarSubscriptionRepository, calRepo calendar.CalendarRepository) *GetUseCase {
	return &GetUseCase{subRepo: subRepo, calRepo: calRepo}
}

func (uc *GetUseCase) Execute(ctx context.Context, userID uint, subUUID string) (*View, error) {
	sub, err := ownedSubscription(ctx, uc.subRepo, userID, subUUID)
	if err != nil {
		return nil, err
	}
	count, err := uc.calRepo.GetEventCount(ctx, sub.CalendarID)
	if err != nil {
		count = 0
	}
	cal := sub.Calendar
	return &View{Subscription: sub, Calendar: &cal, EventCount: count}, nil
}

// UpdateInput carries the mutable settings. Every field is a pointer so
// "absent" and "set to empty" stay distinguishable in a PATCH.
type UpdateInput struct {
	Name            *string
	Description     *string
	Color           *string
	URL             *string
	RefreshInterval *time.Duration
	Enabled         *bool
}

// UpdateUseCase changes a subscription's settings, resyncing when the feed URL
// changes.
type UpdateUseCase struct {
	subRepo           calendar.CalendarSubscriptionRepository
	calRepo           calendar.CalendarRepository
	syncUC            *SyncUseCase
	allowInsecureURLs bool
	now               func() time.Time
}

func NewUpdateUseCase(
	subRepo calendar.CalendarSubscriptionRepository,
	calRepo calendar.CalendarRepository,
	syncUC *SyncUseCase,
	allowInsecureURLs bool,
	now func() time.Time,
) *UpdateUseCase {
	if now == nil {
		now = time.Now
	}
	return &UpdateUseCase{subRepo: subRepo, calRepo: calRepo, syncUC: syncUC, allowInsecureURLs: allowInsecureURLs, now: now}
}

// Execute applies the update. A changed URL triggers an immediate resync (the
// story's "changing URL triggers immediate resync"), because the calendar's
// contents would otherwise keep showing the old feed until the next tick —
// with the settings page insisting they came from the new one.
func (uc *UpdateUseCase) Execute(ctx context.Context, userID uint, subUUID string, input UpdateInput) (*View, error) {
	sub, err := ownedSubscription(ctx, uc.subRepo, userID, subUUID)
	if err != nil {
		return nil, err
	}

	cal, err := uc.calRepo.GetByID(ctx, sub.CalendarID)
	if err != nil || cal == nil {
		return nil, ErrNotFound
	}

	calendarChanged := false
	if input.Name != nil {
		if err := calendar.ValidateName(*input.Name); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		cal.Name = *input.Name
		calendarChanged = true
	}
	if input.Description != nil {
		if err := calendar.ValidateDescription(*input.Description); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		cal.Description = *input.Description
		calendarChanged = true
	}
	if input.Color != nil {
		if err := calendar.ValidateHexColor(*input.Color); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		cal.Color = *input.Color
		calendarChanged = true
	}

	urlChanged := false
	if input.URL != nil {
		normalized, err := NormalizeURL(*input.URL, uc.allowInsecureURLs)
		if err != nil {
			return nil, err
		}
		if normalized != sub.URL {
			sub.URL = normalized
			// The validators belong to the old feed; presenting them to a
			// different origin can produce a 304 for content we have never
			// seen, leaving the calendar showing the previous feed forever.
			sub.ETag = ""
			sub.LastModified = ""
			urlChanged = true
		}
	}
	if input.RefreshInterval != nil {
		if err := calendar.ValidateRefreshInterval(*input.RefreshInterval); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		sub.RefreshInterval = *input.RefreshInterval
		// Re-anchor the schedule to the new interval instead of leaving a
		// next-attempt time computed from the old one, which for a 24h ->
		// 15m change would mean the user waits a day for the faster refresh.
		sub.NextSyncAt = uc.now().Add(*input.RefreshInterval)
	}
	if input.Enabled != nil {
		sub.Enabled = *input.Enabled
		if *input.Enabled {
			// Re-enabling after an auto-disable must clear the error counter,
			// or the very next failure re-trips the limit immediately.
			sub.ErrorCount = 0
			sub.LastError = ""
			sub.NextSyncAt = uc.now()
		}
	}

	if calendarChanged {
		if err := uc.calRepo.UpdateMetadata(ctx, cal); err != nil {
			return nil, fmt.Errorf("failed to update calendar: %w", err)
		}
	}
	if err := uc.subRepo.Update(ctx, sub); err != nil {
		return nil, fmt.Errorf("failed to update subscription: %w", err)
	}

	if urlChanged {
		// A resync failure is not an update failure: the new URL is stored and
		// its error is recorded on the subscription, which is exactly what the
		// client renders. Reporting 500 here would suggest nothing was saved.
		_, _ = uc.syncUC.Sync(ctx, sub)
	}

	count, err := uc.calRepo.GetEventCount(ctx, sub.CalendarID)
	if err != nil {
		count = 0
	}
	return &View{Subscription: sub, Calendar: cal, EventCount: count}, nil
}

// DeleteUseCase removes a subscription together with its calendar.
type DeleteUseCase struct {
	subRepo calendar.CalendarSubscriptionRepository
	calRepo calendar.CalendarRepository
}

func NewDeleteUseCase(subRepo calendar.CalendarSubscriptionRepository, calRepo calendar.CalendarRepository) *DeleteUseCase {
	return &DeleteUseCase{subRepo: subRepo, calRepo: calRepo}
}

// Execute deletes the subscription and the calendar mirroring it.
//
// Unlike deleting an ordinary calendar there is no "cannot delete your last
// calendar" guard and no DELETE confirmation string: a subscription holds no
// data of the user's own — everything in it came from the feed and comes back
// by re-subscribing — so the protections that exist for authored content would
// only be friction here.
//
// The subscription row goes first: if the process dies between the two
// deletes, what remains is an ordinary orphaned calendar the user can delete
// themselves, rather than a subscription pointing at a calendar that no longer
// exists, which every refresh would then fail on.
func (uc *DeleteUseCase) Execute(ctx context.Context, userID uint, subUUID string) error {
	sub, err := ownedSubscription(ctx, uc.subRepo, userID, subUUID)
	if err != nil {
		return err
	}
	if err := uc.subRepo.Delete(ctx, sub.ID); err != nil {
		return fmt.Errorf("failed to delete subscription: %w", err)
	}
	if err := uc.calRepo.Delete(ctx, sub.CalendarID); err != nil {
		return fmt.Errorf("failed to delete subscribed calendar: %w", err)
	}
	return nil
}

// RefreshUseCase forces an immediate sync.
type RefreshUseCase struct {
	subRepo calendar.CalendarSubscriptionRepository
	calRepo calendar.CalendarRepository
	syncUC  *SyncUseCase
}

func NewRefreshUseCase(subRepo calendar.CalendarSubscriptionRepository, calRepo calendar.CalendarRepository, syncUC *SyncUseCase) *RefreshUseCase {
	return &RefreshUseCase{subRepo: subRepo, calRepo: calRepo, syncUC: syncUC}
}

// Execute refreshes now, ignoring the schedule and the stored validators.
//
// A manual refresh re-enables a subscription auto-disabled by repeated
// failures: pressing "refresh" on a subscription showing "disabled after 5
// failures" can only mean "try again now", and requiring a separate re-enable
// step first would be a puzzle, not a safeguard.
//
// It also drops the ETag/Last-Modified for this one request, so a user who
// pressed refresh because the calendar looks wrong gets a real fetch rather
// than a 304 telling them what they already see is current.
func (uc *RefreshUseCase) Execute(ctx context.Context, userID uint, subUUID string) (*View, *SyncOutcome, error) {
	sub, err := ownedSubscription(ctx, uc.subRepo, userID, subUUID)
	if err != nil {
		return nil, nil, err
	}

	sub.Enabled = true
	sub.ErrorCount = 0
	sub.LastError = ""
	sub.ETag = ""
	sub.LastModified = ""

	outcome, syncErr := uc.syncUC.Sync(ctx, sub)

	count, err := uc.calRepo.GetEventCount(ctx, sub.CalendarID)
	if err != nil {
		count = 0
	}
	cal := sub.Calendar
	return &View{Subscription: sub, Calendar: &cal, EventCount: count}, outcome, syncErr
}

// ownedSubscription loads a subscription and verifies ownership, reporting
// somebody else's subscription as not found so ids cannot be probed.
func ownedSubscription(ctx context.Context, repo calendar.CalendarSubscriptionRepository, userID uint, subUUID string) (*calendar.CalendarSubscription, error) {
	sub, err := repo.GetByUUID(ctx, subUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up subscription: %w", err)
	}
	if sub == nil || sub.UserID != userID {
		return nil, ErrNotFound
	}
	return sub, nil
}
