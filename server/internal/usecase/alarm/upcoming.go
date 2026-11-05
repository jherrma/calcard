// Package alarm holds the use cases behind the web app's reminders (#169).
package alarm

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/usecase/ownerzone"
)

// ackLookupBatch is the keys per AcknowledgedKeys call (spec §7.1 step 6).
const ackLookupBatch = 500

// MaxAlarms is the most entries one answer carries (spec §7.1).
const MaxAlarms = 500

// Upcoming is one alarm of the answer together with the calendar it comes from.
type Upcoming struct {
	calendar.AlarmInstance
	Calendar calendar.ReadableCalendar
}

// UpcomingOutput is the answer of UpcomingUseCase.
type UpcomingOutput struct {
	Alarms     []Upcoming
	From       time.Time
	Until      time.Time
	ServerTime time.Time
	Truncated  bool
}

// UpcomingUseCase lists the alarms of every calendar a user can read (spec §7.1).
type UpcomingUseCase struct {
	calendars calendar.CalendarRepository
	acks      calendar.AlarmAcknowledgementRepository
	zones     *ownerzone.Resolver
	now       func() time.Time
}

// NewUpcomingUseCase builds the use case; a nil clock is time.Now.
func NewUpcomingUseCase(calendars calendar.CalendarRepository, acks calendar.AlarmAcknowledgementRepository, zones *ownerzone.Resolver, now func() time.Time) *UpcomingUseCase {
	if now == nil {
		now = time.Now
	}
	return &UpcomingUseCase{calendars: calendars, acks: acks, zones: zones, now: now}
}

// Execute answers the alarms with trigger in [now-24h, until]. until is RFC 3339
// with an offset, or empty for now+24h; it is clamped to now+7d. A bad value is
// ErrInvalidUntil; an until before from is an empty answer.
func (uc *UpcomingUseCase) Execute(ctx context.Context, userID uint, untilRaw string) (*UpcomingOutput, error) {
	now := uc.now().UTC().Truncate(time.Second)
	from := now.Add(-config.AlarmLookBack)
	until := now.Add(config.AlarmDefaultLookAhead)
	if untilRaw != "" {
		t, err := time.Parse(time.RFC3339, untilRaw)
		if err != nil {
			return nil, ErrInvalidUntil
		}
		until = t.UTC()
	}
	if max := now.Add(config.AlarmMaxLookAhead); until.After(max) {
		until = max
	}
	out := &UpcomingOutput{Alarms: []Upcoming{}, From: from, Until: until, ServerTime: now}
	if until.Before(from) {
		return out, nil
	}

	readable, err := uc.calendars.ListReadableCalendarIDs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list readable calendars: %w", err)
	}
	byID := make(map[uint]calendar.ReadableCalendar, len(readable))
	var ids, owners []uint
	seenOwner := map[uint]bool{}
	for _, cal := range readable {
		if cal.Subscribed {
			continue
		}
		byID[cal.ID] = cal
		ids = append(ids, cal.ID)
		if !seenOwner[cal.OwnerID] {
			seenOwner[cal.OwnerID] = true
			owners = append(owners, cal.OwnerID)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	objs, err := uc.calendars.ListAlarmCandidates(ctx, ids, from, until)
	if err != nil {
		return nil, fmt.Errorf("list alarm candidates: %w", err)
	}
	if len(objs) == 0 {
		return out, nil
	}
	zones, err := uc.zones.Many(ctx, owners)
	if err != nil {
		return nil, fmt.Errorf("load owner time zones: %w", err)
	}

	// The lowest calendar id wins a duplicate key (Q7): expand in that order.
	sort.SliceStable(objs, func(i, j int) bool {
		if objs[i].CalendarID != objs[j].CalendarID {
			return objs[i].CalendarID < objs[j].CalendarID
		}
		return objs[i].ID < objs[j].ID
	})
	seen := map[string]bool{}
	var all []Upcoming
	for _, obj := range objs {
		cal, ok := byID[obj.CalendarID]
		if !ok {
			continue // never widen the ids: a row outside the set is dropped
		}
		insts, capped, err := calendar.ExpandAlarms(obj, from, until, zones.For(cal.OwnerID))
		if err != nil {
			slog.Warn("alarm expansion skipped an undecodable object",
				slog.String("object_uuid", obj.UUID), slog.Any("error", err))
			continue
		}
		if capped {
			slog.Warn("alarm expansion reached the per-object instance cap",
				slog.String("object_uuid", obj.UUID), slog.Int("cap", calendar.AlarmInstanceCap))
			out.Truncated = true
		}
		for _, in := range insts {
			if seen[in.Key] {
				continue
			}
			seen[in.Key] = true
			all = append(all, Upcoming{AlarmInstance: in, Calendar: cal})
		}
	}

	// The key match is the join (D19): drop what the caller has acknowledged,
	// one primary-key lookup per 500 keys. Nothing else suppresses an alarm (D18).
	if len(all) > 0 {
		acked := map[string]bool{}
		for start := 0; start < len(all); start += ackLookupBatch {
			end := start + ackLookupBatch
			if end > len(all) {
				end = len(all)
			}
			keys := make([]string, 0, end-start)
			for _, a := range all[start:end] {
				keys = append(keys, a.Key)
			}
			got, err := uc.acks.AcknowledgedKeys(ctx, userID, keys)
			if err != nil {
				return nil, fmt.Errorf("load acknowledged alarm keys: %w", err)
			}
			for k := range got {
				acked[k] = true
			}
		}
		kept := all[:0]
		for _, a := range all {
			if !acked[a.Key] {
				kept = append(kept, a)
			}
		}
		all = kept
	}

	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].TriggerAt.Equal(all[j].TriggerAt) {
			return all[i].TriggerAt.Before(all[j].TriggerAt)
		}
		return all[i].Key < all[j].Key
	})
	if len(all) > MaxAlarms {
		all = all[:MaxAlarms]
		out.Truncated = true
	}
	for _, a := range all {
		if a.ZoneDependent && !zones.Has(a.Calendar.OwnerID) {
			uc.zones.Fallback(ctx, a.Calendar.OwnerID)
		}
	}
	out.Alarms = all
	return out, nil
}
