package mcp

import (
	"encoding/json"
	"sort"
	"time"

	eventuc "github.com/jherrma/caldav-server/internal/usecase/event"
)

// maxFreeSlotWindow bounds how far find_free_slots will look.
//
// The tool expands every recurring series in the window across every calendar,
// so the cost is linear in the window and a model asked for "some time next
// year" would otherwise expand a year of dailies to answer a question about an
// hour.
const maxFreeSlotWindow = 90 * 24 * time.Hour

// maxFreeSlots caps the returned openings. A model needs a handful of
// candidates to propose, not every gap in three months.
const maxFreeSlots = 50

func (s *Server) registerSchedulingTools() {
	s.register(Tool{
		Name: "find_free_slots",
		Description: "Find openings of at least duration_minutes between start and end, across the " +
			"user's calendars. By default all-day events do NOT block — birthdays and holidays " +
			"would otherwise wipe out whole days — pass include_all_day true to treat them as " +
			"busy. Read-only shared calendars count as busy just like owned ones. The window may " +
			"span at most 90 days.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "start": {"type": "string", "description": "Window start, RFC 3339"},
    "end": {"type": "string", "description": "Window end, RFC 3339"},
    "duration_minutes": {"type": "integer", "description": "Minimum length of an opening, in minutes"},
    "calendar_ids": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Calendar UUIDs to consider busy. Defaults to every calendar the user can read"
    },
    "include_all_day": {"type": "boolean", "description": "Treat all-day events as busy. Defaults to false"}
  },
  "required": ["start", "end", "duration_minutes"],
  "additionalProperties": false
}`),
	}, s.toolFindFreeSlots)
}

// interval is a half-open busy period [Start, End).
type interval struct {
	Start time.Time
	End   time.Time
}

type freeSlot struct {
	Start   string `json:"start"`
	End     string `json:"end"`
	Minutes int    `json:"minutes"`
}

func (s *Server) toolFindFreeSlots(cc *callContext, args json.RawMessage) (*toolCallResult, *RPCError) {
	var in struct {
		Start           string   `json:"start"`
		End             string   `json:"end"`
		DurationMinutes int      `json:"duration_minutes"`
		CalendarIDs     []string `json:"calendar_ids"`
		IncludeAllDay   bool     `json:"include_all_day"`
	}
	if rpcErr := decodeArgs(args, &in); rpcErr != nil {
		return nil, rpcErr
	}

	start, err := parseTime("start", in.Start)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	end, err := parseTime("end", in.End)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	if !end.After(start) {
		return errorResult("end must be after start."), nil
	}
	if end.Sub(start) > maxFreeSlotWindow {
		return errorResult("The window may span at most 90 days; narrow start/end."), nil
	}
	if in.DurationMinutes <= 0 {
		return errorResult("duration_minutes must be a positive number of minutes."), nil
	}
	duration := time.Duration(in.DurationMinutes) * time.Minute
	if duration > end.Sub(start) {
		return errorResult("duration_minutes is longer than the whole window."), nil
	}

	// Resolve which calendars count as busy. Going through the list use case
	// means "every calendar" means exactly what the UI shows — owned plus
	// shared — rather than a second, divergent notion of the user's calendars.
	cals, err := s.deps.CalendarList.Execute(cc.ctx, cc.userID)
	if err != nil {
		return errorResult("Failed to list calendars: " + err.Error()), nil
	}

	wanted := map[string]bool{}
	for _, id := range in.CalendarIDs {
		wanted[id] = true
	}

	considered := make([]string, 0, len(cals))
	busy := make([]interval, 0)
	for _, c := range cals {
		if len(wanted) > 0 && !wanted[c.UUID] {
			continue
		}
		considered = append(considered, c.Name)

		instances, err := s.deps.EventList.Execute(cc.ctx, eventuc.ListEventsInput{
			CalendarID: c.ID,
			Start:      start,
			End:        end,
			Expand:     true,
		})
		if err != nil {
			return errorResult("Failed to read events from calendar " + c.Name + ": " + err.Error()), nil
		}
		for _, inst := range instances {
			if inst.IsAllDay && !in.IncludeAllDay {
				continue
			}
			busy = append(busy, interval{Start: inst.Start, End: inst.End})
		}
	}

	// A calendar_ids list naming nothing the user can read is a mistake worth
	// reporting: answering "the whole window is free" would be a confidently
	// wrong answer built on having looked at nothing.
	if len(wanted) > 0 && len(considered) == 0 {
		return errorResult("None of the given calendar_ids are readable by you."), nil
	}

	slots := freeSlots(busy, start, end, duration)
	truncated := false
	if len(slots) > maxFreeSlots {
		slots = slots[:maxFreeSlots]
		truncated = true
	}

	out := map[string]interface{}{
		"free_slots":          slots,
		"count":               len(slots),
		"duration_minutes":    in.DurationMinutes,
		"window_start":        start.Format(time.RFC3339),
		"window_end":          end.Format(time.RFC3339),
		"calendars_consulted": considered,
		"all_day_counts_busy": in.IncludeAllDay,
	}
	if truncated {
		out["truncated"] = true
	}
	return jsonText(out)
}

// freeSlots returns the gaps of at least duration between start and end that no
// busy interval covers.
//
// Busy intervals are merged first, so overlapping and back-to-back meetings
// collapse into one block; without that, two overlapping events would leave a
// phantom gap between the first one's end and the second one's start.
func freeSlots(busy []interval, start, end time.Time, duration time.Duration) []freeSlot {
	clipped := make([]interval, 0, len(busy))
	for _, b := range busy {
		if !b.End.After(start) || !b.Start.Before(end) {
			continue // wholly outside the window
		}
		if b.Start.Before(start) {
			b.Start = start
		}
		if b.End.After(end) {
			b.End = end
		}
		if b.End.After(b.Start) {
			clipped = append(clipped, b)
		}
	}
	sort.Slice(clipped, func(i, j int) bool { return clipped[i].Start.Before(clipped[j].Start) })

	slots := []freeSlot{}
	cursor := start
	for _, b := range clipped {
		if b.Start.After(cursor) {
			if gap := b.Start.Sub(cursor); gap >= duration {
				slots = append(slots, freeSlot{
					Start:   cursor.Format(time.RFC3339),
					End:     b.Start.Format(time.RFC3339),
					Minutes: int(gap / time.Minute),
				})
			}
		}
		if b.End.After(cursor) {
			cursor = b.End
		}
	}
	if end.After(cursor) {
		if gap := end.Sub(cursor); gap >= duration {
			slots = append(slots, freeSlot{
				Start:   cursor.Format(time.RFC3339),
				End:     end.Format(time.RFC3339),
				Minutes: int(gap / time.Minute),
			})
		}
	}
	return slots
}
