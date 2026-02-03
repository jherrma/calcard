package http

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

type PublicCalendarHandler struct {
	calendarRepo calendar.CalendarRepository
}

func NewPublicCalendarHandler(calendarRepo calendar.CalendarRepository) *PublicCalendarHandler {
	return &PublicCalendarHandler{
		calendarRepo: calendarRepo,
	}
}

// GET /public/calendar/:token.ics
func (h *PublicCalendarHandler) GetICalFeed(c fiber.Ctx) error {
	token := c.Params("token")
	// Remove .ics extension if present
	token = strings.TrimSuffix(token, ".ics")

	// Find calendar by public token
	cal, err := h.calendarRepo.FindByPublicToken(c.Context(), token)
	if err != nil || cal == nil || !cal.PublicEnabled {
		return c.Status(fiber.StatusNotFound).SendString("Calendar not found")
	}

	// Check ETag for caching
	clientETag := c.Get("If-None-Match")
	currentETag := fmt.Sprintf(`"%s"`, cal.CTag)
	if clientETag == currentETag {
		return c.SendStatus(fiber.StatusNotModified)
	}

	// Get all events
	events, err := h.calendarRepo.GetCalendarObjects(c.Context(), cal.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Internal error")
	}

	// Generate iCal feed
	ical := h.generateICalFeed(cal, events)

	// Set headers
	c.Set("Content-Type", "text/calendar; charset=utf-8")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.ics"`, cal.Name))
	c.Set("Cache-Control", "public, max-age=300")
	c.Set("ETag", currentETag)

	return c.SendString(ical)
}

func (h *PublicCalendarHandler) generateICalFeed(cal *calendar.Calendar, events []*calendar.CalendarObject) string {
	var b strings.Builder

	// VCALENDAR header
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//CalDAV Server//EN\r\n")
	b.WriteString("CALSCALE:GREGORIAN\r\n")
	b.WriteString("METHOD:PUBLISH\r\n")
	b.WriteString(fmt.Sprintf("X-WR-CALNAME:%s\r\n", escapeICalText(cal.Name)))
	if cal.Timezone != "" {
		b.WriteString(fmt.Sprintf("X-WR-TIMEZONE:%s\r\n", cal.Timezone))
	}

	// Add events - extract VEVENT/VTODO from stored iCalendar data
	for _, event := range events {
		vevent := extractVComponent(event.ICalData)
		if vevent != "" {
			b.WriteString(vevent)
			b.WriteString("\r\n")
		}
	}

	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

func escapeICalText(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func extractVComponent(icalData string) string {
	// Extract VEVENT, VTODO, or VTIMEZONE components from iCalendar data
	var result strings.Builder
	lines := strings.Split(icalData, "\n")
	inComponent := false
	depth := 0

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "BEGIN:VEVENT") ||
			strings.HasPrefix(line, "BEGIN:VTODO") ||
			strings.HasPrefix(line, "BEGIN:VTIMEZONE") {
			inComponent = true
			depth++
		}
		if inComponent {
			result.WriteString(line)
			result.WriteString("\r\n")
		}
		if strings.HasPrefix(line, "END:VEVENT") ||
			strings.HasPrefix(line, "END:VTODO") ||
			strings.HasPrefix(line, "END:VTIMEZONE") {
			depth--
			if depth == 0 {
				inComponent = false
			}
		}
	}
	return strings.TrimRight(result.String(), "\r\n")
}
