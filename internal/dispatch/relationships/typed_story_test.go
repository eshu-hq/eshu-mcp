package relationshiptools

import (
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

func TestTypedStoryRequestShape(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{
		"target":         "pkg.Foo",
		"repo_id":        "r1",
		"language":       "go",
		"max_depth":      3,
		"min_confidence": 0.5,
	}
	req := analyzeCodeRelationshipsTypedStoryRequest(args, "overrides", "both", "OVERRIDES")
	if req.Method != "POST" || req.Path != "/api/v0/code/relationships/story" {
		t.Fatalf("request = %s %s", req.Method, req.Path)
	}
	body, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("body type %T", req.Body)
	}
	if body["query_type"] != "overrides" || body["direction"] != "both" || body["relationship_type"] != "OVERRIDES" {
		t.Fatalf("body = %#v", body)
	}
	if body["max_depth"] != 3 {
		t.Fatalf("max_depth = %#v", body["max_depth"])
	}
	if body["min_confidence"] != 0.5 {
		t.Fatalf("min_confidence = %#v", body["min_confidence"])
	}
	def := analyzeCodeRelationshipsTypedStoryRequest(routecontract.Arguments{}, "q", "d", "r")
	defBody := def.Body.(map[string]any)
	if defBody["max_depth"] != 5 || defBody["limit"] != 25 {
		t.Fatalf("defaults = %#v", defBody)
	}
	if _, ok := defBody["min_confidence"]; ok {
		t.Fatal("absent min_confidence must stay absent")
	}
}

func TestCrossRepoAndMaxDepth(t *testing.T) {
	t.Parallel()

	if !analyzeCodeRelationshipsCrossRepo(routecontract.Arguments{"cross_repo": true}, false) {
		t.Fatal("explicit cross_repo not honored")
	}
	if !analyzeCodeRelationshipsCrossRepo(routecontract.Arguments{"scope": "CROSS_repo"}, false) {
		t.Fatal("scope cross_repo not honored")
	}
	if !analyzeCodeRelationshipsCrossRepo(routecontract.Arguments{}, true) {
		t.Fatal("forced cross_repo not honored")
	}
	if analyzeCodeRelationshipsCrossRepo(routecontract.Arguments{}, false) {
		t.Fatal("default must not be cross_repo")
	}
	if got := parseCodeRelationshipMaxDepth(routecontract.Arguments{"max_depth": 2.0}, 5); got != 2 {
		t.Fatalf("float depth = %d", got)
	}
	if got := parseCodeRelationshipMaxDepth(routecontract.Arguments{"max_depth": 4}, 5); got != 4 {
		t.Fatalf("int depth = %d", got)
	}
	if got := parseCodeRelationshipMaxDepth(routecontract.Arguments{}, 5); got != 5 {
		t.Fatalf("default depth = %d", got)
	}
}

func TestResolveQueryTypeSweep(t *testing.T) {
	t.Parallel()

	story := map[string]string{
		"find_callers": "incoming", "find_callees": "outgoing",
		"find_all_callers": "incoming", "find_all_callees": "outgoing",
		"find_cross_repo_callers": "incoming", "find_cross_repo_callees": "outgoing",
		"find_importers": "incoming", "find_cross_repo_importers": "incoming",
	}
	for queryType, direction := range story {
		req, err := resolveAnalyzeCodeRelationshipsRequest(routecontract.Arguments{"query_type": queryType, "target": "T"})
		if err != nil {
			t.Fatalf("%s: %v", queryType, err)
		}
		if req.Path != "/api/v0/code/relationships/story" {
			t.Fatalf("%s path = %s", queryType, req.Path)
		}
		if body := req.Body.(map[string]any); body["direction"] != direction {
			t.Fatalf("%s direction = %#v", queryType, body["direction"])
		}
	}
	typed := map[string]string{
		"class_hierarchy": "INHERITS", "cross_repo_class_hierarchy": "INHERITS",
		"overrides": "OVERRIDES", "cross_repo_overrides": "OVERRIDES",
	}
	for queryType, rel := range typed {
		req, err := resolveAnalyzeCodeRelationshipsRequest(routecontract.Arguments{"query_type": queryType})
		if err != nil {
			t.Fatalf("%s: %v", queryType, err)
		}
		if body := req.Body.(map[string]any); body["relationship_type"] != rel {
			t.Fatalf("%s rel = %#v", queryType, body["relationship_type"])
		}
	}
	dead, err := resolveAnalyzeCodeRelationshipsRequest(routecontract.Arguments{"query_type": "dead_code", "repo_id": "r"})
	if err != nil {
		t.Fatal(err)
	}
	if dead.Path != "/api/v0/code/dead-code" {
		t.Fatalf("dead path = %s", dead.Path)
	}
	if body := dead.Body.(map[string]any); body["repo_id"] != "r" {
		t.Fatalf("dead body = %#v", body)
	}
	fallback, err := resolveAnalyzeCodeRelationshipsRequest(routecontract.Arguments{"query_type": "who_modifies", "target": "T"})
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Path != "/api/v0/code/relationships" {
		t.Fatalf("fallback path = %s", fallback.Path)
	}
	chain, err := resolveAnalyzeCodeRelationshipsRequest(routecontract.Arguments{"query_type": "call_chain", "target": "A -> B"})
	if err != nil {
		t.Fatalf("call_chain: %v", err)
	}
	if chain.Path != "/api/v0/code/call-chain" {
		t.Fatalf("chain path = %s", chain.Path)
	}
	if body := chain.Body.(map[string]any); body["start"] != "A" || body["end"] != "B" {
		t.Fatalf("chain body = %#v", body)
	}
	if _, err := resolveAnalyzeCodeRelationshipsRequest(routecontract.Arguments{"query_type": "call_chain", "target": "noarrow"}); err == nil {
		t.Fatal("bad chain target passed")
	}
	xchain, err := resolveAnalyzeCodeRelationshipsRequest(routecontract.Arguments{"query_type": "find_cross_repo_call_chain", "start_entity_id": "s", "end_entity_id": "e"})
	if err != nil {
		t.Fatalf("xchain: %v", err)
	}
	if body := xchain.Body.(map[string]any); body["cross_repo"] != true {
		t.Fatalf("xchain cross_repo = %#v", body)
	}
}
