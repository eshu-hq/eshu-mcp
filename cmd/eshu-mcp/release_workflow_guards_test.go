package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Behavior tests for the release.yml guard scripts; helpers live in
// release_workflow_helpers_test.go.

// TestReleaseValidateStampScriptsRun covers both "Validate version stamp"
// scripts: a human tag push keeps the permissive check (pre-release tags
// still publish), a dispatch must name a strict vX.Y.Z tag.
func TestReleaseValidateStampScriptsRun(t *testing.T) {
	t.Parallel()
	scripts := stepScripts(t, relBody(t), "Validate version stamp")
	if len(scripts) != 2 {
		t.Fatalf("%d version-stamp scripts, want 2 (build-and-scan and push)", len(scripts))
	}
	cases := []struct {
		name, event, refType, version string
		wantRC                        int
		want                          string
	}{
		{"tag push strict", "push", "tag", "v0.2.6", 0, ""},
		{"tag push pre-release unchanged", "push", "tag", "v0.3.0-rc.1", 0, ""},
		{"tag push unsafe", "push", "tag", "v1;rm -rf", 1, "unsafe VERSION"},
		{"main push sha", "push", "branch", "0123abcd", 0, ""},
		{"dispatch strict", "workflow_dispatch", "tag", "v0.2.6", 0, ""},
		{"dispatch pre-release refused", "workflow_dispatch", "tag", "v0.3.0-rc.1", 1, "non-version tag"},
		{"dispatch non-version tag refused", "workflow_dispatch", "tag", "v1", 1, "non-version tag"},
		{"dispatch unsafe", "workflow_dispatch", "tag", "v0.2.6$(x)", 1, "unsafe VERSION"},
		{"branch dispatch sha", "workflow_dispatch", "branch", "0123abcd", 0, ""},
	}
	for i, script := range scripts {
		for _, tc := range cases {
			s := newSandbox(t)
			got := s.exec(script, "VERSION="+tc.version, "REF_TYPE="+tc.refType, "EVENT_NAME="+tc.event)
			check(t, fmt.Sprintf("script %d: %s", i, tc.name), got, tc.wantRC, tc.want)
		}
	}
}

// guardEnv is the env the replay guard receives from the workflow.
func guardEnv(tag, sha string, extra ...string) []string {
	return append([]string{"TAG=" + tag, "STUB_TAG=" + tag, "GITHUB_SHA=" + sha}, extra...)
}

// TestReleaseReplayGuardScriptRuns executes the dispatch guard against a
// real repo: Release lookup, main ancestry, and the newest-tag rule.
func TestReleaseReplayGuardScriptRuns(t *testing.T) {
	t.Parallel()
	script := stepScripts(t, relBody(t), "Refuse replayed or off-main tag dispatch")[0]

	t.Run("unreleased newest tag on main passes", func(t *testing.T) {
		s := newSandbox(t)
		sh := s.seed("v0.2.5", "v0.2.6")
		check(t, "ok", s.exec(script, guardEnv("v0.2.6", sh["v0.2.6"])...), 0, "")
	})
	t.Run("existing Release is refused", func(t *testing.T) {
		s := newSandbox(t)
		sh := s.seed("v0.2.6")
		got := s.exec(script, guardEnv("v0.2.6", sh["v0.2.6"], "STUB_RELEASE=exists")...)
		check(t, "exists", got, 1, "already exists")
	})
	t.Run("Release lookup error fails closed", func(t *testing.T) {
		s := newSandbox(t)
		sh := s.seed("v0.2.6")
		got := s.exec(script, guardEnv("v0.2.6", sh["v0.2.6"], "STUB_RELEASE=500")...)
		check(t, "500", got, 1, "cannot tell whether v0.2.6 is released")
	})
	t.Run("tag commit off main is refused", func(t *testing.T) {
		s := newSandbox(t)
		s.seed("v0.2.5")
		s.run(s.work, "git", "checkout", "-q", "-b", "side")
		side := s.commit("side")
		s.tag("v0.2.6", side)
		s.run(s.work, "git", "checkout", "-q", "main") // HEAD must not stand in for GITHUB_SHA
		got := s.exec(script, guardEnv("v0.2.6", side)...)
		check(t, "off-main", got, 1, "is not on main")
	})
	t.Run("superseded tag is refused", func(t *testing.T) {
		s := newSandbox(t)
		sh := s.seed("v0.2.5", "v0.2.6")
		got := s.exec(script, guardEnv("v0.2.5", sh["v0.2.5"])...)
		check(t, "superseded", got, 1, "not the newest tag (v0.2.6)")
	})
	t.Run("newer tag only on origin is seen (tags are fetched)", func(t *testing.T) {
		s := newSandbox(t)
		sh := s.seed("v0.2.5")
		s.remoteOnlyTag("v0.2.6")
		got := s.exec(script, guardEnv("v0.2.5", sh["v0.2.5"])...)
		check(t, "remote-only newer tag", got, 1, "not the newest tag (v0.2.6)")
	})
	t.Run("version order is numeric", func(t *testing.T) {
		s := newSandbox(t)
		sh := s.seed("v0.2.9", "v0.2.10")
		check(t, "newest 10", s.exec(script, guardEnv("v0.2.10", sh["v0.2.10"])...), 0, "")
		got := s.exec(script, guardEnv("v0.2.9", sh["v0.2.9"])...)
		check(t, "9 superseded", got, 1, "not the newest tag (v0.2.10)")
	})
	t.Run("pre-release tags do not count as newest", func(t *testing.T) {
		s := newSandbox(t)
		sh := s.seed("v0.2.6", "v0.3.0-rc1")
		check(t, "rc ignored", s.exec(script, guardEnv("v0.2.6", sh["v0.2.6"])...), 0, "")
	})
	t.Run("no strict tag at all is refused with the message", func(t *testing.T) {
		s := newSandbox(t)
		sh := s.seed("v0.3.0-rc1")
		got := s.exec(script, guardEnv("v0.3.0-rc1", sh["v0.3.0-rc1"])...)
		check(t, "no strict", got, 1, "no vX.Y.Z tag found")
	})
	t.Run("unreadable remote fails closed", func(t *testing.T) {
		s := newSandbox(t)
		sh := s.seed("v0.2.6")
		s.run(s.work, "git", "remote", "set-url", "origin", filepath.Join(s.root, "gone.git"))
		check(t, "no remote", s.exec(script, guardEnv("v0.2.6", sh["v0.2.6"])...), 1, "")
	})
}

// TestReleaseRecheckScriptRuns executes the push-job re-check: newest tag
// only, fail closed on a stale tag or an empty tag list.
func TestReleaseRecheckScriptRuns(t *testing.T) {
	t.Parallel()
	script := stepScripts(t, relBody(t), "Re-check newest tag before publishing")[0]

	s := newSandbox(t)
	s.seed("v0.2.5", "v0.2.6")
	check(t, "newest", s.exec(script, "TAG=v0.2.6"), 0, "")
	check(t, "superseded", s.exec(script, "TAG=v0.2.5"), 1, "no longer the newest tag (v0.2.6)")

	rc := newSandbox(t)
	rc.seed("v0.3.0-rc1")
	check(t, "no strict tag", rc.exec(script, "TAG=v0.3.0-rc1"), 1, "no longer the newest tag (none)")

	gone := newSandbox(t)
	gone.seed("v0.2.6")
	gone.run(gone.work, "git", "remote", "set-url", "origin", filepath.Join(gone.root, "gone.git"))
	check(t, "unreadable remote", gone.exec(script, "TAG=v0.2.6"), 1, "")

	fresh := newSandbox(t)
	fresh.seed("v0.2.5")
	fresh.remoteOnlyTag("v0.2.6")
	check(t, "newer tag only on origin", fresh.exec(script, "TAG=v0.2.5"), 1, "no longer the newest tag (v0.2.6)")
}

func sha(content string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content))) }

// TestReleaseVerifyPublishedScriptRuns executes verify-published against a
// stub registry and stub gh.
func TestReleaseVerifyPublishedScriptRuns(t *testing.T) {
	t.Parallel()
	script := stepScripts(t, relBody(t), "Verify the tag's image and Release exist")[0]
	const image = "ghcr.io/eshu-hq/eshu-mcp"
	manifest := `{"digest":"pushed"}`
	digest := sha(manifest)
	setup := func(tag string, tags map[string]string) (*sandbox, []string) {
		s := newSandbox(t)
		for name, content := range tags {
			key := strings.NewReplacer("/", "_", ":", "_").Replace(image + ":" + name)
			if err := os.WriteFile(filepath.Join(s.registry, key), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return s, []string{
			"TAG=" + tag, "DIGEST=" + digest, "IMAGE=" + image,
			"STUB_BODY=Image: " + image + "@" + digest, "STUB_ASSETS=other.txt\neshu-mcp-image-spdx.json",
		}
	}
	all := map[string]string{"0.2.6": manifest, "0.2": manifest, "0": manifest}

	s, env := setup("v0.2.6", all)
	check(t, "happy", s.exec(script, env...), 0, "")

	s, env = setup("v0.2.6", map[string]string{"v0.2.6": manifest, "0.2": manifest, "0": manifest})
	check(t, "only the v-prefixed tag exists", s.exec(script, env...), 1, "")

	s, env = setup("v0.2.6", map[string]string{"0.2.6": `{"digest":"other"}`, "0.2": manifest, "0": manifest})
	check(t, "X.Y.Z digest mismatch", s.exec(script, env...), 1, "want "+digest)

	s, env = setup("v0.2.6", map[string]string{"0.2.6": manifest, "0": manifest})
	check(t, "X.Y does not resolve", s.exec(script, env...), 1, "")

	s, env = setup("v0.2.6", map[string]string{"0.2.6": manifest, "0.2": manifest})
	check(t, "X does not resolve", s.exec(script, env...), 1, "")

	s, env = setup("v0.2.6", map[string]string{"0.2.6": manifest, "0.2": `{"digest":"newer"}`, "0": `{"digest":"newer"}`})
	check(t, "moving tags only need to resolve", s.exec(script, env...), 0, "")

	s, env = setup("v0.2.6", all)
	check(t, "Release body lacks digest", s.exec(script, append(env, "STUB_BODY=no digest here")...), 1, "does not record")

	s, env = setup("v0.2.6", all)
	check(t, "SBOM asset missing", s.exec(script, append(env, "STUB_ASSETS=other.txt")...), 1, "::error::")

	s, env = setup("v0.3.0-rc.1", map[string]string{"0.3.0-rc.1": manifest})
	got := s.exec(script, env...)
	check(t, "pre-release skips moving tags", got, 0, "::notice::")

	s, env = setup("v0.3.0-rc.1", map[string]string{"0.3.0-rc.1": `{"digest":"other"}`})
	check(t, "pre-release digest still checked", s.exec(script, env...), 1, "want "+digest)
}
