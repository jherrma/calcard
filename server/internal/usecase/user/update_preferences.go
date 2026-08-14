package user

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

var (
	// ErrUnknownPreferenceKey is returned for a key this server does not manage.
	ErrUnknownPreferenceKey = errors.New("unknown preference key")
	// ErrInvalidPreferenceValue is returned for a known key with a value outside
	// its allowed set.
	ErrInvalidPreferenceValue = errors.New("invalid preference value")
	// ErrNoPreferencesGiven is returned when the request carries nothing to write.
	ErrNoPreferencesGiven = errors.New("no preferences provided")
)

// UpdatePreferencesUseCase validates and persists preference changes.
type UpdatePreferencesUseCase struct {
	repo     user.UserRepository
	prefRepo user.UserPreferenceRepository
}

func NewUpdatePreferencesUseCase(repo user.UserRepository, prefRepo user.UserPreferenceRepository) *UpdatePreferencesUseCase {
	return &UpdatePreferencesUseCase{repo: repo, prefRepo: prefRepo}
}

// Execute validates every entry in updates and, only if all of them pass,
// upserts them. Returns the full merged preference map, same shape as
// GetPreferencesUseCase.
//
// Validation happens before the first write on purpose: a partially applied
// PATCH would leave the client's UI and the stored state disagreeing with no way
// to tell which keys landed.
func (uc *UpdatePreferencesUseCase) Execute(ctx context.Context, userUUID string, updates map[string]string) (map[string]string, error) {
	if len(updates) == 0 {
		return nil, ErrNoPreferencesGiven
	}

	// Iterate in sorted key order so a request with several bad keys always
	// reports the same one — an unordered map range would make the error message
	// (and its test) flaky.
	keys := make([]string, 0, len(updates))
	for k := range updates {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Canonical form is what gets validated AND what gets stored, so the table
	// never ends up holding two spellings of one value (story 046: "#3B82F6"
	// and "#3b82f6" are the same accent colour).
	normalized := make(map[string]string, len(updates))
	for _, k := range keys {
		if !user.KnownPreferenceKey(k) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownPreferenceKey, k)
		}
		v := user.NormalizePreferenceValue(k, updates[k])
		if !user.IsAllowedPreferenceValue(k, v) {
			return nil, fmt.Errorf("%w: %q must be %s", ErrInvalidPreferenceValue, k,
				user.PreferenceValueHint(k))
		}
		normalized[k] = v
	}

	u, err := uc.repo.GetByUUID(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}

	for _, k := range keys {
		pref := &user.UserPreference{UserID: u.ID, Key: k, Value: normalized[k]}
		if err := uc.prefRepo.Upsert(ctx, pref); err != nil {
			return nil, err
		}
	}

	stored, err := uc.prefRepo.GetByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	return mergePreferences(stored), nil
}
