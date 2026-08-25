package subscription

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

// CreateInput describes a subscription to create. Everything but URL is
// optional: an omitted name/colour/timezone is taken from the feed itself.
type CreateInput struct {
	URL             string
	Name            string
	Description     string
	Color           string
	RefreshInterval time.Duration
}

// CreateUseCase creates a subscription and its backing read-only calendar.
type CreateUseCase struct {
	subRepo           calendar.CalendarSubscriptionRepository
	calRepo           calendar.CalendarRepository
	fetcher           *Fetcher
	maxPerUser        int
	allowInsecureURLs bool
	now               func() time.Time
}

func NewCreateUseCase(
	subRepo calendar.CalendarSubscriptionRepository,
	calRepo calendar.CalendarRepository,
	fetcher *Fetcher,
	maxPerUser int,
	allowInsecureURLs bool,
	now func() time.Time,
) *CreateUseCase {
	if now == nil {
		now = time.Now
	}
	return &CreateUseCase{
		subRepo:           subRepo,
		calRepo:           calRepo,
		fetcher:           fetcher,
		maxPerUser:        maxPerUser,
		allowInsecureURLs: allowInsecureURLs,
		now:               now,
	}
}

// Execute validates the feed, then creates the calendar and the subscription.
//
// The feed is fetched BEFORE anything is written (the story's "fetch and
// validate on creation"): a subscription that only reveals its URL is a 404 an
// hour later, in a background worker whose failure the user has to go looking
// for, is a worse experience than a create that fails immediately with the
// reason. It also means the calendar shows up already populated.
//
// If any write after the calendar's creation fails, the calendar is removed
// again. A subscribed calendar without its subscription row is unreachable
// wreckage: it is read-only to every write path, and nothing will ever refresh
// it.
func (uc *CreateUseCase) Execute(ctx context.Context, userID uint, input CreateInput) (*View, error) {
	feedURL, err := NormalizeURL(input.URL, uc.allowInsecureURLs)
	if err != nil {
		return nil, err
	}

	interval := input.RefreshInterval
	if interval == 0 {
		interval = calendar.DefaultRefreshInterval
	}
	if err := calendar.ValidateRefreshInterval(interval); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	if input.Name != "" {
		if err := calendar.ValidateName(input.Name); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
	}
	if input.Description != "" {
		if err := calendar.ValidateDescription(input.Description); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
	}
	if input.Color != "" {
		if err := calendar.ValidateHexColor(input.Color); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
	}

	if uc.maxPerUser > 0 {
		count, err := uc.subRepo.CountByUserID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to count subscriptions: %w", err)
		}
		if count >= int64(uc.maxPerUser) {
			return nil, fmt.Errorf("%w: at most %d subscriptions per account", ErrLimitReached, uc.maxPerUser)
		}
	}

	res, err := uc.fetcher.Fetch(ctx, feedURL, "", "")
	if err != nil {
		return nil, err
	}
	feed, err := ParseFeed(res.Body)
	if err != nil {
		return nil, err
	}
	if len(feed.Objects) == 0 {
		return nil, ErrEmptyFeed
	}

	cal := &calendar.Calendar{
		UUID:                uuid.New().String(),
		UserID:              userID,
		Name:                firstNonEmpty(input.Name, feed.Name, "Subscribed calendar"),
		Description:         firstNonEmpty(input.Description, feed.Description),
		Color:               firstNonEmpty(input.Color, calendar.GenerateRandomColor()),
		Timezone:            feedTimezone(feed.Timezone),
		SupportedComponents: "VEVENT,VTODO",
		Subscribed:          true,
	}
	cal.Path = cal.UUID + ".ics"
	// A feed's X-WR-CALNAME can be longer than a calendar name may be, so it
	// goes through the same validation as a user-supplied one and is trimmed
	// rather than rejected — the user did not choose it and cannot fix it.
	cal.Name = clampName(cal.Name)
	cal.Description = clampDescription(cal.Description)

	if err := uc.calRepo.Create(ctx, cal); err != nil {
		return nil, fmt.Errorf("failed to create calendar: %w", err)
	}

	now := uc.now()
	sub := &calendar.CalendarSubscription{
		UUID:            uuid.New().String(),
		CalendarID:      cal.ID,
		UserID:          userID,
		URL:             feedURL,
		RefreshInterval: interval,
		NextSyncAt:      now.Add(interval),
		LastSyncedAt:    &now,
		Enabled:         true,
		ETag:            res.ETag,
		LastModified:    res.LastModified,
	}
	if err := uc.subRepo.Create(ctx, sub); err != nil {
		uc.rollback(ctx, cal.ID)
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	if _, err := uc.calRepo.ReplaceFeedObjects(ctx, cal.ID, feed.Objects); err != nil {
		_ = uc.subRepo.Delete(ctx, sub.ID)
		uc.rollback(ctx, cal.ID)
		return nil, fmt.Errorf("failed to store feed events: %w", err)
	}

	sub.Calendar = *cal
	// The events were just written, so the count is what was stored — no need
	// to ask the database what we put there a line ago.
	return &View{Subscription: sub, Calendar: cal, EventCount: int64(len(feed.Objects))}, nil
}

// rollback removes a calendar created moments ago for a subscription that then
// failed to materialize.
func (uc *CreateUseCase) rollback(ctx context.Context, calendarID uint) {
	_ = uc.calRepo.Delete(ctx, calendarID)
}

// feedTimezone accepts the feed's X-WR-TIMEZONE only when it names a zone this
// server can load, so a publisher's typo cannot produce a calendar whose
// timezone breaks every client that reads it.
func feedTimezone(tz string) string {
	if tz == "" {
		return "UTC"
	}
	if err := calendar.ValidateTimezone(tz); err != nil {
		return "UTC"
	}
	return tz
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

const (
	maxCalendarName        = 255
	maxCalendarDescription = 1000
)

func clampName(name string) string {
	return clampBytes(name, maxCalendarName)
}

func clampDescription(desc string) string {
	return clampBytes(desc, maxCalendarDescription)
}

// clampBytes truncates to at most max BYTES — the unit calendar.ValidateName
// and the column definition both use — without splitting a rune, which would
// leave invalid UTF-8 in the database.
func clampBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
