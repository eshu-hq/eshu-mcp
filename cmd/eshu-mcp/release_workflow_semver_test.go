package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleaseVerifyPublishedSemverGrammar pins the verify-published semver
// gate to the semver 2.0 grammar (Refs eshu-hq/eshu-mcp#53). metadata-action
// emits the X.Y.Z image tag only for tags node-semver accepts, so a looser
// gate makes verify-published demand a :ver tag that was never published.
//
// The three `+build` cases assert grammar acceptance only. metadata-action
// strips build metadata and Docker tags cannot hold `+`, and the version-stamp
// step in release.yml refuses `+` before verify-published ever runs, so such
// a tag cannot publish; the fixtures model the gate in isolation.
//
// Each case is discriminating through the registry contents alone: a tag the
// gate accepts must resolve as :<tag without v>; a tag it rejects must
// resolve as :sha-<full> and say so with a ::notice::. The wrong branch finds
// no manifest and fails the script.
func TestReleaseVerifyPublishedSemverGrammar(t *testing.T) {
	t.Parallel()
	script := stepScripts(t, relBody(t), "Verify the tag's image and Release exist")[0]
	const image = "ghcr.io/eshu-hq/eshu-mcp"
	const fullSHA = "0123456789abcdef0123456789abcdef01234567"
	manifest := `{"digest":"pushed"}`
	digest := sha(manifest)

	run := func(t *testing.T, tag string, published ...string) result {
		t.Helper()
		s := newSandbox(t)
		for _, name := range published {
			key := strings.NewReplacer("/", "_", ":", "_").Replace(image + ":" + name)
			if err := os.WriteFile(filepath.Join(s.registry, key), []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return s.exec(script,
			"TAG="+tag, "DIGEST="+digest, "IMAGE="+image, "GITHUB_SHA="+fullSHA,
			"STUB_BODY=Image: "+image+"@"+digest, "STUB_ASSETS=eshu-mcp-image-spdx.json",
		)
	}

	valid := []string{
		"v1.2.3", "v0.0.0", "v10.20.30",
		"v1.2.3-rc.1", "v1.2.3-0", "v1.2.3-alpha-1", "v1.2.3-0a", "v1.2.3--",
		"v1.2.3-1.2.3", "v1.2.3-x.7.z.92",
		"v1.2.3+build.5", "v1.2.3+001", "v1.2.3-rc.1+build.5",
	}
	for _, tag := range valid {
		t.Run("valid "+tag, func(t *testing.T) {
			t.Parallel()
			ver := strings.TrimPrefix(tag, "v")
			published := []string{ver}
			if !strings.ContainsAny(ver, "-+") { // strict vX.Y.Z also publishes :X.Y and :X
				parts := strings.Split(ver, ".")
				published = append(published, parts[0]+"."+parts[1], parts[0])
			}
			got := run(t, tag, published...)
			check(t, tag+" is semver: :"+strings.TrimPrefix(tag, "v")+" is verified", got, 0, "")
			if strings.Contains(got.out, "is not valid semver") {
				t.Errorf("%s was treated as non-semver:\n%s", tag, got.out)
			}
		})
	}

	invalid := []string{
		"v1.2.3-01",       // leading zero in a numeric prerelease id
		"v1.2.3-rc.01",    // same, second identifier
		"v1.2.3-00",       // same, all zeros
		"v1.2.3-rc..1",    // empty prerelease identifier in the middle
		"v1.2.3-.rc",      // empty leading prerelease identifier
		"v1.2.3-rc.",      // empty trailing prerelease identifier
		"v1.2.3-",         // empty prerelease
		"v1.2.3+",         // empty build
		"v1.2.3+build..5", // empty build identifier
		"v1.2.3+.5",       // empty leading build identifier
		"v1.2.3+5.",       // empty trailing build identifier
		"v1.2.3x",         // trailing characters
		"v1.2.3-rc.1_x",   // character outside the identifier alphabet
		"v1.2.3+b+c",      // second build separator
		"v1.2",            // missing patch
		"v01.2.3",         // leading zero in the core
		"v1.02.3",
		"v1.2.03",
	}
	for _, tag := range invalid {
		t.Run("invalid "+tag, func(t *testing.T) {
			t.Parallel()
			got := run(t, tag, "sha-"+fullSHA)
			check(t, tag+" is not semver: only the sha tag is verified", got, 0, "::notice::"+tag+" is not valid semver")
		})
	}
}
