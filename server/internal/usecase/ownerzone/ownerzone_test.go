package ownerzone

import (
	"context"
	"testing"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type prefs struct {
	user.UserPreferenceRepository
	rows     map[uint]string
	listCall int
}

func (p *prefs) ListByKey(_ context.Context, ids []uint, key string) ([]user.UserPreference, error) {
	p.listCall++
	var out []user.UserPreference
	for _, id := range ids {
		if v, ok := p.rows[id]; ok {
			out = append(out, user.UserPreference{UserID: id, Key: key, Value: v})
		}
	}
	return out, nil
}

func (p *prefs) GetByKey(_ context.Context, id uint, key string) (*user.UserPreference, error) {
	if v, ok := p.rows[id]; ok {
		return &user.UserPreference{UserID: id, Key: key, Value: v}, nil
	}
	return nil, nil
}

type users struct{ user.UserRepository }

func (users) GetByID(context.Context, uint) (*user.User, error) { return &user.User{UUID: "x"}, nil }

func TestManyValidatesAndFallsBack(t *testing.T) {
	p := &prefs{rows: map[uint]string{1: "Europe/Berlin", 2: "Not/AZone", 3: "Local"}}
	r := New(p, users{})
	z, err := r.Many(context.Background(), []uint{1, 2, 3, 4})
	require.NoError(t, err)
	assert.Equal(t, 1, p.listCall)
	assert.True(t, z.Has(1))
	assert.Equal(t, "Europe/Berlin", z.For(1).String())
	for _, id := range []uint{2, 3, 4} {
		assert.False(t, z.Has(id), id)
		assert.Equal(t, "UTC", z.For(id).String(), id)
	}
	_, err = r.Many(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 1, p.listCall, "no owners, no query")
}

func TestOne(t *testing.T) {
	r := New(&prefs{rows: map[uint]string{1: "Asia/Tokyo", 2: "bogus"}}, users{})
	loc, ok, err := r.One(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "Asia/Tokyo", loc.String())
	for _, id := range []uint{2, 3} {
		loc, ok, err = r.One(context.Background(), id)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Equal(t, "UTC", loc.String())
	}
}
