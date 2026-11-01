# Operating eshu-mcp: SSE session limits (Refs #50)

Read this before you deploy or re-pin a release at v0.2.8 or later. Earlier
releases do not limit SSE sessions, so moving to v0.2.8 changes behavior for
deployments that were never limited before.

## Upgrade note: callers behind the shared key share one allowance of 32 sessions

`GET /sse` opens a long-lived stream (SSE, Server-Sent Events) that an MCP
client holds open for as long as it is connected. From v0.2.8 each principal
may hold at most **32** of them at once.

A principal is whoever the credential identifies. The deployment key
(`ESHU_API_KEY`, the "shared key") is a single principal, whoever presents it.
Every person and every tool that connects with that key counts together
against the same 32. Callers with their own credentials (personal tokens, OIDC
JWTs) each get their own 32.

| Credential | Principal | Allowance |
| --- | --- | --- |
| `ESHU_API_KEY` (shared key) | one principal named `shared` | 32 for everyone using it |
| Personal or scoped token | one per token | 32 per token |
| OIDC JWT, verified at the edge | one per issuer and `sub` claim | 32 per user, shared by all of that user's tokens |
| Other credential forwarded to core without edge verification | one per distinct credential | 32 per credential |
| Transport auth off (the escape hatch: no credential check) | one anonymous principal | 32 for everyone |

There is also a process-wide ceiling of **2048** sessions across all
principals (`ESHU_MCP_MAX_SSE_SESSIONS_TOTAL`).

Both values are first guesses. They have not been tuned against real traffic.

### What a caller sees when the limit is hit

- The request gets HTTP `429` with `Retry-After: 5` and a JSON-RPC error with
  code `-32002`. The body reads `too many open sessions; close one or retry
  shortly`. The same response covers the per-principal cap and the process
  ceiling.
- No stream header is written, so the client sees a plain failed connect.
- Some clients, notably `EventSource`-style ones, treat any non-200 as fatal
  and do not honor `Retry-After`. Such a client stays disconnected until
  someone restarts it.
- A slot is freed when the server notices the connection is gone (a failed
  write or keepalive, or a context cancel). A dead peer therefore holds its
  slot for a short time. A flaky network that reconnects in a loop can briefly
  use more than one slot per client and push a busy shared key to its cap.

### Before you deploy or re-pin

1. Find out whether any deployment gives callers the shared key, and how many
   concurrent clients sit behind it. Count each editor session, agent, and
   script as a client, plus headroom for reconnect overlap. The satellite does
   not know this: it cannot tell the people behind the shared key apart.
2. If the expected concurrent clients plus reconnect overlap approach 32, set
   `ESHU_MCP_MAX_SSE_SESSIONS_PER_PRINCIPAL` above that number on the
   `eshu-mcp` container. A malformed or non-positive value fails the boot, so
   a bad value shows up at rollout and never in traffic.
3. Keep the per-principal value at or below the process ceiling
   (`ESHU_MCP_MAX_SSE_SESSIONS_TOTAL`, default 2048). A shared key can never
   hold more than the ceiling allows.
4. After the rollout, watch `/healthz` (below). A non-zero and growing
   `sse.rejected` means callers are being refused.

If you cannot tell who uses the shared key, prefer giving each person or tool
its own credential over raising the shared allowance without bound. A raised
cap keeps one leaking or looping client from being contained.

### Settings

| Variable | Default | Effect |
| --- | --- | --- |
| `ESHU_MCP_MAX_SSE_SESSIONS_PER_PRINCIPAL` | `32` | Open sessions allowed per principal. Boot fails on a malformed or non-positive value. |
| `ESHU_MCP_MAX_SSE_SESSIONS_TOTAL` | `2048` | Open sessions allowed across the whole process. Boot fails on a malformed or non-positive value. |

### What to watch

`GET /healthz` includes:

```json
{ "sse": { "active": 14, "rejected": 0 } }
```

- `sse.active`: sessions open now, summed over all principals. Compare it
  with the process ceiling (2048). It cannot show how close one principal is
  to its cap of 32, because `/healthz` reports no per-principal figure.
- `sse.rejected`: refusals since the process started, per-principal cap and
  process ceiling counted together. It only grows; a rise between two scrapes
  means callers were refused in that window. This is the signal that a
  principal, most likely the shared key, has hit its cap. Confirm which limit
  it was from the refusal log `outcome` below.

The refusal log line is a warning. Its `outcome` field tells the two limits
apart and names the limit, but never the principal:

- `sse_session_cap_reached`: a principal is at its cap. Raise the per-principal
  variable, or split the callers onto their own credentials.
- `sse_session_ceiling_reached`: the process is at its ceiling. Many distinct
  credentials are connected at once; raise the total or scale out replicas.

Counters are per process. With more than one replica, each replica counts and
limits on its own, so the effective allowance for a principal is the cap times
the number of replicas that receive its connections.

### Not verified

- Whether the defaults suit real traffic. No deployment has been observed
  against them.
- Which deployments use the shared key with many users. That is deploy-repo
  knowledge and is not recorded in this tree.

The mechanism, the key canonicalization, and the tests behind it are in
[parity](parity.md#sse-session-bound-eshu-hqeshu-mcp50).
