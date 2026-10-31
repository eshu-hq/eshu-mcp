package mcpserver

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPeekMCPMethod(t *testing.T) {
	t.Parallel()

	nilBody, _ := peekMCPMethod(&http.Request{})
	if nilBody != "unknown" {
		t.Fatalf("nil body = %q", nilBody)
	}
	payload := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{}}`
	req, _ := http.NewRequest("POST", "/mcp/message", strings.NewReader(payload))
	method, out := peekMCPMethod(req)
	if method != "tools/call" {
		t.Fatalf("method = %q", method)
	}
	rest, err := io.ReadAll(out.Body)
	if err != nil || string(rest) != payload {
		t.Fatalf("body not preserved: %q, %v", rest, err)
	}
	if err := out.Body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	garbage, _ := http.NewRequest("POST", "/mcp/message", strings.NewReader("not json"))
	if m, _ := peekMCPMethod(garbage); m != "unknown" {
		t.Fatalf("garbage = %q", m)
	}
	custom, _ := http.NewRequest("POST", "/mcp/message", strings.NewReader(`{"method":"frobnicate"}`))
	if m, _ := peekMCPMethod(custom); m != "other" {
		t.Fatalf("custom = %q", m)
	}
}
