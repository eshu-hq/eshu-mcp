package mcpserver

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateForLog(t *testing.T) {
	t.Parallel()

	if got := truncateForLog("short"); got != "short" {
		t.Fatalf("short = %q", got)
	}
	long := strings.Repeat("a", 100)
	got := truncateForLog(long)
	if !strings.HasSuffix(got, "...") || len(got) > 64+3 {
		t.Fatalf("long cut = %q", got)
	}
	if !utf8.ValidString(strings.TrimSuffix(got, "...")) {
		t.Fatal("cut is not valid UTF-8")
	}
	// Multi-byte runes: the cut must back up to a rune boundary.
	runes := strings.Repeat("é", 40)
	got = truncateForLog(runes)
	if !utf8.ValidString(strings.TrimSuffix(got, "...")) {
		t.Fatalf("rune cut invalid: %q", got)
	}
}

func TestIsProductionProfile(t *testing.T) {
	t.Parallel()

	if !IsProductionProfile("production") || !IsProductionProfile("  production  ") {
		t.Fatal("production variants rejected")
	}
	for _, raw := range []string{"", "Production", "staging", "productionx"} {
		if IsProductionProfile(raw) {
			t.Fatalf("%q accepted", raw)
		}
	}
}

func TestRequireCredentialSource(t *testing.T) {
	t.Parallel()

	if err := RequireCredentialSource(true, false); err != nil {
		t.Fatalf("shared key: %v", err)
	}
	if err := RequireCredentialSource(false, true); err != nil {
		t.Fatalf("escape hatch: %v", err)
	}
	if err := RequireCredentialSource(false, false); err == nil {
		t.Fatal("open mode accepted")
	} else if err != ErrNoCredentialSource {
		t.Fatalf("error = %v, want the sentinel", err)
	}
}
