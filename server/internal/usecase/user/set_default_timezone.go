package user

import (
	"context"
	"fmt"

	"github.com/jherrma/caldav-server/internal/domain/user"
)

// SetDefaultTimezoneUseCase stores a zone as the user's timezone preference
// only when they have none (set-if-empty), then answers what is stored.
type SetDefaultTimezoneUseCase struct {
	repo     user.UserRepository
	prefRepo user.UserPreferenceRepository
}

func NewSetDefaultTimezoneUseCase(repo user.UserRepository, prefRepo user.UserPreferenceRepository) *SetDefaultTimezoneUseCase {
	return &SetDefaultTimezoneUseCase{repo: repo, prefRepo: prefRepo}
}

// Execute never overwrites a valid stored zone, including an explicit "UTC".
// A stored value that no longer validates (hand-edited row) is replaced by the
// given zone. The returned value is always a valid zone.
func (uc *SetDefaultTimezoneUseCase) Execute(ctx context.Context, userUUID, zone string) (string, error) {
	zone = user.NormalizePreferenceValue(user.PrefTimezone, zone)
	if !user.IsAllowedPreferenceValue(user.PrefTimezone, zone) {
		return "", fmt.Errorf("%w: %q must be %s", ErrInvalidPreferenceValue, user.PrefTimezone,
			user.PreferenceValueHint(user.PrefTimezone))
	}

	u, err := uc.repo.GetByUUID(ctx, userUUID)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", ErrUserNotFound
	}

	if err := uc.prefRepo.InsertIfAbsent(ctx, &user.UserPreference{UserID: u.ID, Key: user.PrefTimezone, Value: zone}); err != nil {
		return "", err
	}
	stored, err := uc.prefRepo.GetByKey(ctx, u.ID, user.PrefTimezone)
	if err != nil {
		return "", err
	}
	if stored == nil {
		// Deleted between the insert and the read; the caller's zone is the answer.
		return zone, nil
	}
	if user.IsAllowedPreferenceValue(user.PrefTimezone, stored.Value) {
		return stored.Value, nil
	}

	// Compare-and-set so a concurrent PATCH to a valid zone is not clobbered.
	if _, err := uc.prefRepo.ReplaceValueIf(ctx, u.ID, user.PrefTimezone, stored.Value, zone); err != nil {
		return "", err
	}
	stored, err = uc.prefRepo.GetByKey(ctx, u.ID, user.PrefTimezone)
	if err != nil {
		return "", err
	}
	if stored == nil || !user.IsAllowedPreferenceValue(user.PrefTimezone, stored.Value) {
		return zone, nil
	}
	return stored.Value, nil
}
