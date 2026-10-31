package supplychainimpacttools

import (
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

func TestBoolStr(t *testing.T) {
	t.Parallel()

	if boolStr(routecontract.Arguments{"v": true}, "v") != "true" {
		t.Fatal("true")
	}
	if boolStr(routecontract.Arguments{"v": false}, "v") != "false" {
		t.Fatal("false")
	}
	if boolStr(routecontract.Arguments{"v": "yes"}, "v") != "" || boolStr(routecontract.Arguments{}, "v") != "" {
		t.Fatal("non-bool/missing want empty")
	}
}

func TestFindingsRequestShape(t *testing.T) {
	t.Parallel()

	req := findingsRequest(routecontract.Arguments{
		"cve_id": "CVE-1", "limit": 10, "include_suppressed": true,
	})
	if req.Method != "GET" || req.Path != "/api/v0/supply-chain/impact/findings" {
		t.Fatalf("request = %+v", req)
	}
	if req.Query["cve_id"] != "CVE-1" || req.Query["limit"] != "10" || req.Query["include_suppressed"] != "true" {
		t.Fatalf("query = %#v", req.Query)
	}
	bare := findingsRequest(routecontract.Arguments{})
	if bare.Query["limit"] != "50" {
		t.Fatalf("default limit = %q", bare.Query["limit"])
	}
	if _, ok := bare.Query["include_suppressed"]; ok {
		t.Fatal("absent flag must stay unset")
	}
	q := aggregateFilterQuery(routecontract.Arguments{"severity": "high", "include_suppressed": false})
	if q["severity"] != "high" || q["include_suppressed"] != "false" {
		t.Fatalf("filter = %#v", q)
	}
}
