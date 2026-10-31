package dispatch

import (
	"testing"
)

// TestAppendedToolsDispatchToCore proves the two post-seed registry tools
// reach core over their versioned read-contract routes: the SSM host
// inventory readback as a bounded GET with query filters, and the PHP
// configuration search as a POST with the guarded-evidence body. Before the
// port both names stop at ResolveRoute with the historic unknown-tool
// refusal, the same failure the B-7 gate reports.
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
