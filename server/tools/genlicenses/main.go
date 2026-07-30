// Command genlicenses generates the committed open-source attribution manifest
// for the Go side of the project (story 101).
//
// It deliberately depends on NOTHING but the standard library and the `go`
// command itself: adding a license scanner as a module dependency would enlarge
// the very dependency set we are attributing, and the module cache already holds
// every LICENSE file we need. No network access is required at request time —
// the generated JSON is committed and embedded with //go:embed (see
// internal/usecase/about).
//
// Usage (run from the `server/` directory):
//
//	go run ./tools/genlicenses                 # modules linked into ./cmd/server
//	go run ./tools/genlicenses -all            # the entire module graph
//	go run ./tools/genlicenses -out other.json
//
// Re-run it after any go.mod change and commit the result.
//
// LICENSE DETECTION IS BEST-EFFORT. It matches distinctive phrases of the
// common license families against the module's license file. Anything it cannot
// pin down unambiguously is reported as "unknown" rather than guessed at: a
// wrong license attribution is worse than an absent one, because a reader may
// rely on it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// defaultOut is relative to the `server/` directory (the module root).
const defaultOut = "internal/usecase/about/open_source_go.json"

// licenseUnknown is emitted whenever detection is not confident. Keep it in sync
// with the value the frontend treats as "not detected".
const licenseUnknown = "unknown"

// maxLicenseBytes caps how much of a license file we read. Real license texts
// are a few KB; the cap only guards against a pathological file in the cache.
const maxLicenseBytes = 512 * 1024

// pkg is one attributed dependency. The field set is fixed by story 101.
type pkg struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	License string `json:"license"`
	URL     string `json:"url"`
}

// manifest is the shape of the generated JSON, mirrored by the frontend
// manifest in webinterface/public/open-source.json and by the
// GET /api/v1/about/open-source response.
//
// There is deliberately NO timestamp: the output must be deterministic so that
// re-running the generator on an unchanged go.mod produces a byte-identical file
// (an empty diff, instead of noise on every build).
type manifest struct {
	Generator string `json:"generator"`
	Note      string `json:"note"`
	Count     int    `json:"count"`
	Packages  []pkg  `json:"packages"`
}

// modInfo is the subset of `go list -m -json` we consume.
type modInfo struct {
	Path    string
	Version string
	Dir     string
	Main    bool
	Replace *modInfo
}

// listPkg is the subset of `go list -deps -json` we consume.
type listPkg struct {
	Standard bool
	Module   *modInfo
}

func main() {
	out := flag.String("out", defaultOut, "path of the manifest to write")
	pattern := flag.String("pattern", "./cmd/server", "package pattern whose dependency closure is attributed")
	everything := flag.Bool("all", false, "attribute the whole module graph instead of just the shipped closure")
	flag.Parse()

	if err := run(*out, *pattern, *everything); err != nil {
		fmt.Fprintf(os.Stderr, "genlicenses: %v\n", err)
		os.Exit(1)
	}
}

func run(outPath, pattern string, everything bool) error {
	mods, err := listModules()
	if err != nil {
		return err
	}

	// Default to the modules that actually contribute code to the binary we
	// ship. The full module graph (`go list -m all`) additionally contains
	// build/test-only modules that are never linked in AND are usually not even
	// present in the module cache, so their license cannot be read — they would
	// show up as a wall of "unknown" entries that we have no obligation to
	// attribute in the first place. `-all` opts into the wider set.
	var shipped map[string]bool
	if !everything {
		if shipped, err = shippedModules(pattern); err != nil {
			return err
		}
	}

	pkgs := make([]pkg, 0, len(mods))
	for _, m := range mods {
		if m.Main {
			continue // our own code needs no attribution
		}
		if shipped != nil && !shipped[m.Path] {
			continue
		}
		eff := m
		if m.Replace != nil {
			// A replace directive means the code actually built comes from the
			// replacement, so read ITS license and report ITS version.
			eff = *m.Replace
		}
		version := eff.Version
		if version == "" {
			version = licenseUnknown
		}
		pkgs = append(pkgs, pkg{
			Name:    m.Path,
			Version: version,
			License: detectLicenseInDir(eff.Dir),
			URL:     repoURL(m.Path),
		})
	}

	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Name != pkgs[j].Name {
			return pkgs[i].Name < pkgs[j].Name
		}
		return pkgs[i].Version < pkgs[j].Version
	})

	man := manifest{
		Generator: "server/tools/genlicenses (go run ./tools/genlicenses)",
		Note:      "License detection is best-effort; \"" + licenseUnknown + "\" means it could not be determined automatically, not that the package is unlicensed.",
		Count:     len(pkgs),
		Packages:  pkgs,
	}

	data, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if dir := filepath.Dir(outPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return err
	}

	unknown := 0
	for _, p := range pkgs {
		if p.License == licenseUnknown {
			unknown++
		}
	}
	fmt.Printf("genlicenses: wrote %s (%d modules, %d with undetected license)\n", outPath, len(pkgs), unknown)
	return nil
}

// listModules runs `go list -m -json all` and decodes the concatenated JSON
// objects it streams (it is NOT a JSON array).
func listModules() ([]modInfo, error) {
	data, err := runGo("list", "-m", "-json", "all")
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	var mods []modInfo
	for {
		var m modInfo
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decoding `go list -m -json all`: %w", err)
		}
		mods = append(mods, m)
	}
	return mods, nil
}

// shippedModules returns the set of module paths providing packages in the
// dependency closure of `pattern`. Keys are the ORIGINAL (pre-replace) module
// paths, which is what `go list -m` reports as Path, so the two lists join.
func shippedModules(pattern string) (map[string]bool, error) {
	data, err := runGo("list", "-deps", "-json", pattern)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	set := map[string]bool{}
	for {
		var p listPkg
		if err := dec.Decode(&p); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decoding `go list -deps -json %s`: %w", pattern, err)
		}
		if p.Standard || p.Module == nil {
			continue
		}
		set[p.Module.Path] = true
	}
	return set, nil
}

func runGo(args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running `go %s`: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// licenseFileNames are the base names (extension stripped, upper-cased) we
// accept as a license file, in preference order.
var licenseFileNames = []string{"LICENSE", "LICENCE", "COPYING", "UNLICENSE", "LICENSE-MIT", "LICENSE-APACHE", "COPYRIGHT"}

// detectLicenseInDir finds the license file of a module directory and identifies
// it. An empty dir (module not in the cache) or no license file yields
// "unknown".
func detectLicenseInDir(dir string) string {
	if dir == "" {
		return licenseUnknown
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return licenseUnknown
	}

	best, bestRank := "", len(licenseFileNames)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		base := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
		for rank, want := range licenseFileNames {
			if base == want && rank < bestRank {
				best, bestRank = name, rank
			}
		}
	}
	if best == "" {
		return licenseUnknown
	}

	f, err := os.Open(filepath.Join(dir, best))
	if err != nil {
		return licenseUnknown
	}
	defer f.Close()
	text, err := io.ReadAll(io.LimitReader(f, maxLicenseBytes))
	if err != nil {
		return licenseUnknown
	}
	return detectLicense(string(text))
}

var whitespace = regexp.MustCompile(`\s+`)

// licenseSignature pairs an SPDX identifier with phrases that must ALL appear in
// the (whitespace-normalised, upper-cased) license text for it to match.
type licenseSignature struct {
	id      string
	phrases []string
	// absent, when present, must NOT appear — used to separate license
	// variants that share their opening paragraphs.
	absent []string
}

// signatures are checked independently; a text matching more than one distinct
// identifier is reported as "unknown" (see detectLicense). Phrases are upper-case
// because the text is upper-cased before matching.
var signatures = []licenseSignature{
	{id: "Apache-2.0", phrases: []string{"APACHE LICENSE", "VERSION 2.0"}},
	{id: "MIT", phrases: []string{"PERMISSION IS HEREBY GRANTED, FREE OF CHARGE, TO ANY PERSON OBTAINING A COPY", "WITHOUT RESTRICTION"}},
	{id: "ISC", phrases: []string{"PERMISSION TO USE, COPY, MODIFY, AND/OR DISTRIBUTE THIS SOFTWARE FOR ANY PURPOSE WITH OR WITHOUT FEE"}},
	{id: "BSD-3-Clause", phrases: []string{"REDISTRIBUTION AND USE IN SOURCE AND BINARY FORMS", "NEITHER THE NAME OF"}},
	{id: "BSD-2-Clause", phrases: []string{"REDISTRIBUTION AND USE IN SOURCE AND BINARY FORMS", "REDISTRIBUTIONS IN BINARY FORM"}, absent: []string{"NEITHER THE NAME OF", "NAME OF THE COPYRIGHT HOLDER"}},
	{id: "MPL-2.0", phrases: []string{"MOZILLA PUBLIC LICENSE", "VERSION 2.0"}},
	{id: "LGPL-3.0", phrases: []string{"GNU LESSER GENERAL PUBLIC LICENSE", "VERSION 3"}},
	{id: "LGPL-2.1", phrases: []string{"GNU LESSER GENERAL PUBLIC LICENSE", "VERSION 2.1"}},
	{id: "GPL-3.0", phrases: []string{"GNU GENERAL PUBLIC LICENSE", "VERSION 3"}, absent: []string{"GNU LESSER GENERAL PUBLIC LICENSE"}},
	{id: "GPL-2.0", phrases: []string{"GNU GENERAL PUBLIC LICENSE", "VERSION 2"}, absent: []string{"GNU LESSER GENERAL PUBLIC LICENSE"}},
	{id: "Unlicense", phrases: []string{"THIS IS FREE AND UNENCUMBERED SOFTWARE RELEASED INTO THE PUBLIC DOMAIN"}},
	{id: "CC0-1.0", phrases: []string{"CREATIVE COMMONS", "CC0"}},
	{id: "Zlib", phrases: []string{"THIS SOFTWARE IS PROVIDED 'AS-IS', WITHOUT ANY EXPRESS OR IMPLIED WARRANTY"}},
}

// detectLicense identifies a license text, or returns "unknown" when it matches
// nothing or matches ambiguously (e.g. a file containing two license texts).
// Ambiguity is intentionally NOT resolved by picking the first match: reporting
// one half of a dual license as the whole truth is a wrong attribution.
func detectLicense(text string) string {
	norm := strings.ToUpper(whitespace.ReplaceAllString(text, " "))
	if strings.TrimSpace(norm) == "" {
		return licenseUnknown
	}

	var matches []string
	for _, sig := range signatures {
		if matchesSignature(norm, sig) {
			matches = append(matches, sig.id)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return licenseUnknown
}

func matchesSignature(norm string, sig licenseSignature) bool {
	for _, p := range sig.phrases {
		if !strings.Contains(norm, p) {
			return false
		}
	}
	for _, a := range sig.absent {
		if strings.Contains(norm, a) {
			return false
		}
	}
	return true
}

var majorSuffix = regexp.MustCompile(`/v[0-9]+$`)

// repoURL maps a module path to a human-visitable page. For the well-known code
// hosts that is the repository itself; for everything else it is the pkg.go.dev
// page, which always exists for a published module and links on to both the
// source and the license text. Guessing an https:// URL for an arbitrary vanity
// module path would produce dead links.
func repoURL(modPath string) string {
	trimmed := majorSuffix.ReplaceAllString(modPath, "")
	parts := strings.Split(trimmed, "/")
	switch parts[0] {
	case "github.com", "gitlab.com", "bitbucket.org", "codeberg.org", "sr.ht":
		if len(parts) >= 3 {
			return "https://" + strings.Join(parts[:3], "/")
		}
	case "gopkg.in":
		return "https://" + trimmed
	}
	return "https://pkg.go.dev/" + modPath
}
