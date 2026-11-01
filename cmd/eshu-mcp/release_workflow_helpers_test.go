package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
