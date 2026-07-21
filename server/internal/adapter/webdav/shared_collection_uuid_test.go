package webdav

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSharedCalendarUUIDPathAvoidsCollision is the regression test for #47: when
// a recipient owns a calendar with the SAME path as one shared with them, the
// share must be addressed by UUID so the two don't collide on one DAV URL — and
// resolution (which drives PUT routing) must send each URL to the right
// calendar, not silently prefer the owned one for both.
func TestSharedCalendarUUIDPathAvoidsCollision(t *testing.T) {
	_, db, _ := setupTestApp(t)
	defer db.Close()

	ctx := context.Background()
	userRepo := repository.NewUserRepository(db.DB())
	calRepo := repository.NewCalendarRepository(db.DB())
	shareRepo := repository.NewCalendarShareRepository(db.DB())
	backend := NewCalDAVBackend(calRepo, userRepo, shareRepo)

	a := &user.User{UUID: "a", Email: "a@example.com", Username: "usera", IsActive: true}
	require.NoError(t, userRepo.Create(ctx, a))
	b := &user.User{UUID: "b", Email: "b@example.com", Username: "userb", IsActive: true}
	require.NoError(t, userRepo.Create(ctx, b))

	// Both own a calendar at path "work" — the collision.
	calA := &calendar.Calendar{UserID: a.ID, UUID: uuid.New().String(), Path: "work", Name: "A's Work", Timezone: "UTC"}
	require.NoError(t, calRepo.Create(ctx, calA))
	calB := &calendar.Calendar{UserID: b.ID, UUID: uuid.New().String(), Path: "work", Name: "B's Work", Timezone: "UTC"}
	require.NoError(t, calRepo.Create(ctx, calB))

	require.NoError(t, shareRepo.Create(ctx, &sharing.CalendarShare{
		UUID: uuid.New().String(), CalendarID: calA.ID, SharedWithID: b.ID, Permission: "read-write",
	}))

	ctxB := WithUser(ctx, b)

	// B's listing: two distinct paths — own "work" and A's by UUID.
	cals, err := backend.ListCalendars(ctxB)
	require.NoError(t, err)
	paths := map[string]bool{}
	for _, c := range cals {
		paths[c.Path] = true
	}
	assert.True(t, paths["/dav/userb/calendars/work/"], "B's own calendar keeps its friendly path")
	assert.True(t, paths["/dav/userb/calendars/"+calA.UUID+"/"], "the shared calendar is addressed by UUID")

	// Resolution (what PUT uses) sends each URL to the right calendar.
	own, _, ownPerm, err := backend.ResolvePath(ctxB, "/dav/userb/calendars/work/")
	require.NoError(t, err)
	assert.Equal(t, calB.ID, own.ID, "the friendly path must resolve to B's OWN calendar")
	assert.Equal(t, calendar.PermissionOwner, ownPerm)

	shared, _, sharedPerm, err := backend.ResolvePath(ctxB, "/dav/userb/calendars/"+calA.UUID+"/")
	require.NoError(t, err)
	assert.Equal(t, calA.ID, shared.ID, "the UUID path must resolve to A's shared calendar")
	assert.Equal(t, calendar.PermissionReadWrite, sharedPerm)
}
