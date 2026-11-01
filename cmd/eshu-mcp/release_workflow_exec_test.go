package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Behavior tests for the shell inside the release and auto-tag workflows
// (Refs eshu-hq/eshu-mcp#40). Each test extracts a step's `run:` script from
// the workflow file and executes it under bash against a real temporary git
// repo and stub gh/docker/sleep binaries, so a mutated condition, message or
// exit code in the script fails here rather than only a substring check.

// stepScripts returns the dedented run: script of every step whose name
// contains name.
func stepScripts(t *testing.T, body, name string) []string {
	t.Helper()
	lines := strings.Split(body, "\n")
	indentOf := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }
	var out []string
	for i, line := range lines {
		tl := strings.TrimLeft(line, " ")
		if !strings.HasPrefix(tl, "- name: ") || !strings.Contains(tl, name) {
			continue
		}
		stepIndent := indentOf(line)
		for j := i + 1; j < len(lines); j++ {
			l := lines[j]
			if strings.TrimSpace(l) == "" {
				continue
			}
			if indentOf(l) <= stepIndent {
				break
			}
			if strings.TrimSpace(l) != "run: |" {
				continue
			}
			var script []string
			for _, sl := range lines[j+1:] {
				if strings.TrimSpace(sl) == "" {
					script = append(script, "")
					continue
				}
				if indentOf(sl) <= indentOf(l) {
					break
				}
				script = append(script, sl[indentOf(l)+2:])
			}
			text := strings.Join(script, "\n") + "\n"
			if strings.Contains(text, "${{") {
				t.Fatalf("step %q script interpolates ${{ }} directly: pass it through env", name)
			}
			out = append(out, text)
			break
		}
	}
	if len(out) == 0 {
		t.Fatalf("no run: script found for step %q", name)
	}
	return out
}

const stubGH = `#!/usr/bin/env bash
echo "gh $*" >> "$STUB_LOG"
case "$1" in
api)
  [ "$2" = "repos/o/r/releases/tags/$STUB_TAG" ] || { echo "gh: unexpected path $2" >&2; exit 2; }
  case "${STUB_RELEASE:-404}" in
    exists) echo "https://github.com/o/r/releases/tag/t"; exit 0 ;;
    404) echo "gh: Not Found (HTTP 404)" >&2; exit 1 ;;
    *) echo "gh: Internal Server Error (HTTP 500)" >&2; exit 1 ;;
  esac ;;
release)
  if [ "$5" = body ]; then printf '%s\n' "$STUB_BODY"; else printf '%s\n' "$STUB_ASSETS"; fi ;;
run)
  filter='.'; expr='.'
  while [ $# -gt 0 ]; do
    case "$1" in
      --branch) filter="$filter | map(select(.headBranch == \"$2\"))" ;;
      --event) filter="$filter | map(select(.event == \"$2\"))" ;;
      --jq) expr="$2" ;;
    esac
    shift
  done
  runs="${STUB_RUNS:-[]}"; [ -f "$STUB_LOG.runs" ] && runs="$(cat "$STUB_LOG.runs")"
  printf '%s' "$runs" | jq -r "$filter | $expr" ;;
workflow)
  [ "${STUB_DISPATCH_RC:-0}" = 0 ] && [ -n "${STUB_RUNS_AFTER:-}" ] && printf '%s' "$STUB_RUNS_AFTER" > "$STUB_LOG.runs"
  exit "${STUB_DISPATCH_RC:-0}" ;;
esac
`

const stubDocker = `#!/usr/bin/env bash
ref="${@: -1}"
f="$STUB_REGISTRY/$(printf '%s' "$ref" | tr '/:' '__')"
[ -f "$f" ] || { echo "ERROR: $ref: not found" >&2; exit 1; }
cat "$f"
`

// sandbox is a work clone with an origin remote, plus stub binaries.
type sandbox struct {
	t             *testing.T
	root, work    string
	bin, registry string
	gitEnv        []string
}

func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	root := t.TempDir()
	s := &sandbox{
		t: t, root: root, work: filepath.Join(root, "work"),
		bin: filepath.Join(root, "bin"), registry: filepath.Join(root, "registry"),
	}
	for _, d := range []string{s.bin, s.registry} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"gh": stubGH, "docker": stubDocker, "sleep": "#!/bin/sh\necho sleep >> \"$STUB_LOG\"\n"} {
		if err := os.WriteFile(filepath.Join(s.bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.gitEnv = []string{
		"HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "PATH=/usr/bin:/bin",
	}
	s.run(root, "git", "init", "-q", "--bare", "-b", "main", "origin.git")
	s.run(root, "git", "clone", "-q", "origin.git", "work")
	s.run(s.work, "git", "checkout", "-q", "-b", "main")
	return s
}

func (s *sandbox) run(dir, name string, args ...string) string {
	s.t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Env = dir, s.gitEnv
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit adds an empty commit on the current branch, pushes it, and
// returns its sha.
func (s *sandbox) commit(branch string) string {
	s.run(s.work, "git", "commit", "-q", "--allow-empty", "-m", "c")
	s.run(s.work, "git", "push", "-q", "origin", branch)
	return s.run(s.work, "git", "rev-parse", "HEAD")
}

func (s *sandbox) tag(name, sha string) {
	s.run(s.work, "git", "tag", name, sha)
	s.run(s.work, "git", "push", "-q", "origin", name)
}

// remoteOnlyTag creates a tag on origin that the work clone has never
// fetched, on a commit that is not reachable from main (so a plain fetch of
// main cannot auto-follow it).
func (s *sandbox) remoteOnlyTag(name string) {
	other := filepath.Join(s.root, "other")
	s.run(s.root, "git", "clone", "-q", "origin.git", other)
	s.run(other, "git", "checkout", "-q", "-b", "elsewhere")
	s.run(other, "git", "commit", "-q", "--allow-empty", "-m", "x")
	s.run(other, "git", "tag", name)
	s.run(other, "git", "push", "-q", "origin", name)
}

// seed builds main with the given tags, one commit per tag, and returns the
// sha of each tag.
func (s *sandbox) seed(tags ...string) map[string]string {
	shas := map[string]string{}
	for _, tg := range tags {
		shas[tg] = s.commit("main")
		s.tag(tg, shas[tg])
	}
	return shas
}

type result struct {
	out string
	rc  int
	log string
}

func (s *sandbox) exec(script string, env ...string) result {
	s.t.Helper()
	logPath := filepath.Join(s.root, "stub.log")
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = s.work
	cmd.Env = append([]string{
		"PATH=" + s.bin + ":/usr/bin:/bin", "HOME=" + s.root, "GIT_CONFIG_NOSYSTEM=1",
		"STUB_LOG=" + logPath, "STUB_REGISTRY=" + s.registry, "GH_REPO=o/r", "GH_TOKEN=x",
	}, env...)
	out, err := cmd.CombinedOutput()
	rc := 0
	if ee, ok := err.(*exec.ExitError); ok {
		rc = ee.ExitCode()
	} else if err != nil {
		s.t.Fatalf("run script: %v", err)
	}
	logged, err := os.ReadFile(logPath)
	if err != nil && !os.IsNotExist(err) {
		s.t.Fatalf("read stub log: %v", err)
	}
	return result{out: string(out), rc: rc, log: string(logged)}
}

// check asserts the exit status and that the output carries want.
func check(t *testing.T, name string, got result, wantRC int, want string) {
	t.Helper()
	if (wantRC == 0) != (got.rc == 0) {
		t.Errorf("%s: rc=%d, want rc %s0\n%s", name, got.rc, map[bool]string{true: "=", false: "!="}[wantRC == 0], got.out)
	}
	if want != "" && !strings.Contains(got.out, want) {
		t.Errorf("%s: output lacks %q:\n%s", name, want, got.out)
	}
}

func relBody(t *testing.T) string { return readWorkflow(t, "release.yml") }

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

func runsJSON(sha, branch, event string, created time.Time) string {
	return fmt.Sprintf(`[{"databaseId":1,"headSha":%q,"headBranch":%q,"event":%q,"createdAt":%q}]`,
		sha, branch, event, created.UTC().Format("2006-01-02T15:04:05Z"))
}

// TestAutoTagDispatchScriptRuns executes the dispatch step: ref form,
// duplicate-run skip, dispatch failure, and the loud poll timeout.
func TestAutoTagDispatchScriptRuns(t *testing.T) {
	t.Parallel()
	script := stepScripts(t, readWorkflow(t, "auto-tag.yml"), "Dispatch release for the new tag")[0]
	now := time.Now()
	setup := func() (*sandbox, string) {
		s := newSandbox(t)
		return s, s.seed("v0.2.6")["v0.2.6"]
	}
	// env: runs before the dispatch, and the runs visible once it has been
	// accepted (the dispatched run appearing, or not).
	env := func(before, after string, extra ...string) []string {
		return append([]string{"NEXT_TAG=v0.2.6", "STUB_RUNS=" + before, "STUB_RUNS_AFTER=" + after}, extra...)
	}
	const none = "[]"

	s, sha := setup()
	dispatchedRun := runsJSON(sha, "v0.2.6", "workflow_dispatch", now)
	got := s.exec(script, env(none, dispatchedRun)...)
	check(t, "dispatch then run appears", got, 0, "is queued or running")
	if !strings.Contains(got.log, "gh workflow run release.yml --ref refs/tags/v0.2.6\n") {
		t.Errorf("dispatch did not target the fully qualified tag ref:\n%s", got.log)
	}

	s, sha = setup()
	got = s.exec(script, env(runsJSON(sha, "v0.2.6", "push", now.Add(-time.Hour)), none)...)
	check(t, "existing run for the tag skips dispatch", got, 0, "already exists")
	if strings.Contains(got.log, "gh workflow run") {
		t.Error("dispatched although a release run for the tag already existed")
	}

	s, sha = setup()
	// Created just now with the tag commit's sha: exactly what the merge
	// commit's own push-to-main run looks like to the poll.
	mainRun := runsJSON(sha, "main", "push", now)
	got = s.exec(script, env(mainRun, mainRun)...)
	check(t, "a main-push run neither skips the dispatch nor satisfies the poll", got, 1, "no release run appeared")
	if !strings.Contains(got.log, "gh workflow run release.yml") {
		t.Error("the merge commit's main-push run was mistaken for the tag's release run: no dispatch happened")
	}

	s, _ = setup()
	got = s.exec(script, env(none, dispatchedRun, "STUB_DISPATCH_RC=1")...)
	check(t, "failed dispatch fails the job", got, 1, "")
	if strings.Contains(got.out, "queued or running") || strings.Contains(got.out, "dispatched release.yml") {
		t.Error("failed dispatch was reported as success")
	}

	s, _ = setup()
	got = s.exec(script, env(none, runsJSON("deadbeef", "v0.2.6", "workflow_dispatch", now))...)
	check(t, "run for another commit", got, 1, "::error::v0.2.6 is tagged but no release run appeared")
	if n := strings.Count(got.log, "sleep"); n != 12 {
		t.Errorf("poll slept %d times, want 12 (bounded loop)", n)
	}

	s, sha = setup()
	got = s.exec(script, env(none, runsJSON(sha, "v0.2.6", "workflow_dispatch", now.Add(-time.Hour)))...)
	check(t, "stale run (created before the dispatch)", got, 1, "no release run appeared")
}
