package user

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCardDAVCredential_IsValid(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	oneHourLater := now.Add(1 * time.Hour)

	tests := []struct {
		name string
		cred CardDAVCredential
		want bool
	}{
		{
			name: "Valid credential",
			cred: CardDAVCredential{
				RevokedAt: nil,
				ExpiresAt: nil,
			},
			want: true,
		},
		{
			name: "Revoked credential",
			cred: CardDAVCredential{
				RevokedAt: &now,
				ExpiresAt: nil,
			},
			want: false,
		},
		{
			name: "Expired credential",
			cred: CardDAVCredential{
				RevokedAt: nil,
				ExpiresAt: &oneHourAgo,
			},
			want: false,
		},
		{
			name: "Not expired credential",
			cred: CardDAVCredential{
				RevokedAt: nil,
				ExpiresAt: &oneHourLater,
			},
			want: true,
		},
		{
			name: "Revoked and Expired credential",
			cred: CardDAVCredential{
				RevokedAt: &now,
				ExpiresAt: &oneHourAgo,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cred.IsValid())
		})
	}
}

func TestCardDAVCredential_CanWrite(t *testing.T) {
	tests := []struct {
		name string
		cred CardDAVCredential
		want bool
	}{
		{
			name: "Read-write permission",
			cred: CardDAVCredential{Permission: "read-write"},
			want: true,
		},
		{
			name: "Read permission",
			cred: CardDAVCredential{Permission: "read"},
			want: false,
		},
		{
			name: "Empty permission",
			cred: CardDAVCredential{Permission: ""},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cred.CanWrite())
		})
	}
}

func TestCardDAVCredential_IsRevoked(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		cred CardDAVCredential
		want bool
	}{
		{
			name: "Revoked",
			cred: CardDAVCredential{RevokedAt: &now},
			want: true,
		},
		{
			name: "Not Revoked",
			cred: CardDAVCredential{RevokedAt: nil},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cred.IsRevoked())
		})
	}
}

func TestCardDAVCredential_IsExpired(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	oneHourLater := now.Add(1 * time.Hour)

	tests := []struct {
		name string
		cred CardDAVCredential
		want bool
	}{
		{
			name: "Expired",
			cred: CardDAVCredential{ExpiresAt: &oneHourAgo},
			want: true,
		},
		{
			name: "Not Expired (Future)",
			cred: CardDAVCredential{ExpiresAt: &oneHourLater},
			want: false,
		},
		{
			name: "Not Expired (Nil)",
			cred: CardDAVCredential{ExpiresAt: nil},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cred.IsExpired())
		})
	}
}
