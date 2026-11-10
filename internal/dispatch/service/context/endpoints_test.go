package servicecontexttools

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-mcp/internal/routecontract"
)

// TestListServiceAPIEndpointsRoute (eshu-hq/eshu-mcp#65, eshu#10477)
// proves list_service_api_endpoints resolves to GET
// /api/v0/services/{selector}/api-surface/endpoints with get_service_story
// selector parity: service_id wins over workload_id wins over service_name.
func TestListServiceAPIEndpointsRoute(t *testing.T) {
	t.Parallel()

	route, handled, err := Route("list_service_api_endpoints", routecontract.Arguments{
		"service_id": "service:payments-api",
	})
	if err != nil {
		t.Fatalf("Route error = %v, want nil", err)
	}
	if !handled {
		t.Fatal("Route(list_service_api_endpoints) handled = false, want true")
	}
	if route.Method != "GET" || route.Path != "/api/v0/services/payments-api/api-surface/endpoints" {
		t.Fatalf("Route = %v %v, want GET /api/v0/services/payments-api/api-surface/endpoints", route.Method, route.Path)
	}
	if len(route.Query) != 0 {
		t.Fatalf("Route query = %v, want empty when only a bare selector is given", route.Query)
	}
}

// TestListServiceAPIEndpointsSelectorPrecedence proves the first non-blank
// selector wins in service_id, workload_id, service_name order, and that a
// workload-qualified selector is also forwarded as the service_id query.
func TestListServiceAPIEndpointsSelectorPrecedence(t *testing.T) {
	t.Parallel()

	route, _, err := Route("list_service_api_endpoints", routecontract.Arguments{
		"service_id":   "service:exact",
		"workload_id":  "workload:shop",
		"service_name": "payments",
	})
	if err != nil {
		t.Fatal(err)
	}
	if route.Path != "/api/v0/services/exact/api-surface/endpoints" {
		t.Fatalf("Route path = %q, want the service_id selector to win", route.Path)
	}
	route, _, err = Route("list_service_api_endpoints", routecontract.Arguments{
		"workload_id":  "workload:shop",
		"service_name": "payments",
	})
	if err != nil {
		t.Fatal(err)
	}
	if route.Path != "/api/v0/services/workload:shop/api-surface/endpoints" {
		t.Fatalf("Route path = %q, want the workload_id selector whole", route.Path)
	}
	if route.Query["service_id"] != "workload:shop" {
		t.Fatalf("Route query = %v, want the workload handle forwarded as service_id", route.Query)
	}
}

// TestListServiceAPIEndpointsPagingAndRepo proves limit/offset travel only
// when the caller names them (the handler owns defaults, clamping, and
// 400s), while environment and the repo aliases always forward.
func TestListServiceAPIEndpointsPagingAndRepo(t *testing.T) {
	t.Parallel()

	route, _, err := Route("list_service_api_endpoints", routecontract.Arguments{
		"service_name": "payments",
		"repo_id":      "r1",
		"environment":  "prod",
		"limit":        10,
		"offset":       20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if route.Path != "/api/v0/services/payments/api-surface/endpoints" {
		t.Fatalf("Route path = %q, want the service_name path", route.Path)
	}
	want := map[string]string{"repo": "r1", "environment": "prod", "limit": "10", "offset": "20"}
	for key, value := range want {
		if route.Query[key] != value {
			t.Fatalf("Route query[%q] = %q, want %q (query = %v)", key, route.Query[key], value, route.Query)
		}
	}
	if len(route.Query) != len(want) {
		t.Fatalf("Route query = %v, want exactly %v", route.Query, want)
	}
}

// TestListServiceAPIEndpointsRequiresSelector proves a missing selector is
// a handled refusal naming the tool, never a request with an empty path
// segment.
func TestListServiceAPIEndpointsRequiresSelector(t *testing.T) {
	t.Parallel()

	_, handled, err := Route("list_service_api_endpoints", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(list_service_api_endpoints, {}) handled = false, want true with a refusal")
	}
	if err == nil || err.Error() != "list_service_api_endpoints requires service_id, workload_id, or service_name" {
		t.Fatalf("Route error = %v, want the selector refusal", err)
	}
}

// TestDownstreamReadLimitForwarded (eshu#10477) proves the explicit
// downstream bound travels as the HTTP query parameter of the same name on
// story and context; absent stays absent so the handler keeps its default.
func TestDownstreamReadLimitForwarded(t *testing.T) {
	t.Parallel()

	story, err := serviceSelectorRoute(routecontract.Arguments{
		"service_id":            "service:pay",
		"downstream_read_limit": 10,
	}, "get_service_story", "story")
	if err != nil {
		t.Fatal(err)
	}
	if story.Query["downstream_read_limit"] != "10" {
		t.Fatalf("story query = %v, want downstream_read_limit=10", story.Query)
	}
	bare, err := serviceSelectorRoute(routecontract.Arguments{
		"service_id": "service:pay",
	}, "get_service_story", "story")
	if err != nil {
		t.Fatal(err)
	}
	if _, present := bare.Query["downstream_read_limit"]; present {
		t.Fatalf("story query = %v, want no downstream_read_limit when absent", bare.Query)
	}
	context, err := serviceSelectorRoute(routecontract.Arguments{
		"workload_id":           "workload:shop",
		"downstream_read_limit": 7,
	}, "get_service_context", "context")
	if err != nil {
		t.Fatal(err)
	}
	if context.Query["downstream_read_limit"] != "7" {
		t.Fatalf("context query = %v, want downstream_read_limit=7", context.Query)
	}
	if !strings.HasSuffix(context.Path, "/services/workload:shop/context") {
		t.Fatalf("context path = %q, want the workload path", context.Path)
	}
}
