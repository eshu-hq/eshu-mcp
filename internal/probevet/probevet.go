// Package probevet is a temporary CI probe (unmerged): seeds a vet error.
package probevet

import "fmt"

// FormatMismatch triggers go vet's printf check: %d with a string argument.
func FormatMismatch() string {
	return fmt.Sprintf("%d", "not-a-number")
}
