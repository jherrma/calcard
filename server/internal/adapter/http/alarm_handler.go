package http

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/adapter/http/dto"
	alarmuc "github.com/jherrma/caldav-server/internal/usecase/alarm"
)

// AlarmHandler serves the web app's reminders (#169).
type AlarmHandler struct {
	upcomingUC *alarmuc.UpcomingUseCase
}

// NewAlarmHandler creates the handler.
func NewAlarmHandler(upcomingUC *alarmuc.UpcomingUseCase) *AlarmHandler {
	return &AlarmHandler{upcomingUC: upcomingUC}
}

// GET /api/v1/alarms/upcoming
//
// The alarms of every calendar the caller can read, owned and shared, never a
// subscribed one (resolved on every request), whose trigger lies in [now - 24 h,
// until]. Query parameter until (optional): RFC 3339 WITH an offset (send "+" as
// %2B, as in any query string); default now + 24 h, clamped to now + 7 d, and
// the answer's "until" shows the value used. An until before "from" is an empty
// list, not an error. Anything that is not RFC 3339 with an offset (also an
// unencoded "+", which arrives as a space) is 400 "Invalid 'until' value".
//
// Answers 200, raw JSON (no status/data envelope), Cache-Control: no-store:
// {"alarms":[{key, trigger_at, uid, recurrence_id, alarm_index, action,
// description, object_type, object_id, calendar_id, calendar_uuid,
// calendar_name, calendar_color, summary, location, all_day, start, start_date,
// end, due, due_date}],"count","from","until","server_time","truncated"}, sorted
// by trigger_at then key, at most 500 (truncated: true when more existed).
// Floating and all-day triggers are resolved in the calendar owner's time zone.
func (h *AlarmHandler) Upcoming(c fiber.Ctx) error {
	userID, err := GetUserIDFromContext(c)
	if err != nil {
		// Only reachable if the route lost its Authenticate middleware.
		return UnauthorizedResponse(c, "missing authentication token")
	}
	out, err := h.upcomingUC.Execute(c.Context(), userID, c.Query("until"))
	if err != nil {
		if errors.Is(err, alarmuc.ErrInvalidUntil) {
			return BadRequestResponse(c, "Invalid 'until' value")
		}
		slog.Error("alarm request failed",
			slog.String("method", c.Method()),
			slog.String("path", c.Path()),
			slog.Any("error", err),
		)
		return ErrorResponse(c, fiber.StatusInternalServerError, "Internal server error")
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(dto.NewUpcomingAlarmsResponse(out))
}
