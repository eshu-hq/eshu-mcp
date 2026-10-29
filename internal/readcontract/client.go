package readcontract

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultTimeout bounds every read-contract call. It matches the
	// in-tree dispatch default pinned by the source census.
	DefaultTimeout = 30 * time.Second

	// DefaultResponseByteBudget caps a single read-contract response body.
	// It matches the in-tree dispatch budget pinned by the source census.
	DefaultResponseByteBudget = 256 * 1024

	// EnvelopeMIME is the canonical eshu envelope every dispatch requests.
	EnvelopeMIME = "application/eshu.envelope+json"

	// readAPIPrefix is the versioned read-API path all routes hang under.
	readAPIPrefix = "/api/v0/"

	// errorSnippetBytes caps how much of an error body a StatusError keeps,
	// so a failing route cannot flood operator output.
	errorSnippetBytes = 256
)

// ErrOverBudget reports a read-contract body larger than the configured
// response byte budget.
var ErrOverBudget = fmt.Errorf("readcontract: response exceeds byte budget")

// StatusError reports a non-2xx answer from a read-contract route.
type StatusError struct {
	// StatusCode is the HTTP status the route answered.
	StatusCode int
	// Route is the read-contract route that was called, without the host.
	Route string
	// Snippet is the first bytes of the error body, clipped for operators.
	Snippet string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("readcontract: route %s answered HTTP %d", e.Route, e.StatusCode)
}

// Config wires a Client to one core read-API deployment. The API key travels
// only in the Authorization header; this package never logs it.
type Config struct {
	// BaseURL is the core read-API origin, for example https://core:8443.
	// A trailing slash is tolerated.
	BaseURL string
	// APIKey is the bearer credential sent on every call. It is never logged.
	APIKey string
	// Timeout bounds a single call, including body read.
	Timeout time.Duration
	// ResponseByteBudget caps a single response body.
	ResponseByteBudget int
}

// DefaultConfig returns the pinned census bounds for one core origin.
func DefaultConfig(baseURL, apiKey string) Config {
	return Config{
		BaseURL:            baseURL,
		APIKey:             apiKey,
		Timeout:            DefaultTimeout,
		ResponseByteBudget: DefaultResponseByteBudget,
	}
}

// Client calls versioned core read contracts over HTTP.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient builds a Client from cfg.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// Do calls one read-contract route and returns its raw envelope body.
// Route names a path under /api/v0/, for example "repositories".
func (c *Client) Do(ctx context.Context, route string, query url.Values) ([]byte, error) {
	target := strings.TrimRight(c.cfg.BaseURL, "/") + readAPIPrefix + strings.TrimLeft(route, "/")
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("readcontract: build request for %s: %w", route, err)
	}
	req.Header.Set("Accept", EnvelopeMIME)
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("readcontract: call %s: %w", route, err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(c.cfg.ResponseByteBudget)+1))
	if closeErr := resp.Body.Close(); closeErr != nil && readErr == nil {
		readErr = closeErr
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if readErr != nil {
			// A short body still diagnoses a refused route; the status wins.
			raw = nil
		}
		snippet := raw
		if len(snippet) > errorSnippetBytes {
			snippet = snippet[:errorSnippetBytes]
		}
		return nil, &StatusError{StatusCode: resp.StatusCode, Route: route, Snippet: string(snippet)}
	}
	if readErr != nil {
		return nil, fmt.Errorf("readcontract: read %s: %w", route, readErr)
	}
	if len(raw) > c.cfg.ResponseByteBudget {
		return nil, fmt.Errorf("readcontract: read %s: %w", route, ErrOverBudget)
	}
	return raw, nil
}
