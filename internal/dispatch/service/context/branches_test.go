package servicecontexttools

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

func TestServiceSelectorRouteBranches(t *testing.T) {
	t.Parallel()

	if _, err := serviceSelectorRoute(routecontract.Arguments{}, "get_service_story", "story"); err == nil {
		t.Fatal("story without selector passed")
	}
	if _, err := serviceSelectorRoute(routecontract.Arguments{}, "get_service_context", "context"); err == nil {
		t.Fatal("context without selector passed")
	}
	req, err := serviceSelectorRoute(routecontract.Arguments{
		"service_name": "payments",
		"environment":  "prod",
	}, "get_service_story", "story")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(req.Path, "/services/payments/story") || req.Query["environment"] != "prod" {
		t.Fatalf("story = %+v", req)
	}
	req, err = serviceSelectorRoute(routecontract.Arguments{
		"workload_id": "workload:shop",
	}, "get_service_context", "context")
	if err != nil {
		t.Fatal(err)
	}
	if req.Query["evidence_detail"] != "handles" || !strings.HasSuffix(req.Path, "/services/workload:shop/context") {
		t.Fatalf("context = %+v", req)
	}
	req, err = serviceSelectorRoute(routecontract.Arguments{
		"workload_id": "workload:shop",
		"repo_id":     "r1",
	}, "get_service_story", "story")
	if err != nil {
		t.Fatal(err)
	}
	if req.Query["service_id"] != "workload:shop" || req.Query["repo"] != "r1" {
		t.Fatalf("story forwarding = %+v", req)
	}
	req, err = serviceSelectorRoute(routecontract.Arguments{
		"workload_id":     "w1",
		"evidence_detail": "full",
	}, "get_service_context", "context")
	if err != nil {
		t.Fatal(err)
	}
	if req.Query["evidence_detail"] != "full" {
		t.Fatalf("override = %+v", req.Query)
	}
}

func TestIntelligenceAndInvestigateRoutes(t *testing.T) {
	t.Parallel()

	if _, err := serviceIntelligenceReportRoute(routecontract.Arguments{}); err == nil {
		t.Fatal("report without selector passed")
	}
	req, err := serviceIntelligenceReportRoute(routecontract.Arguments{
		"service_id": "service:pay",
		"repo":       "r1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(req.Path, "/services/pay/intelligence-report") || req.Query["repo"] != "r1" {
		t.Fatalf("report = %+v", req)
	}
	if _, err := investigateServiceRoute(routecontract.Arguments{}); err == nil {
		t.Fatal("investigate without selector passed")
	}
	req, err = investigateServiceRoute(routecontract.Arguments{
		"service_name": "svc",
		"intent":       "why slow",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(req.Path, "/investigations/services/svc") || req.Query["intent"] != "why slow" {
		t.Fatalf("investigate = %+v", req)
	}
}

func TestIdentifierHelpers(t *testing.T) {
	t.Parallel()

	if firstSelector(routecontract.Arguments{"service_name": "  ", "workload_id": "w"}, "service_id", "workload_id") != "w" {
		t.Fatal("firstSelector precedence")
	}
	if firstSelector(routecontract.Arguments{}, "a") != "" {
		t.Fatal("firstSelector empty")
	}
	if serviceStoryRepositorySelector(routecontract.Arguments{"repo_id": "r"}) != "r" {
		t.Fatal("repo_id alias")
	}
	if normalizeQualifiedIdentifier("service:pay") != "pay" {
		t.Fatal("prefix strip")
	}
	if normalizeQualifiedIdentifier("workload:shop") != "workload:shop" {
		t.Fatal("workload handle must pass whole")
	}
	if normalizeQualifiedIdentifier(":x") != ":x" || normalizeQualifiedIdentifier("a:") != "a:" {
		t.Fatal("empty head/tail passes through")
	}
	if canonicalWorkloadIdentifier("workload:shop") == "" || canonicalWorkloadIdentifier("plain") != "" {
		t.Fatal("canonicalWorkloadIdentifier wrong")
	}
}
