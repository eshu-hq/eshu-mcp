// Package probelint is a temporary CI probe (unmerged): seeds an errcheck finding.
package probelint

import "os"

// IgnoreSetenvError calls os.Setenv without handling its error return.
func IgnoreSetenvError() {
	os.Setenv("ESHU_MCP_PROBE", "1")
}
