package repository

import (
	"context"
	"errors"

	"strings"
	"time"

	"github.com/jherrma/caldav-server/internal/domain/user"
	"gorm.io/gorm"
)

type gormUserRepo struct {
	db *gorm.DB
}

// NewUserRepository creates a new GORM-based user repository
func NewUserRepository(db *gorm.DB) user.UserRepository {
	return &gormUserRepo{db: db}
}

func (r *gormUserRepo) Create(ctx context.Context, u *user.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *gormUserRepo) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u user.User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// GetByUsername matches case-insensitively (see findByUsername): older
// generated usernames are mixed-case, newer ones and set-up admin usernames
// lower-case, and DAV clients and share dialogs send whatever the user typed.
func (r *gormUserRepo) GetByUsername(ctx context.Context, username string) (*user.User, error) {
	return findByUsername[user.User](r.db.WithContext(ctx), username)
}

func (r *gormUserRepo) GetByUUID(ctx context.Context, uuid string) (*user.User, error) {
	var u user.User
	if err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (r *gormUserRepo) GetByID(ctx context.Context, id uint) (*user.User, error) {
	var u user.User
	if err := r.db.WithContext(ctx).First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// Update saves every column EXCEPT tokens_valid_after. A User struct is
// usually loaded at the start of a request (the auth middleware does it); if a
// logout elsewhere moves the marker in the meantime, a full Save would write
// the stale value back and resurrect the tokens that logout just revoked.
func (r *gormUserRepo) Update(ctx context.Context, u *user.User) error {
	return saveUser(r.db.WithContext(ctx), u)
}

func saveUser(db *gorm.DB, u *user.User) error {
	return db.Omit("TokensValidAfter").Save(u).Error
}

// CompletePasswordReset saves u — the caller has set its new PasswordHash and
// EmailVerified — and, in the same transaction, closes the account to anyone
// who held it before: every access token issued before at, every refresh
// token, every password-reset link, and every app password, CalDAV/CardDAV
// credential, MCP token and OAuth connection (accountCredentials). An account
// is usable before its address is confirmed, so whoever registered it may not
// be the person who now proved control of the mailbox. All or nothing: a
// failure leaves the old password and every credential in place.
func (r *gormUserRepo) CompletePasswordReset(ctx context.Context, u *user.User, at time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := saveUser(tx, u); err != nil {
			return err
		}
		if err := endSessions(tx, u.ID, at); err != nil {
			return err
		}
		return purgeAccountCredentials(tx, u.ID)
	})
}

// endSessions revokes the access tokens userID was issued before at and
// deletes its refresh tokens and password-reset links.
func endSessions(tx *gorm.DB, userID uint, at time.Time) error {
	if err := invalidateAccessTokens(tx, userID, at); err != nil {
		return err
	}
	if err := tx.Where("user_id = ?", userID).Delete(&user.RefreshToken{}).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ?", userID).Delete(&user.PasswordReset{}).Error
}

// InvalidateAccessTokens sets tokens_valid_after to at, written as UTC so
// SQLite's text timestamps stay comparable whatever the host time zone. It is
// a targeted single-column UPDATE, never a Save of a loaded User.
func (r *gormUserRepo) InvalidateAccessTokens(ctx context.Context, userID uint, at time.Time) error {
	return invalidateAccessTokens(r.db.WithContext(ctx), userID, at)
}

func invalidateAccessTokens(db *gorm.DB, userID uint, at time.Time) error {
	return db.Model(&user.User{}).
		Where("id = ?", userID).
		UpdateColumn("tokens_valid_after", at.UTC()).Error
}

func (r *gormUserRepo) Delete(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Revoke outstanding access tokens too. A soft-deleted user already
		// fails the auth lookup, but the marker keeps those tokens dead even
		// if the row is ever restored. Refresh tokens and reset links go.
		if err := endSessions(tx, userID, time.Now()); err != nil {
			return err
		}

		// Hard delete related data
		if err := tx.Where("user_id = ?", userID).Delete(&user.EmailVerification{}).Error; err != nil {
			return err
		}
		// Calendars, address books, shares in both directions, credentials:
		// a soft-deleted owner alone left all of it reachable (F18).
		if err := PurgeAccountData(tx, userID); err != nil {
			return err
		}

		// Release the e-mail and username: the unique indexes cover the
		// soft-deleted row, so without the tombstone the address could never
		// register again (F19).
		if err := tombstoneIdentity(tx.Model(&user.User{}).Where("id = ?", userID)); err != nil {
			return err
		}

		// Soft delete user
		return tx.Delete(&user.User{}, userID).Error
	})
}

// tombstoneIdentity rewrites email and username of the users matched by q to
// "deleted:<uuid>" (user.DeletedIdentityPrefix), in SQL so one statement
// serves any number of rows; `||` concatenates on SQLite and PostgreSQL alike.
func tombstoneIdentity(q *gorm.DB) error {
	expr := gorm.Expr("? || uuid", user.DeletedIdentityPrefix)
	return q.UpdateColumns(map[string]interface{}{"email": expr, "username": expr}).Error
}

func (r *gormUserRepo) CreateVerification(ctx context.Context, v *user.EmailVerification) error {
	return r.db.WithContext(ctx).Create(v).Error
}

func (r *gormUserRepo) GetVerificationByToken(ctx context.Context, token string) (*user.EmailVerification, error) {
	var v user.EmailVerification
	if err := r.db.WithContext(ctx).Preload("User").Where("token = ?", token).First(&v).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

func (r *gormUserRepo) DeleteVerification(ctx context.Context, token string) error {
	return r.db.WithContext(ctx).Where("token = ?", token).Delete(&user.EmailVerification{}).Error
}

// Count returns how many accounts have EVER existed, soft-deleted ones
// included. It backs first-run setup and admin_configured: if deleted rows were
// ignored, the last user deleting their account would silently re-open setup to
// anyone. (Delete tombstones the e-mail and username, so counting a deleted row
// no longer blocks its address from registering again.)
func (r *gormUserRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Unscoped().Model(&user.User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *gormUserRepo) GetByOAuth(ctx context.Context, provider, providerID string) (*user.User, error) {
	var conn user.OAuthConnection
	if err := r.db.WithContext(ctx).Preload("User").
		Where("provider = ? AND provider_id = ?", provider, providerID).
		First(&conn).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &conn.User, nil
}
