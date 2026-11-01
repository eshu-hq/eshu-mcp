package readcontract

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// admissionProbePath is the one core route the admission probe reads: the
// static capability catalog. It is authenticated, carries no tenant data, and
// core lets every credential class it accepts call it (a scoped token, a
// personal token, a file-registry token, and the shared key), so a 200 means
// core admits the credential and a 401 or 403 means it does not. The profile
// route is not used: it refuses subject-less credentials that core admits.
//
// Core sources this rests on, at eshu 9032fc3073:
//   - the route is classed scopedRouteTenantDataFree, so a caller needs no
//     repository grant:
//     https://github.com/eshu-hq/eshu/blob/9032fc3073/go/internal/query/auth_scoped_routes_completeness.go#L109
//   - a scoped token reaches it (TestAuthMiddlewareWithScopedTokensAllowsCapabilityCatalogRoute):
//     https://github.com/eshu-hq/eshu/blob/9032fc3073/go/internal/query/auth_capabilities_test.go
//   - the OpenAPI entry carries x-scoped-token-support:
//     https://github.com/eshu-hq/eshu/blob/9032fc3073/go/internal/query/openapi/paths/catalog/capabilities.go
const (
	admissionProbePath  = "/api/v0/capabilities"
	admissionProbeQuery = "limit=1"
)

// ProbeAdmission asks core whether it admits one credential. It sends a single
// GET on a tenant-data-free, authenticated route carrying authHeader verbatim,
// and returns core's HTTP status and its WWW-Authenticate challenge. The body
// is discarded unread beyond a small drain, so the probe cannot be used to
// move data, and redirects are not followed. authHeader is never logged. A
// transport error or timeout is returned as an error: the caller decides what
// an unanswered probe means.
func (c *Client) ProbeAdmission(ctx context.Context, authHeader string) (status int, challenge string, err error) {
	target := strings.TrimRight(c.cfg.BaseURL, "/") + admissionProbePath + "?" + admissionProbeQuery
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, "", fmt.Errorf("readcontract: build admission probe: %w", err)
	}
	req.Header.Set("Accept", EnvelopeMIME)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("readcontract: admission probe: %w", err)
	}
	// The answer is the status and challenge. Drain a bounded amount so the
	// connection can be reused; a failed drain or close changes nothing.
	defer func() {
		_, derr := io.Copy(io.Discard, io.LimitReader(resp.Body, probeDrainBytes))
		cerr := resp.Body.Close()
		if derr != nil || cerr != nil {
			return
		}
	}()
	return resp.StatusCode, resp.Header.Get("WWW-Authenticate"), nil
}

// probeDrainBytes bounds how much of a probe response is read before the body
// is closed.
const probeDrainBytes = 8 << 10
