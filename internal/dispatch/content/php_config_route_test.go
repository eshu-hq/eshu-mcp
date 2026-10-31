package contenttools

import (
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// TestPHPConfigurationRoute pins the post-seed PHP configuration search to
// its versioned core route: a POST carrying the directive and mode selectors
// with the bounded limit and the opaque page cursor. It mirrors the source
// php_config_routes_test.go case for case.
func TestPHPConfigurationRoute(t *testing.T) {
	t.Parallel()
	got, ok := Route("search_php_configuration", routecontract.Arguments{"mode": "mod_php", "repo_id": "repo-a", "limit": 12, "cursor": "page-token"})
	if !ok || got.Method != "POST" || got.Path != "/api/v0/content/php-config/search" {
		t.Fatalf("route=%+v ok=%t", got, ok)
	}
	body, ok := got.Body.(map[string]any)
	if !ok || body["mode"] != "mod_php" || body["repo_id"] != "repo-a" || body["limit"] != 12 || body["cursor"] != "page-token" {
		t.Fatalf("body=%+v ok=%t", got.Body, ok)
	}
}
