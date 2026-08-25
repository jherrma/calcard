package http

import (
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/usecase/subscription"
)

// SubscriptionHandler exposes remote calendar subscriptions (story 100).
//
// Like the address-book endpoints these return raw JSON rather than the
// {status, data} envelope, matching the calendar/event surface the frontend
// already consumes for this area.
type SubscriptionHandler struct {
	createUC  *subscription.CreateUseCase
	listUC    *subscription.ListUseCase
	getUC     *subscription.GetUseCase
	updateUC  *subscription.UpdateUseCase
	deleteUC  *subscription.DeleteUseCase
	refreshUC *subscription.RefreshUseCase
}

func NewSubscriptionHandler(
	createUC *subscription.CreateUseCase,
	listUC *subscription.ListUseCase,
	getUC *subscription.GetUseCase,
	updateUC *subscription.UpdateUseCase,
	deleteUC *subscription.DeleteUseCase,
	refreshUC *subscription.RefreshUseCase,
) *SubscriptionHandler {
	return &SubscriptionHandler{
		createUC:  createUC,
		listUC:    listUC,
		getUC:     getUC,
		updateUC:  updateUC,
		deleteUC:  deleteUC,
		refreshUC: refreshUC,
	}
}

// subscriptionResponse is the wire shape. It flattens the subscription and its
// calendar into one object because they are one thing to the user: "a calendar
// I subscribed to".
type subscriptionResponse struct {
	ID              string     `json:"id"`
	CalendarID      string     `json:"calendar_id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Color           string     `json:"color"`
	URL             string     `json:"url"`
	RefreshInterval string     `json:"refresh_interval"`
	Status          string     `json:"status"`
	Enabled         bool       `json:"enabled"`
	LastSyncedAt    *time.Time `json:"last_synced_at"`
	NextSyncAt      *time.Time `json:"next_sync_at"`
	LastError       string     `json:"last_error"`
	ErrorCount      int        `json:"error_count"`
	EventCount      int64      `json:"event_count"`
	CreatedAt       time.Time  `json:"created_at"`
}

func toSubscriptionResponse(v *subscription.View) subscriptionResponse {
	sub := v.Subscription
	resp := subscriptionResponse{
		ID:              sub.UUID,
		URL:             sub.URL,
		RefreshInterval: formatInterval(sub.RefreshInterval),
		Status:          sub.Status(),
		Enabled:         sub.Enabled,
		LastSyncedAt:    sub.LastSyncedAt,
		LastError:       sub.LastError,
		ErrorCount:      sub.ErrorCount,
		EventCount:      v.EventCount,
		CreatedAt:       sub.CreatedAt,
	}
	// next_sync_at is meaningless once auto-sync is off — reporting a stale
	// future timestamp would tell the user a refresh is coming that never will.
	if sub.Enabled {
		next := sub.NextSyncAt
		resp.NextSyncAt = &next
	}
	if v.Calendar != nil {
		resp.CalendarID = v.Calendar.UUID
		resp.Name = v.Calendar.Name
		resp.Description = v.Calendar.Description
		resp.Color = v.Calendar.Color
	}
	return resp
}

// formatInterval renders a duration the way the API accepts it ("1h", "15m").
//
// time.Duration.String() would render an hour as "1h0m0s". That parses back
// fine, but it is what the client echoes into a dropdown and what a user reads
// in the settings page, so the trailing zero units are trimmed.
func formatInterval(d time.Duration) string {
	if d <= 0 {
		d = calendar.DefaultRefreshInterval
	}
	// Built from the value rather than trimmed off d.String(): trimming "0s"
	// and then "0m" turns "30m0s" into "3".
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return d.String()
	}
}

// parseInterval accepts a Go duration string. An empty value means "leave it
// alone" (PATCH) or "use the default" (POST), which the use case decides.
func parseInterval(raw string) (time.Duration, error) {
	return time.ParseDuration(raw)
}

type createSubscriptionRequest struct {
	URL             string `json:"url"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Color           string `json:"color"`
	RefreshInterval string `json:"refresh_interval"`
}

// POST /api/v1/calendar-subscriptions
//
// The feed is fetched synchronously before anything is stored, so a bad URL is
// reported here rather than surfacing as a silently broken calendar later. That
// makes this endpoint as slow as the third-party feed it is validating.
func (h *SubscriptionHandler) Create(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	var req createSubscriptionRequest
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	input := subscription.CreateInput{
		URL:         req.URL,
		Name:        req.Name,
		Description: req.Description,
		Color:       req.Color,
	}
	if req.RefreshInterval != "" {
		d, err := parseInterval(req.RefreshInterval)
		if err != nil {
			return BadRequestResponse(c, "Invalid refresh_interval, expected a duration such as 1h")
		}
		input.RefreshInterval = d
	}

	view, err := h.createUC.Execute(c.Context(), u.ID, input)
	if err != nil {
		return subscriptionError(c, err)
	}

	return c.Status(fiber.StatusCreated).JSON(toSubscriptionResponse(view))
}

// GET /api/v1/calendar-subscriptions
func (h *SubscriptionHandler) List(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	views, err := h.listUC.Execute(c.Context(), u.ID)
	if err != nil {
		return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list calendar subscriptions")
	}

	// An empty list must render as [] rather than null so the client can
	// iterate it unconditionally.
	out := make([]subscriptionResponse, 0, len(views))
	for _, v := range views {
		out = append(out, toSubscriptionResponse(v))
	}
	return c.JSON(fiber.Map{"subscriptions": out})
}

// GET /api/v1/calendar-subscriptions/:id
func (h *SubscriptionHandler) Get(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	view, err := h.getUC.Execute(c.Context(), u.ID, c.Params("id"))
	if err != nil {
		return subscriptionError(c, err)
	}
	return c.JSON(toSubscriptionResponse(view))
}

type updateSubscriptionRequest struct {
	URL             *string `json:"url"`
	Name            *string `json:"name"`
	Description     *string `json:"description"`
	Color           *string `json:"color"`
	RefreshInterval *string `json:"refresh_interval"`
	Enabled         *bool   `json:"enabled"`
}

// PATCH /api/v1/calendar-subscriptions/:id
//
// Changing the URL resyncs immediately; a resync failure is reported on the
// subscription's status rather than as a failed update, because the new URL
// has been saved either way.
func (h *SubscriptionHandler) Update(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	var req updateSubscriptionRequest
	if err := c.Bind().JSON(&req); err != nil {
		return BadRequestResponse(c, "Invalid request body")
	}

	input := subscription.UpdateInput{
		URL:         req.URL,
		Name:        req.Name,
		Description: req.Description,
		Color:       req.Color,
		Enabled:     req.Enabled,
	}
	if req.RefreshInterval != nil {
		d, err := parseInterval(*req.RefreshInterval)
		if err != nil {
			return BadRequestResponse(c, "Invalid refresh_interval, expected a duration such as 1h")
		}
		input.RefreshInterval = &d
	}

	view, err := h.updateUC.Execute(c.Context(), u.ID, c.Params("id"), input)
	if err != nil {
		return subscriptionError(c, err)
	}
	return c.JSON(toSubscriptionResponse(view))
}

// DELETE /api/v1/calendar-subscriptions/:id
//
// Deletes the subscription and the calendar mirroring it.
func (h *SubscriptionHandler) Delete(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	if err := h.deleteUC.Execute(c.Context(), u.ID, c.Params("id")); err != nil {
		return subscriptionError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// refreshResponse reports what the forced sync did alongside the new state.
type refreshResponse struct {
	subscriptionResponse
	Synced  bool `json:"synced"`
	Created int  `json:"created"`
	Updated int  `json:"updated"`
	Deleted int  `json:"deleted"`
	Skipped int  `json:"skipped"`
}

// POST /api/v1/calendar-subscriptions/:id/refresh
//
// A failed refresh answers 200 with status:"error" and the reason, not 5xx: the
// request succeeded, the third-party feed is what failed, and the client needs
// the updated subscription state either way to render it.
func (h *SubscriptionHandler) Refresh(c fiber.Ctx) error {
	u := c.Locals("user").(*user.User)

	view, outcome, syncErr := h.refreshUC.Execute(c.Context(), u.ID, c.Params("id"))
	if view == nil {
		return subscriptionError(c, syncErr)
	}

	resp := refreshResponse{subscriptionResponse: toSubscriptionResponse(view)}
	if syncErr == nil && outcome != nil {
		resp.Synced = true
		resp.Created = outcome.Stats.Created
		resp.Updated = outcome.Stats.Updated
		resp.Deleted = outcome.Stats.Deleted
		resp.Skipped = outcome.Skipped
	}
	return c.JSON(resp)
}

// subscriptionError maps use-case errors to responses.
//
// A feed-specific failure (unreachable host, HTTP 404, not iCalendar) is a 400:
// the request named a URL that does not work, which is the caller's input, and
// the message is already sanitized for display — it never contains the URL,
// which may carry a secret token.
func subscriptionError(c fiber.Ctx, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, subscription.ErrNotFound):
		return ErrorResponse(c, fiber.StatusNotFound, "Subscription not found")
	case errors.Is(err, subscription.ErrLimitReached):
		return ErrorResponse(c, fiber.StatusConflict, err.Error())
	case errors.Is(err, subscription.ErrInvalidInput),
		errors.Is(err, subscription.ErrInvalidURL),
		errors.Is(err, subscription.ErrNotCalendar),
		errors.Is(err, subscription.ErrTooLarge),
		errors.Is(err, subscription.ErrEmptyFeed):
		return BadRequestResponse(c, err.Error())
	}

	// A failure the remote feed caused: its message was written to be shown to
	// the user and carries no URL.
	var feedErr *subscription.FeedError
	if errors.As(err, &feedErr) {
		return BadRequestResponse(c, feedErr.Message)
	}

	// Anything left is ours. Say so without describing it — the details are
	// wrapped storage errors that mean nothing to the caller.
	return ErrorResponse(c, fiber.StatusInternalServerError, "Failed to process calendar subscription")
}
