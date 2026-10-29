package sourcetool

import (
	"testing"
)

// TestVocabulary proves the closed source_tool set admits its members and
// nothing else, including case variants.
func TestVocabulary(t *testing.T) {
	t.Parallel()

	for _, token := range Canonical {
		if !IsValid(token) {
			t.Errorf("IsValid(%q) = false, want true", token)
		}
		if IsValid(" "+token) || IsValid(token+" ") {
			t.Errorf("IsValid(padded %q) = true, want exact match", token)
		}
	}
	for _, token := range []string{"", "terraformx", "Docker", "DOCKER", "not-a-tool"} {
		if IsValid(token) {
			t.Errorf("IsValid(%q) = true, want false", token)
		}
	}
}
