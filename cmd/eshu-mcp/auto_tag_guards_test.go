package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Behavior test for the auto-tag.yml dispatch step; helpers live in
// release_workflow_helpers_test.go.

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
	// Production tags the merge commit with an annotated tag and the job's
	// HEAD is the pull_request merge ref, so HEAD is neither the tag object
	// nor the tagged commit: leave HEAD on a later commit.
	setup := func() (*sandbox, string) {
		s := newSandbox(t)
		tagged := s.commit("main")
		s.annotatedTag("v0.2.6", tagged)
		s.commit("main")
		return s, tagged
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

// TestAutoTagWorkflowPinsSettings pins settings whose mutation changes no
// script: token scopes, serialization, and the full-history checkout the
// tag computation needs.
func TestAutoTagWorkflowPinsSettings(t *testing.T) {
	t.Parallel()
	body := readWorkflow(t, "auto-tag.yml")
	for _, want := range []string{
		"\npermissions:\n  contents: read\n\nconcurrency:\n  group: auto-tag\n  cancel-in-progress: false\n",
		"          fetch-depth: 0\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("auto-tag.yml lost the pinned setting:\n%s", want)
		}
	}
	got := jobPermissions(t, body, "tag-patch")
	if strings.Join(got, "|") != "contents: write|actions: write" {
		t.Errorf("tag-patch permissions = %v, want [contents: write actions: write]", got)
	}
}
