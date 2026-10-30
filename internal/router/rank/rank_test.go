// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	registry "github.com/eshu-hq/eshu-mcp"
	"github.com/eshu-hq/eshu-mcp/internal/router"
)

// def builds a tool definition whose input schema lists props as optional
// string properties.
func def(name, desc string, props ...string) registry.ToolDefinition {
	properties := map[string]any{}
	for _, p := range props {
		properties[p] = map[string]any{"type": "string"}
	}
	return registry.ToolDefinition{
		Name:        name,
		Description: desc,
		InputSchema: map[string]any{"type": "object", "properties": properties},
	}
}

func allowAll(string) bool { return true }

func names(cs []router.Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rankWith builds a fresh catalog-free ranker over tools and ranks one query
// with every tool allowed.
func rankWith(t *testing.T, tools []registry.ToolDefinition, q router.Query) []router.Candidate {
	t.Helper()
	got, err := New(tools).Rank(context.Background(), q, tools, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	return got
}

// TestNewRanksCatalogFreeWithHighDefault pins the #9578 acceptance shape for
// the port: the catalog-free build ranks with no entries, every candidate
// carries the conservative CostHigh default, and Rank reuses the New index
// with no rebuild.
func TestNewRanksCatalogFreeWithHighDefault(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("alpha_one", "Handles zebra processing."),
		def("alpha_two", "Handles other processing."),
	}
	q := router.Query{Intent: "zebra", Limit: 5}

	r := New(tools)
	if got := r.builds.Load(); got != 1 {
		t.Fatalf("builds after New = %d, want 1", got)
	}
	got, err := r.Rank(context.Background(), q, tools, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if len(got) == 0 || got[0].Name != "alpha_one" {
		t.Fatalf("top = %v, want alpha_one first", names(got))
	}
	for _, c := range got {
		if c.Cost != CostHigh {
			t.Fatalf("candidate %q Cost = %q, want %q (catalog-free default)", c.Name, c.Cost, CostHigh)
		}
	}
	if got := r.builds.Load(); got != 1 {
		t.Fatalf("builds after Rank = %d, want 1 (index must be reused)", got)
	}
	again, err := r.Rank(context.Background(), q, tools, allowAll)
	if err != nil {
		t.Fatalf("second Rank: %v", err)
	}
	if !reflect.DeepEqual(got, again) {
		t.Fatalf("second Rank = %v, want identical to the first", names(again))
	}
}

// TestRankSameInputsReuseIndex pins the other side of the cache: identical
// inputs keep using the one index.
func TestRankSameInputsReuseIndex(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("alpha", "Locates the zebra."),
		def("beta", "Locates the zebra."),
	}
	r := New(tools)
	for range 5 {
		if _, err := r.Rank(context.Background(), router.Query{Intent: "zebra", Limit: 5}, tools, allowAll); err != nil {
			t.Fatalf("Rank: %v", err)
		}
	}
	if got := r.builds.Load(); got != 1 {
		t.Fatalf("builds = %d, want 1", got)
	}
}

// TestRankNewSliceRebuilds proves a different tools slice is never answered
// from a stale index, even when its names look the same.
func TestRankNewSliceRebuilds(t *testing.T) {
	old := []registry.ToolDefinition{def("alpha", "Locates the zebra."), def("beta", "Locates the horse.")}
	r := New(old)
	changed := []registry.ToolDefinition{def("alpha", "Locates the horse."), def("beta", "Locates the zebra.")}
	got, err := r.Rank(context.Background(), router.Query{Intent: "zebra", Limit: 5}, changed, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if len(got) == 0 || got[0].Name != "beta" {
		t.Fatalf("top = %v, want beta (a different slice must rebuild, not serve stale text)", names(got))
	}
	if got := r.builds.Load(); got != 2 {
		t.Fatalf("builds = %d, want 2", got)
	}
}

// TestRankNilAllowedFailsClosed proves Rank with no visibility rule fails
// closed instead of inventing an order.
func TestRankNilAllowedFailsClosed(t *testing.T) {
	tools := []registry.ToolDefinition{def("alpha_one", "Handles zebra processing.")}
	got, err := New(tools).Rank(context.Background(), router.Query{Intent: "zebra", Limit: 5}, tools, nil)
	if err == nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want an error and no candidates for a nil allowed func", names(got), err)
	}
}

// TestRankDeterministicOverFreshBuilds runs one query on fresh rankers (a
// fresh build re-randomizes Go map iteration) and requires one identical
// ordered result including scores and Why text.
func TestRankDeterministicOverFreshBuilds(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("find_symbol", "Locate a function or type definition by its name in indexed source code.", "repo_id", "name"),
		def("list_repositories", "Enumerate every repository known to the graph.", "limit"),
		def("get_service_context", "Return the deployment story for one service.", "service_id", "environment"),
		def("trace_deployment_chain", "Follow a workload from source repository to running environment.", "workload_id", "repo_id"),
		def("search_content", "Full text search across file bodies.", "query", "repo_id"),
		def("get_dead_code", "Report symbols with no incoming references.", "repo_id"),
	}
	queries := []router.Query{
		{Intent: "show deployment story for a service", Limit: 5},
		{Intent: "find the dead code in a repository", Limit: 5, Anchors: router.Anchors{RepoID: "r-1"}},
		{Intent: "zebra", Limit: 5},
	}
	for _, q := range queries {
		var first []router.Candidate
		for run := range 25 {
			got := rankWith(t, tools, q)
			if run == 0 {
				first = got
				continue
			}
			if len(got) != len(first) {
				t.Fatalf("query %q run %d: %d candidates, want %d", q.Intent, run, len(got), len(first))
			}
			for i := range got {
				if got[i].Name != first[i].Name || got[i].Score != first[i].Score || got[i].Why != first[i].Why ||
					fmt.Sprint(got[i].Example) != fmt.Sprint(first[i].Example) {
					t.Fatalf("query %q run %d position %d: %+v, want %+v", q.Intent, run, i, got[i], first[i])
				}
			}
		}
	}
}

// TestRankScoreMatchesWorkedBM25Example pins the exact score for a two-tool
// registry whose inputs are derived by hand, not read from the code:
//
//	alpha_one: tokens alpha,one (name, weight 2 each = 4) + handle,zebra (desc, 1 each = 2)
//	           -> length 6, tf(zebra) = 1
//	alpha_two: name 4 + handle,zebra,widely,used,here = 5 -> length 9
//	average length = 7.5; N = 2, df(zebra) = 2
//	idf = ln(1 + (2 - 2 + 0.5) / (2 + 0.5)) = ln(1.2)
//	score(alpha_one) = idf * tf * (k1+1) / (tf + k1 * (1 - b + b * 6/7.5))
//	with k1 = 1.2 and b = 0.75.
func TestRankScoreMatchesWorkedBM25Example(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("alpha_one", "Handles zebra."),
		def("alpha_two", "Handles zebra widely used here."),
	}
	got := rankWith(t, tools, router.Query{Intent: "zebra", Limit: 5})
	byName := map[string]float64{}
	for _, c := range got {
		byName[c.Name] = c.Score
	}
	idf := math.Log(1.2)
	wantOne := idf * 1 * 2.2 / (1 + 1.2*(1-0.75+0.75*6/7.5))
	wantTwo := idf * 1 * 2.2 / (1 + 1.2*(1-0.75+0.75*9/7.5))
	if math.Abs(byName["alpha_one"]-wantOne) > 1e-9 || math.Abs(byName["alpha_two"]-wantTwo) > 1e-9 {
		t.Fatalf("scores = %v, want alpha_one %.12f alpha_two %.12f", byName, wantOne, wantTwo)
	}
}

// TestRankTieBreakFallsToName proves the catalog-free tie order: every tool
// keeps CostHigh and no Canonical mark, so equal scores order by name.
func TestRankTieBreakFallsToName(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("beta_d", "Locates the zebra."),
		def("beta_c", "Locates the zebra."),
		def("beta_b", "Locates the zebra."),
		def("beta_a", "Locates the zebra."),
	}
	got := rankWith(t, tools, router.Query{Intent: "zebra", Limit: 10})
	if want := []string{"beta_a", "beta_b", "beta_c", "beta_d"}; !equalStrings(names(got), want) {
		t.Fatalf("order = %v, want %v", names(got), want)
	}
	if got[0].Score != got[3].Score {
		t.Fatalf("scores differ (%v vs %v); fixture is not a true tie", got[0].Score, got[3].Score)
	}
}

// TestRankMatchesNameTokens proves a name-token match wins without any
// curated intent.
func TestRankMatchesNameTokens(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("find_symbol", "Locate a function or type definition by its name in indexed source code."),
		def("get_dead_code", "Report symbols with no incoming references."),
	}
	got := rankWith(t, tools, router.Query{Intent: "dead code", Limit: 3})
	if len(got) == 0 || got[0].Name != "get_dead_code" {
		t.Fatalf("top = %v, want get_dead_code first", names(got))
	}
}

// TestRankAnchorsPreferButDoNotCreate proves anchors reorder near-ties toward
// tools that accept them, and never create a match on their own.
func TestRankAnchorsPreferButDoNotCreate(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("gamma_a", "Locates the zebra.", "name"),
		def("gamma_b", "Locates the zebra.", "repo_id"),
		def("gamma_c", "Locates the zebra.", "environment"),
	}
	base := router.Query{Intent: "zebra", Limit: 5}
	if got := rankWith(t, tools, base); !equalStrings(names(got), []string{"gamma_a", "gamma_b", "gamma_c"}) {
		t.Fatalf("no anchors: order = %v, want name order", names(got))
	}
	withRepo := base
	withRepo.Anchors.RepoID = "repo-1"
	if got := rankWith(t, tools, withRepo); got[0].Name != "gamma_b" {
		t.Fatalf("repo_id anchor: order = %v, want gamma_b first", names(got))
	}
	withEnv := base
	withEnv.Anchors.Environment = "prod"
	if got := rankWith(t, tools, withEnv); got[0].Name != "gamma_c" {
		t.Fatalf("environment anchor: order = %v, want gamma_c first", names(got))
	}

	anchored := router.Query{Intent: "quasar", Limit: 5}
	anchored.Anchors.RepoID = "repo-1"
	if got := rankWith(t, tools[:1], anchored); len(got) != 0 {
		t.Fatalf("got %v, want none: anchors only prefer, they never add a lexical match", names(got))
	}
}

// TestRankEmptyStopwordsAndNoMatch proves a blank intent, an all-stopword
// intent, and an intent with no match return no candidates and no error.
func TestRankEmptyStopwordsAndNoMatch(t *testing.T) {
	tools := []registry.ToolDefinition{def("alpha_one", "Handles zebra processing.")}
	for _, intent := range []string{"", "   ", "the of and", "xylophone quasar"} {
		got := rankWith(t, tools, router.Query{Intent: intent, Limit: 5})
		if len(got) != 0 {
			t.Fatalf("intent %q: got %v, want no candidates", intent, names(got))
		}
	}
}

// TestRankLimitBounds proves Limit below 1 returns nothing and Limit above
// the cap stops at MaxLimit.
func TestRankLimitBounds(t *testing.T) {
	tools := make([]registry.ToolDefinition, 0, 14)
	for i := range 14 {
		tools = append(tools, def(fmt.Sprintf("tool_%02d", i), fmt.Sprintf("Locates the zebra near area%02d.", i)))
	}
	for _, tc := range []struct {
		limit int
		want  int
	}{
		{-1, 0}, {0, 0}, {1, 1}, {3, 3}, {10, 10}, {14, router.MaxLimit}, {100, router.MaxLimit},
	} {
		got := rankWith(t, tools, router.Query{Intent: "zebra", Limit: tc.limit})
		if len(got) != tc.want {
			t.Errorf("Limit %d: got %d candidates, want %d", tc.limit, len(got), tc.want)
		}
	}
}

// TestRankAllowedFiltersBeforeRanking proves a disallowed tool never appears
// and never shifts a visible tool's score.
func TestRankAllowedFiltersBeforeRanking(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("secret_zebra", "Zebra zebra zebra internals."),
		def("public_zebra", "Zebra lookup."),
		def("public_other", "Unrelated horse lookup."),
	}
	q := router.Query{Intent: "zebra lookup", Limit: 10}
	r := New(tools)
	allowed := func(n string) bool { return n != "secret_zebra" }
	got, err := r.Rank(context.Background(), q, tools, allowed)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	for _, c := range got {
		if c.Name == "secret_zebra" {
			t.Fatalf("returned a disallowed tool: %v", names(got))
		}
	}
	want := rankWith(t, tools[1:], q)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", names(got), names(want))
	}
	for i := range got {
		if got[i].Name != want[i].Name || got[i].Score != want[i].Score {
			t.Fatalf("candidate %d = %s/%v, want %s/%v (disallowed tool shifted scores)",
				i, got[i].Name, got[i].Score, want[i].Name, want[i].Score)
		}
	}
}

// boilerplateRegistry builds n tools with unique first sentences; the first
// copies tools also carry the shared sentence.
func boilerplateRegistry(n, copies int, shared string) []registry.ToolDefinition {
	tools := make([]registry.ToolDefinition, 0, n)
	for i := range n {
		desc := fmt.Sprintf("Reads area%02d data.", i)
		if i < copies {
			desc += " " + shared
		}
		tools = append(tools, def(fmt.Sprintf("tool_%02d", i), desc))
	}
	return tools
}

// TestRankSentenceRepeatedTwiceIsKeptThreeTimesIsStripped pins the repeat
// threshold: two copies are coincidence, three are boilerplate.
func TestRankSentenceRepeatedTwiceIsKeptThreeTimesIsStripped(t *testing.T) {
	const shared = "Quasar frobnicates zebra gently."
	q := router.Query{Intent: "frobnicates", Limit: 10}
	if got := rankWith(t, boilerplateRegistry(14, 2, shared), q); len(got) != 2 {
		t.Fatalf("sentence in 2 tools: got %v, want both tools kept", names(got))
	}
	if got := rankWith(t, boilerplateRegistry(14, 3, shared), q); len(got) != 0 {
		t.Fatalf("sentence in 3 tools: got %v, want it stripped as boilerplate", names(got))
	}
}

// TestRankBoilerplateNeedsTwelveTools pins the corpus floor: an 11-tool
// registry keeps even a sentence repeated in every tool; 12 tools strip it.
func TestRankBoilerplateNeedsTwelveTools(t *testing.T) {
	const shared = "Quasar frobnicates zebra gently."
	q := router.Query{Intent: "frobnicates", Limit: 10}
	if got := rankWith(t, boilerplateRegistry(11, 11, shared), q); len(got) != 10 {
		t.Fatalf("11 tools: got %d results, want 10 (limit) because nothing is stripped", len(got))
	}
	if got := rankWith(t, boilerplateRegistry(12, 12, shared), q); len(got) != 0 {
		t.Fatalf("12 tools: got %v, want the shared sentence stripped", names(got))
	}
}

// TestRankBoundsHugeIntent proves a caller-supplied intent cannot make Rank do
// unbounded work: only the leading bytes are read and only the first
// maxQueryTerms distinct terms score.
func TestRankBoundsHugeIntent(t *testing.T) {
	tools := []registry.ToolDefinition{def("get_dead_code", "Report symbols with no incoming references.")}
	var b strings.Builder
	b.WriteString("dead code ")
	for i := 0; b.Len() < 4<<20; i++ {
		fmt.Fprintf(&b, "w%d ", i)
	}
	r := New(tools)
	start := time.Now()
	got, err := r.Rank(context.Background(), router.Query{Intent: b.String(), Limit: 3}, tools, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("Rank on a 4 MiB intent took %v, want it bounded", elapsed)
	}
	if len(got) == 0 || got[0].Name != "get_dead_code" {
		t.Fatalf("got %v, want get_dead_code first (leading words must still count)", names(got))
	}
	terms := queryTerms(b.String())
	if len(terms) != 32 || maxQueryTerms > 64 || maxIntentBytes > 8192 {
		t.Fatalf("queryTerms = %d terms (caps %d terms, %d bytes), want exactly 32 terms and sane caps",
			len(terms), maxQueryTerms, maxIntentBytes)
	}
	if terms[0] != "dead" || terms[1] != "code" {
		t.Fatalf("queryTerms = %v, want leading words first", terms[:3])
	}
}

// TestClipIntentCutsOnRuneBoundary proves the intent clip never splits a
// multibyte rune.
func TestClipIntentCutsOnRuneBoundary(t *testing.T) {
	in := "a" + strings.Repeat("é", maxIntentBytes)
	got := clipIntent(in)
	if len(got) > maxIntentBytes || !utf8.ValidString(got) {
		t.Fatalf("clipIntent = %d bytes, valid=%v; want <= %d and valid UTF-8", len(got), utf8.ValidString(got), maxIntentBytes)
	}
	if short := "dead code"; clipIntent(short) != short {
		t.Fatalf("clipIntent changed a short intent")
	}
}

// TestRankWhyIsOneLineFromMatchingText proves the Why line quotes the match
// on a single line, falling back to the tool name when only it matched.
func TestRankWhyIsOneLineFromMatchingText(t *testing.T) {
	tools := []registry.ToolDefinition{
		def("alpha_tool", "Does something unrelated.\nSecond line mentions zebra here."),
		def("zebra_tool", "Does something unrelated."),
	}
	got := rankWith(t, tools, router.Query{Intent: "zebra", Limit: 5})
	byName := map[string]router.Candidate{}
	for _, c := range got {
		byName[c.Name] = c
	}
	if why := byName["alpha_tool"].Why; !strings.Contains(why, "Second line mentions zebra here") || strings.ContainsAny(why, "\n\r") {
		t.Fatalf("alpha_tool Why = %q, want the matching description sentence on one line", why)
	}
	if why := byName["zebra_tool"].Why; !strings.Contains(why, "zebra") || strings.ContainsAny(why, "\n\r") {
		t.Fatalf("zebra_tool Why = %q, want a one-line name-based reason", why)
	}
}
