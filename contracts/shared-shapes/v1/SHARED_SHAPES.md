# Shared envelope and result shapes v1

Refs `eshu-hq/eshu#8873`. Status: shape references, not a Go move.

## Rule

Adapters stay separate repositories. Shared response and result shapes belong
to one explicit public contract with a single publisher, never to whichever
adapter imports them first. This repository consumes that contract; it does
not publish it and does not vendor core implementation packages.

## Shapes (by reference to the pinned source commit)

| Shape | Source file | SHA-256 |
| --- | --- | --- |
| Dispatch envelope and result layout | `go/internal/mcp/result/shape/shape.go` | `e2ca3bf593694d87aaa3207b5236f199f4ba42b2537ab3a199658d83140fd82b` |
| Tool contract type | `go/internal/mcp/contract/tool/contract.go` | `f77e1f42a63662b033248487f8f58b1e5f4bcd0c24bbf130041be5497e3be64a` |
| Route contract type | `go/internal/mcp/contract/route/contract.go` | `a146745325224caebce0409606bf49d09b370169780608c47fa917cf14d6e416` |
| Envelope aliases (`EnvelopeMIMEType`) | `go/internal/query/envelope_aliases.go` | `f955532fbe87540d0e6650266728781ce2bb7587ad2da8ff908458046990ec84` |

Envelope MIME type: `application/eshu.envelope+json`.

Source commit for every hash:
`e4c385f493494ed9d6c7f192b04ce0255361f621`.

## Open question

Which explicit public contract publishes these shapes (SDK fact-schema
module versus a read-contract module) is still owner-decided in the source
ownership map. No Go code moves here until that publisher exists; this file
records the reference so the move stays mechanical.
