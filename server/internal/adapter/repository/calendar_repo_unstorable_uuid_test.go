package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestLookupsRejectUnstorableUUIDs: a lookup key that PostgreSQL cannot store
// as text (invalid UTF-8, a NUL from %00) is a miss, calendar.ErrNotFound, and
// never reaches the database; before, PostgreSQL answered it with a 500.
func TestLookupsRejectUnstorableUUIDs(t *testing.T) {
	db := newFeedDB(t)
	repo := repository.NewCalendarRepository(db)
	ctx := context.Background()

	for _, bad := range []string{"\xff\xfe", "abc\x00def", "\xc3"} {
		cal, err := repo.GetByUUID(ctx, bad)
		assert.Nil(t, cal)
		assert.True(t, errors.Is(err, calendar.ErrNotFound), "calendar %q: %v", bad, err)

		obj, err := repo.GetCalendarObjectByUUID(ctx, bad)
		assert.Nil(t, obj)
		assert.True(t, errors.Is(err, calendar.ErrNotFound), "object %q: %v", bad, err)
		assert.True(t, errors.Is(err, gorm.ErrRecordNotFound), "object %q keeps the record-not-found match", bad)
	}

	// A well-formed unknown key is still an ordinary not-found.
	_, err := repo.GetByUUID(ctx, "unknown-uuid")
	require.True(t, errors.Is(err, calendar.ErrNotFound))
}
