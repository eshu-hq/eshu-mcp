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
