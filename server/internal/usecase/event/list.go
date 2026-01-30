package event

import (
	"context"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/calendar"
)

type ListEventsInput struct {
	CalendarID uint
	Start      time.Time
	End        time.Time
	Expand     bool
}

type ListEventsUseCase struct {
	calendarRepo calendar.CalendarRepository
}

func NewListEventsUseCase(calendarRepo calendar.CalendarRepository) *ListEventsUseCase {
	return &ListEventsUseCase{calendarRepo: calendarRepo}
}

func (uc *ListEventsUseCase) Execute(ctx context.Context, input ListEventsInput) ([]calendar.EventInstance, error) {
	objects, err := uc.calendarRepo.ListEvents(ctx, input.CalendarID, input.Start, input.End)
	if err != nil {
		return nil, err
	}

	var result []calendar.EventInstance
	for _, obj := range objects {
		if input.Expand {
			instances, err := calendar.ExpandRecurringEvent(obj, input.Start, input.End)
			if err != nil {
				// Log error and continue or return?
				// For now, let's return error but maybe we should skip problematic events
				return nil, err
			}
			result = append(result, instances...)
		} else {
			if obj.StartTime != nil && obj.EndTime != nil {
				result = append(result, calendar.ToEventInstance(obj, *obj.StartTime, *obj.EndTime, "", nil, nil))
			}
		}
	}

	return result, nil
}
