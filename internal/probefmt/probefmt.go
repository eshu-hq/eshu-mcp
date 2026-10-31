// Package probefmt is a temporary CI probe (unmerged): seeds a gofumpt finding.
package probefmt

// Answer returns a constant through deliberately misaligned code.
func Answer() int {
	x :=  1

	return x + 41
}
