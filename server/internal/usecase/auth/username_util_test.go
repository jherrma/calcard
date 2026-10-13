package auth

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// New account usernames (registration and OAuth sign-up) are stored in lower
// case, like the set-up admin's and every new DAV credential's.
func TestGenerateUniqueUsername_LowercaseLetters(t *testing.T) {
	ctx := context.Background()
	repo := new(mockUserRepo)
	repo.On("GetByUsername", ctx, mock.Anything).Return(nil, nil)

	lowercase16 := regexp.MustCompile(`^[a-z]{16}$`)
	for range 50 {
		name, err := GenerateUniqueUsername(ctx, repo)
		require.NoError(t, err)
		assert.Regexp(t, lowercase16, name)
	}
}
