// Package ownerzone resolves the time zone of a calendar's owner (#169, spec
// §6.5): floating and all-day values of an event or task are read in that zone,
// whoever looks at them. It reads the owner's `timezone` preference and falls
// back to UTC (today's behaviour) when there is none or it no longer validates.
package ownerzone

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

// FallbackMessage is the INFO line logged the first time an owner's zone is
// needed and unset.
const FallbackMessage = "alarm time zone fallback: owner has no timezone preference, using UTC"

// Resolver looks up owner zones. One instance per process, so Fallback logs once
// per owner per process.
type Resolver struct {
	prefs  user.UserPreferenceRepository
	users  user.UserRepository
	logged sync.Map // owner id -> struct{}
}

// New creates a Resolver.
func New(prefs user.UserPreferenceRepository, users user.UserRepository) *Resolver {
	return &Resolver{prefs: prefs, users: users}
}

// Zones maps an owner id to its validated zone. An owner without a valid
// preference is absent.
type Zones map[uint]*time.Location

// For returns the owner's zone, UTC when it has none.
func (z Zones) For(ownerID uint) *time.Location {
	if loc, ok := z[ownerID]; ok {
		return loc
	}
	return time.UTC
}

// Has reports whether the owner has a valid zone.
func (z Zones) Has(ownerID uint) bool {
	_, ok := z[ownerID]
	return ok
}

// Many loads the zones of all owners with one ListByKey. Owners with no row or
// an invalid value are absent from the result.
func (r *Resolver) Many(ctx context.Context, ownerIDs []uint) (Zones, error) {
	zones := Zones{}
	if len(ownerIDs) == 0 {
		return zones, nil
	}
	rows, err := r.prefs.ListByKey(ctx, ownerIDs, user.PrefTimezone)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if loc, ok := user.LoadTimezone(row.Value); ok {
			zones[row.UserID] = loc
		}
	}
	return zones, nil
}

// One loads a single owner's zone. ok is false (and the zone UTC) when the
// owner has no valid preference.
func (r *Resolver) One(ctx context.Context, ownerID uint) (loc *time.Location, ok bool, err error) {
	pref, err := r.prefs.GetByKey(ctx, ownerID, user.PrefTimezone)
	if err != nil {
		return nil, false, err
	}
	if pref != nil {
		if loc, ok := user.LoadTimezone(pref.Value); ok {
			return loc, true, nil
		}
	}
	return time.UTC, false, nil
}

// Fallback logs the INFO line for ownerID, naming the owner's user UUID (never
// the e-mail), only the first time per process. A failed lookup of the user
// logs nothing and is retried on the next call.
func (r *Resolver) Fallback(ctx context.Context, ownerID uint) {
	if _, seen := r.logged.Load(ownerID); seen {
		return
	}
	u, err := r.users.GetByID(ctx, ownerID)
	if err != nil || u == nil {
		return
	}
	if _, loaded := r.logged.LoadOrStore(ownerID, struct{}{}); loaded {
		return
	}
	slog.Info(FallbackMessage, slog.String("owner_uuid", u.UUID))
}
