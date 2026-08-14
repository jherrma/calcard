package user

import (
	"context"
	"errors"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

// ErrUserNotFound is returned when the authenticated UUID no longer resolves to
// a user row (e.g. the account was deleted while a token was still valid).
var ErrUserNotFound = errors.New("user not found")

// GetPreferencesUseCase reads a user's preferences, filling in defaults.
type GetPreferencesUseCase struct {
	repo     user.UserRepository
	prefRepo user.UserPreferenceRepository
}

func NewGetPreferencesUseCase(repo user.UserRepository, prefRepo user.UserPreferenceRepository) *GetPreferencesUseCase {
	return &GetPreferencesUseCase{repo: repo, prefRepo: prefRepo}
}

// Execute returns the full preference map for userUUID: every known key is
// present, with the stored value where the user set one and the default
// otherwise (story 103).
func (uc *GetPreferencesUseCase) Execute(ctx context.Context, userUUID string) (map[string]string, error) {
	u, err := uc.repo.GetByUUID(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}

	stored, err := uc.prefRepo.GetByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	return mergePreferences(stored), nil
}

// mergePreferences overlays stored rows onto the defaults.
//
// Rows for keys this build no longer knows (or values that are no longer allowed
// after the option set changed) are skipped rather than echoed back: the
// response contract is "the known keys, each with a usable value".
//
// Values are normalized on the way out as well as on the way in, so a row
// written by an older build in a non-canonical form (an uppercase hex colour)
// still reads back as the setting the user made rather than silently reverting
// to the default.
func mergePreferences(stored []user.UserPreference) map[string]string {
	prefs := user.DefaultPreferences()
	for _, p := range stored {
		v := user.NormalizePreferenceValue(p.Key, p.Value)
		if user.IsAllowedPreferenceValue(p.Key, v) {
			prefs[p.Key] = v
		}
	}
	return prefs
}
