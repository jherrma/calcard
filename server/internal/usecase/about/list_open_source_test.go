package about

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The committed manifest is what makes the endpoint work even when the generator
// never runs (e.g. a plain `go build`), so guard its integrity here rather than
// discovering a broken/empty file through a 500 in production.
func TestListOpenSourceUseCase_EmbeddedManifest(t *testing.T) {
	uc := NewListOpenSourceUseCase()

	man, err := uc.Execute()
	require.NoError(t, err)

	assert.NotEmpty(t, man.Packages, "committed manifest must not be empty — regenerate with `go run ./tools/genlicenses`")
	assert.Equal(t, len(man.Packages), man.Count, "count must match the number of packages")
	assert.Contains(t, man.Note, "best-effort", "the caveat about license detection must survive into the response")

	for _, p := range man.Packages {
		assert.NotEmpty(t, p.Name, "every package needs a name")
		assert.NotEmpty(t, p.Version, "every package needs a version")
		assert.NotEmpty(t, p.License, "license must be a value (\"unknown\" when undetected), never empty")
		assert.True(t, strings.HasPrefix(p.URL, "https://"), "URL must be an https link, got %q for %q", p.URL, p.Name)
	}
}

func TestListOpenSourceUseCase_CachesDecodedManifest(t *testing.T) {
	uc := NewListOpenSourceUseCase()

	first, err := uc.Execute()
	require.NoError(t, err)
	second, err := uc.Execute()
	require.NoError(t, err)

	assert.Equal(t, first.Count, second.Count)
	// Same backing array => the decode happened once.
	require.NotEmpty(t, first.Packages)
	assert.Same(t, &first.Packages[0], &second.Packages[0])
}
