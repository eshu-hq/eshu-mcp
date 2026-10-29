// Package readcontract is the only path this binary uses to reach core
// truth: versioned HTTP read contracts. It never imports core
// implementation packages; at cutover it replaces the in-tree wiring
// composition with these clients.
package readcontract
