package dispatch

import (
	"testing"
)

// TestAppendedToolsDispatchToCore proves the two post-seed registry tools
// reach core over their versioned read-contract routes: the SSM host
// inventory readback as a bounded GET with query filters, and the PHP
// configuration search as a POST with the guarded-evidence body. Before the
// port both names stop at ResolveRoute with the historic unknown-tool
// refusal, the same failure the B-7 gate reports. The eshu#10477 service
// paging tool has its own test below (TestServicePagingDispatchToCore);
// it stays separate because its route lives in the service-context family
// rather than the #9670 appendix lanes.
func TestAppendedToolsDispatchToCore(t *testing.T) {
	t.Parallel()

	t.Run("list_ssm_host_inventory", func(t *testing.T) {
		t.Parallel()

		stub := &stubCore{status: 200, body: `{"data":{"hosts":[]},"truth":null,"error":null}`}
		result, err := dispatchAgainstStub(t, stub, "list_ssm_host_inventory",
			map[string]any{
				"instance_id": "i-1234567890abcdef0",
				"account_id":  "123456789012",
				"region":      "us-east-1",
				"status":      "successful",
				"limit":       5,
				"cursor":      "10",
			}, "Bearer [REDACTED]", DefaultOptions())
		if err != nil {
			t.Fatalf("Dispatch() error = %v, want nil", err)
		}
		if result.IsError {
			t.Error("Dispatch() IsError = true, want false for a 200 envelope")
		}
		if stub.seenMethod != "GET" {
			t.Errorf("method = %q, want GET", stub.seenMethod)
		}
		if stub.seenPath != "/api/v0/cloud/ssm-host-inventory" {
			t.Errorf("path = %q, want the SSM host inventory route", stub.seenPath)
		}
		wantQuery := "account_id=123456789012&cursor=10&instance_id=i-1234567890abcdef0&limit=5&region=us-east-1&status=successful"
		if stub.seenQuery != wantQuery {
			t.Errorf("query = %q, want %q", stub.seenQuery, wantQuery)
		}
		if stub.seenAuth != "Bearer [REDACTED]" {
			t.Errorf("forwarded Authorization = %q, want the caller credential verbatim", stub.seenAuth)
		}
	})

	t.Run("search_php_configuration", func(t *testing.T) {
		t.Parallel()

		stub := &stubCore{status: 200, body: `{"data":{"findings":[]},"truth":null,"error":null}`}
		result, err := dispatchAgainstStub(t, stub, "search_php_configuration",
			map[string]any{"mode": "mod_php", "repo_id": "repo-a", "limit": 12, "cursor": "page-token"},
			"Bearer [REDACTED]", DefaultOptions())
		if err != nil {
			t.Fatalf("Dispatch() error = %v, want nil", err)
		}
		if result.IsError {
			t.Error("Dispatch() IsError = true, want false for a 200 envelope")
		}
		if stub.seenMethod != "POST" {
			t.Errorf("method = %q, want POST", stub.seenMethod)
		}
		if stub.seenPath != "/api/v0/content/php-config/search" {
			t.Errorf("path = %q, want the PHP configuration search route", stub.seenPath)
		}
		wantBody := `{"cursor":"page-token","directive":"","limit":12,"mode":"mod_php","repo_id":"repo-a"}`
		if stub.seenBody != wantBody {
			t.Errorf("body = %s, want %s", stub.seenBody, wantBody)
		}
	})
}

// TestServicePagingDispatchToCore proves the eshu#10477 appendix tool reaches
// core over its versioned read-contract route: list_service_api_endpoints as
// a GET on /api/v0/services/{selector}/api-surface/endpoints with story
// selector parity and opt-in paging, and the downstream_read_limit bound
// forwarded on get_service_story. Before the port the new name stops at
// ResolveRoute with the historic unknown-tool refusal, the same failure the
// B-7 gate reports.
func TestServicePagingDispatchToCore(t *testing.T) {
	t.Parallel()

	t.Run("endpoints_page", func(t *testing.T) {
		t.Parallel()

		stub := &stubCore{status: 200, body: `{"data":{"endpoints":[]},"truth":null,"error":null}`}
		result, err := dispatchAgainstStub(t, stub, "list_service_api_endpoints",
			map[string]any{
				"service_name": "payments",
				"repo":         "r1",
				"environment":  "prod",
				"limit":        10,
				"offset":       20,
			}, "Bearer [REDACTED]", DefaultOptions())
		if err != nil {
			t.Fatalf("Dispatch() error = %v, want nil", err)
		}
		if result.IsError {
			t.Error("Dispatch() IsError = true, want false for a 200 envelope")
		}
		if stub.seenMethod != "GET" {
			t.Errorf("method = %q, want GET", stub.seenMethod)
		}
		if stub.seenPath != "/api/v0/services/payments/api-surface/endpoints" {
			t.Errorf("path = %q, want the service endpoints route", stub.seenPath)
		}
		wantQuery := "environment=prod&limit=10&offset=20&repo=r1"
		if stub.seenQuery != wantQuery {
			t.Errorf("query = %q, want %q", stub.seenQuery, wantQuery)
		}
	})

	t.Run("endpoints_absent_paging_stays_absent", func(t *testing.T) {
		t.Parallel()

		stub := &stubCore{status: 200, body: `{"data":{"endpoints":[]},"truth":null,"error":null}`}
		if _, err := dispatchAgainstStub(t, stub, "list_service_api_endpoints",
			map[string]any{"workload_id": "workload:shop"},
			"Bearer [REDACTED]", DefaultOptions()); err != nil {
			t.Fatalf("Dispatch() error = %v, want nil", err)
		}
		if stub.seenPath != "/api/v0/services/workload:shop/api-surface/endpoints" {
			t.Errorf("path = %q, want the workload handle whole", stub.seenPath)
		}
		wantQuery := "service_id=workload%3Ashop"
		if stub.seenQuery != wantQuery {
			t.Errorf("query = %q, want %q", stub.seenQuery, wantQuery)
		}
	})

	t.Run("story_forwards_downstream_read_limit", func(t *testing.T) {
		t.Parallel()

		stub := &stubCore{status: 200, body: `{"data":{},"truth":null,"error":null}`}
		if _, err := dispatchAgainstStub(t, stub, "get_service_story",
			map[string]any{"service_id": "service:pay", "downstream_read_limit": 10},
			"Bearer [REDACTED]", DefaultOptions()); err != nil {
			t.Fatalf("Dispatch() error = %v, want nil", err)
		}
		if stub.seenPath != "/api/v0/services/pay/story" {
			t.Errorf("path = %q, want the service story route", stub.seenPath)
		}
		wantQuery := "downstream_read_limit=10&evidence_detail=handles"
		if stub.seenQuery != wantQuery {
			t.Errorf("query = %q, want %q", stub.seenQuery, wantQuery)
		}
	})
}
