package dispatch

import (
	"strings"
	"testing"
)

// TestRepositoryStoryEvidenceDetailDefault pins eshu-hq/eshu-mcp#71 (story
// half): a no-argument get_repo_story call builds a path carrying
// evidence_detail=handles, and an explicit evidence_detail wins, exactly like
// the get_service_story precedent.
func TestRepositoryStoryEvidenceDetailDefault(t *testing.T) {
	t.Parallel()

	req, handled, err := repositoryRoute("get_repo_story", map[string]any{"repo_id": "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("get_repo_story not handled")
	}
	if !strings.HasSuffix(req.Path, "/repositories/r1/story") {
		t.Fatalf("story path = %q", req.Path)
	}
	if req.Query["evidence_detail"] != "handles" {
		t.Fatalf("default evidence_detail = %q, want handles", req.Query["evidence_detail"])
	}

	req, handled, err = repositoryRoute("get_repo_story", map[string]any{
		"repo_id":         "r1",
		"evidence_detail": "full",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("get_repo_story with explicit detail not handled")
	}
	if req.Query["evidence_detail"] != "full" {
		t.Fatalf("explicit evidence_detail = %q, want full", req.Query["evidence_detail"])
	}
}

// TestRepositoryContextEvidenceDetailDefault pins eshu-hq/eshu-mcp#71
// (context half): a no-argument get_repo_context call builds a path carrying
// evidence_detail=handles, and an explicit evidence_detail wins, exactly like
// the get_service_context precedent. Unblocked by eshu-hq/eshu#10654 (core
// main accepts evidence_detail on /repositories/{id}/context).
func TestRepositoryContextEvidenceDetailDefault(t *testing.T) {
	t.Parallel()

	req, handled, err := repositoryRoute("get_repo_context", map[string]any{"repo_id": "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("get_repo_context not handled")
	}
	if !strings.HasSuffix(req.Path, "/repositories/r1/context") {
		t.Fatalf("context path = %q", req.Path)
	}
	if req.Query["evidence_detail"] != "handles" {
		t.Fatalf("default evidence_detail = %q, want handles", req.Query["evidence_detail"])
	}

	req, handled, err = repositoryRoute("get_repo_context", map[string]any{
		"repo_id":         "r1",
		"evidence_detail": "full",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("get_repo_context with explicit detail not handled")
	}
	if req.Query["evidence_detail"] != "full" {
		t.Fatalf("explicit evidence_detail = %q, want full", req.Query["evidence_detail"])
	}
}
