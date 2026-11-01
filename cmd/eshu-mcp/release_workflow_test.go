package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Guard tests for the auto-tag -> release hand-off (Refs
// eshu-hq/eshu-mcp#40). A tag pushed with the default GITHUB_TOKEN does not
// start the `push: tags` release run, so auto-tag must dispatch release.yml
// on the new tag, and release.yml must treat a dispatch on a tag ref exactly
// like the tag push while a dispatch on a branch stays a local-build dry run.

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}

// ifAfter returns the expression of the first `if:` line that follows the
// first line containing anchor, so each guard reads the condition of one
// named job or step.
func ifAfter(t *testing.T, body, anchor string) string {
	t.Helper()
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if !strings.Contains(line, anchor) {
			continue
		}
		for _, next := range lines[i+1:] {
			trimmed := strings.TrimSpace(next)
			if strings.HasPrefix(trimmed, "if:") {
				return strings.TrimSpace(strings.TrimPrefix(trimmed, "if:"))
			}
		}
	}
	t.Fatalf("no if: condition found after %q", anchor)
	return ""
}

// evalCondition evaluates the small GitHub expression subset the release
// conditions use (string literals, context names, ==, !=, &&, ||, parens)
// so the guards test behavior per event rather than pinning spelling.
func evalCondition(t *testing.T, expr string, ctx map[string]string) bool {
	t.Helper()
	var toks []string
	for i := 0; i < len(expr); {
		switch c := expr[i]; {
		case c == ' ':
			i++
		case c == '(' || c == ')':
			toks = append(toks, string(c))
			i++
		case c == '\'':
			end := strings.IndexByte(expr[i+1:], '\'')
			if end < 0 {
				t.Fatalf("unterminated string in %q", expr)
			}
			toks = append(toks, expr[i:i+end+2])
			i += end + 2
		case strings.HasPrefix(expr[i:], "&&"), strings.HasPrefix(expr[i:], "||"),
			strings.HasPrefix(expr[i:], "=="), strings.HasPrefix(expr[i:], "!="):
			toks = append(toks, expr[i:i+2])
			i += 2
		default:
			j := i
			for j < len(expr) && strings.IndexByte(" ()'&|=!", expr[j]) < 0 {
				j++
			}
			if j == i {
				t.Fatalf("cannot tokenize %q at %d", expr, i)
			}
			toks = append(toks, expr[i:j])
			i = j
		}
	}
	pos := 0
	truthy := func(v string) bool { return v != "" && v != "false" }
	var parseOr func() string
	primary := func() string {
		if pos >= len(toks) {
			t.Fatalf("unexpected end of %q", expr)
		}
		tok := toks[pos]
		pos++
		switch {
		case tok == "(":
			v := parseOr()
			pos++ // ")"
			return v
		case strings.HasPrefix(tok, "'"):
			return strings.Trim(tok, "'")
		default:
			v, ok := ctx[tok]
			if !ok {
				t.Fatalf("condition %q uses unmodeled context %q", expr, tok)
			}
			return v
		}
	}
	equality := func() string {
		l := primary()
		if pos < len(toks) && (toks[pos] == "==" || toks[pos] == "!=") {
			op := toks[pos]
			pos++
			r := primary()
			return fmt.Sprint((l == r) == (op == "=="))
		}
		return l
	}
	and := func() string {
		l := equality()
		for pos < len(toks) && toks[pos] == "&&" {
			pos++
			r := equality()
			if truthy(l) {
				l = r
			}
		}
		return l
	}
	parseOr = func() string {
		l := and()
		for pos < len(toks) && toks[pos] == "||" {
			pos++
			r := and()
			if !truthy(l) {
				l = r
			}
		}
		return l
	}
	got := parseOr()
	if pos != len(toks) {
		t.Fatalf("trailing tokens in %q", expr)
	}
	return truthy(got)
}

func refCtx(event, refType, ref string) map[string]string {
	return map[string]string{
		"github.event_name": event,
		"github.ref_type":   refType,
		"github.ref":        ref,
	}
}

// TestReleasePublishPathAdmitsTagDispatchOnly pins the publish matrix for
// every gate on the publish path (Trivy step, push job, github-release job).
func TestReleasePublishPathAdmitsTagDispatchOnly(t *testing.T) {
	t.Parallel()

	body := readWorkflow(t, "release.yml")
	cases := []struct {
		name                     string
		event, refType, ref      string
		wantPublish, wantRelease bool
	}{
		{"tag push", "push", "tag", "refs/tags/v0.2.6", true, true},
		{"main push", "push", "branch", "refs/heads/main", true, false},
		{"tag dispatch", "workflow_dispatch", "tag", "refs/tags/v0.2.6", true, true},
		{"main dispatch", "workflow_dispatch", "branch", "refs/heads/main", false, false},
		{"feature dispatch", "workflow_dispatch", "branch", "refs/heads/drive/x", false, false},
	}
	gates := map[string]string{
		"Trivy step":  "name: Scan local image for HIGH/CRITICAL CVEs",
		"push job":    "needs: [build-and-scan]",
		"release job": "needs: [push]",
	}
	for _, tc := range cases {
		ctx := refCtx(tc.event, tc.refType, tc.ref)
		for gate, anchor := range gates {
			want := tc.wantPublish
			if gate == "release job" {
				want = tc.wantRelease
			}
			if got := evalCondition(t, ifAfter(t, body, anchor), ctx); got != want {
				t.Errorf("%s: %s admits=%v, want %v", tc.name, gate, got, want)
			}
		}
	}
}

// TestReleaseTagDispatchStampsAndSerializes pins the stamp and the
// concurrency key: a dispatched tag run stamps the tag, rejects non-version
// tags, and shares the tag push's group so the two runs queue, never overlap.
func TestReleaseTagDispatchStampsAndSerializes(t *testing.T) {
	t.Parallel()

	body := readWorkflow(t, "release.yml")
	if got := strings.Count(body, "github.ref_type == 'tag' && github.ref_name || github.sha"); got < 3 {
		t.Errorf("version stamp expression appears %d times, want >=3 (two validations + build arg)", got)
	}
	if !strings.Contains(body, "group: image-publish-${{ github.ref }}") {
		t.Error("concurrency group no longer keyed on github.ref: tag push and tag dispatch would not serialize")
	}
	if !strings.Contains(body, "workflow_dispatch:") {
		t.Error("release.yml lost workflow_dispatch: auto-tag cannot start the release")
	}
	if got := strings.Count(body, `'^v[0-9]+\.[0-9]+\.[0-9]+$'`); got < 2 {
		t.Errorf("tag-shape check appears %d times, want it in both version-stamp validations", got)
	}
}

// TestAutoTagDispatchesReleaseForNewTag pins the hand-off: after the tag
// push, auto-tag dispatches release.yml on that tag with a token that may do
// so, at least privilege, never on the dry run, and never for a tag that
// already has a release run.
func TestAutoTagDispatchesReleaseForNewTag(t *testing.T) {
	t.Parallel()

	body := readWorkflow(t, "auto-tag.yml")
	push := strings.Index(body, `git push origin "$next"`)
	dispatch := strings.Index(body, `gh workflow run release.yml --ref "$`)
	if push < 0 || dispatch < 0 {
		t.Fatalf("push at %d, dispatch at %d: auto-tag must push then dispatch release.yml on the new tag", push, dispatch)
	}
	if dispatch < push {
		t.Error("release dispatch precedes the tag push: the ref would not exist yet")
	}
	if !strings.Contains(body, "GH_TOKEN: ${{ github.token }}") {
		t.Error("dispatch step does not authenticate gh with github.token")
	}
	if !strings.Contains(body, "gh run list --workflow release.yml") {
		t.Error("no existing-run check before dispatch: a tag push run plus a dispatch could double-publish")
	}
	header, jobs, ok := strings.Cut(body, "\njobs:")
	if !ok {
		t.Fatal("auto-tag.yml has no jobs: section")
	}
	if strings.Contains(header, "actions: write") {
		t.Error("actions: write is granted workflow-wide; scope it to the tag job")
	}
	for _, want := range []string{"actions: write", "contents: write"} {
		if !strings.Contains(jobs, want) {
			t.Errorf("tag job lacks %q: it cannot push the tag and dispatch the release", want)
		}
	}
	cond := ifAfter(t, body, "Dispatch release")
	if !strings.Contains(cond, "!= 'workflow_dispatch'") {
		t.Errorf("dispatch step condition %q does not exclude the dry run", cond)
	}
	if !strings.Contains(cond, "steps.tag.outputs.tag != ''") {
		t.Errorf("dispatch step condition %q does not require a freshly pushed tag", cond)
	}
}
