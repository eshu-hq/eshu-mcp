package impacttools

import (
	"testing"
)

func TestNamesSections(t *testing.T) {
	t.Parallel()

	if namesSections(nil) || namesSections([]any{}) || namesSections([]string{}) {
		t.Fatal("nil/empty names nothing")
	}
	if !namesSections([]any{"full"}) || !namesSections([]string{"handles"}) || !namesSections("bogus") || !namesSections(7) {
		t.Fatal("named/wrong-type values must count as named")
	}
}
