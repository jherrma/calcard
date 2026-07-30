// Package about serves project metadata that is not tied to any user, currently
// the open-source attribution list required by story 101.
package about

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"
)

// open_source_go.json is GENERATED — do not hand-edit. Regenerate it with
// `go run ./tools/genlicenses` from the server/ directory after any go.mod
// change (see server/tools/genlicenses/main.go).
//
// It is embedded rather than read from disk at runtime so the endpoint works in
// the scratch container image, needs no network access, and cannot 500 because a
// data file was left behind during deployment.
//
//go:embed open_source_go.json
var manifestFS embed.FS

const manifestFile = "open_source_go.json"

// OpenSourcePackage is one attributed third-party dependency. The JSON tags are
// the wire format of GET /api/v1/about/open-source and must stay in sync with
// the generator's output and the frontend's OpenSourcePackage type.
type OpenSourcePackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	License string `json:"license"`
	URL     string `json:"url"`
}

// OpenSourceManifest is the decoded attribution file.
type OpenSourceManifest struct {
	// Generator and Note describe HOW the list came to be. Note carries the
	// best-effort caveat about license detection and is surfaced in the UI, so a
	// reader is never led to believe "unknown" means "unlicensed".
	Generator string              `json:"generator"`
	Note      string              `json:"note"`
	Count     int                 `json:"count"`
	Packages  []OpenSourcePackage `json:"packages"`
}

// ListOpenSourceUseCase returns the embedded Go dependency attribution list.
// It has no repository: the data is a build artefact, not application state.
type ListOpenSourceUseCase struct {
	// The manifest never changes for a given binary, so decode it once on first
	// use and hand out the cached value afterwards.
	once     sync.Once
	manifest OpenSourceManifest
	err      error
}

// NewListOpenSourceUseCase creates a new ListOpenSourceUseCase.
func NewListOpenSourceUseCase() *ListOpenSourceUseCase {
	return &ListOpenSourceUseCase{}
}

// Execute returns the attribution manifest. The returned Packages slice is the
// cached one; callers must treat it as read-only.
func (uc *ListOpenSourceUseCase) Execute() (OpenSourceManifest, error) {
	uc.once.Do(func() {
		data, err := manifestFS.ReadFile(manifestFile)
		if err != nil {
			uc.err = fmt.Errorf("reading embedded %s: %w", manifestFile, err)
			return
		}
		var m OpenSourceManifest
		if err := json.Unmarshal(data, &m); err != nil {
			uc.err = fmt.Errorf("parsing embedded %s: %w", manifestFile, err)
			return
		}
		// Never hand a nil slice to the JSON encoder — the frontend expects an
		// array, and `null` would break its `.length` / filter code paths.
		if m.Packages == nil {
			m.Packages = []OpenSourcePackage{}
		}
		uc.manifest = m
	})

	return uc.manifest, uc.err
}
