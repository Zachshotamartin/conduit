# Conduit Protocol Conformance Specification

## 1. Document Status

Document status: accepted.
Normative wire-protocol specification. Last revised: 2026-08-30.

Companion specifications:
[Product requirements](./PRODUCT_REQUIREMENTS.md),
[Build plan](./BUILD_PLAN.md), [Architecture](./ARCHITECTURE.md),
[Authorization model](./AUTHORIZATION_MODEL.md),
[Operations and test plan](./OPERATIONS_TEST_PLAN.md),
[Threat model](./THREAT_MODEL.md), [Glossary](./GLOSSARY.md),
[Open questions](./OPEN_QUESTIONS.md),
[ADR-0002](./decisions/ADR-0002-graphql-transport-ws-only.md),
[ADR-0007](./decisions/ADR-0007-at-most-once-live-with-bounded-resume.md),
[ADR-0008](./decisions/ADR-0008-reconnect-refresh-and-bus-propagated-revocation.md).

Owning gate: **R2** (subscriptions over `graphql-transport-ws`, conformance
against the unmodified reference client) for every section of this document
except the resume extension behaviors in §4.9.1–§4.9.3 and the resume rows of
§7, whose owning gate is **R7**. Backpressure-notice wire shapes (§4.9.1
`dropped`) are pinned here but their runtime behavior is owned by R6; the
grant-expiry ping payload (§4.9.4) is pinned here and its enforcement is
owned by R3. Where this document names another gate on a row, that gate owns
the behavior; R2 owns the wire shape.

Status of every deliverable in this document: `planned`. Nothing below is an
implementation claim. Per the conflict rules in [README](./README.md), this
document controls wire-protocol behavior that does not conflict with an
accepted ADR, PRODUCT_REQUIREMENTS, BUILD_PLAN, or ARCHITECTURE.

This document reproduces the `graphql-transport-ws` protocol (the
`PROTOCOL.md` shipped with the `graphql-ws` npm package) as Conduit commits
to implement it. Where the protocol document is silent or ambiguous, the
behavior is a recorded Conduit decision in the ambiguity register (§5) and is
never attributed to the specification.

## 2. Scope and Transport Settings

### 2.1 One protocol

Conduit implements the `graphql-transport-ws` subprotocol and nothing else
(ADR-0002, FR-SUB-001). The legacy `subscriptions-transport-ws` protocol
(subprotocol string `graphql-ws`), SSE transports, and HTTP long-polling are
out of scope for this document and for Conduit 1.0; §12.1 records the
deferrals and their reopen triggers.

### 2.2 Handshake requirements

- The client listener accepts `GET /graphql` with a standard HTTP/1.1 WebSocket upgrade: `Upgrade: websocket`, `Connection: Upgrade`, `Sec-WebSocket-Version: 13`, and a valid `Sec-WebSocket-Key`.
- The upgrade request must offer `graphql-transport-ws` in `Sec-WebSocket-Protocol`. Conduit selects exactly that subprotocol and echoes it in the `101` response. A request offering multiple subprotocols that include `graphql-transport-ws` is accepted with only `graphql-transport-ws` selected.
- A request offering only unrecognized subprotocols (including the legacy `graphql-ws` string) or no subprotocol is rejected **pre-handshake** with HTTP `400 Bad Request` and a plain-text body naming `graphql-transport-ws` as the only supported subprotocol (ADR-0002).
- If a WebSocket connection is ever established without the `graphql-transport-ws` subprotocol agreed (intermediary interference, library edge path), Conduit closes it **post-handshake** with close code `4406: Subprotocol not acceptable` before processing any frame.
- Load shedding under file-descriptor pressure rejects the upgrade with HTTP `503` before the WebSocket exists (FR-CONN-014). Handshake-level failures are always HTTP status codes, never WebSocket close codes.
- TLS is terminated locally or the listener runs in `trusted_proxy` mode with the mandatory proxy allowlist (FR-CONN-013, NFR-SEC-005). Handshake bytes cross a bounded reader before parsing (NFR-SEC-001); a handshake that has not completed within the slowloris deadline (default 10 s, configurable) is dropped at the TCP level with no response.

### 2.3 WebSocket-layer settings

- **No compression.** `permessage-deflate` is not negotiated; a client offering the extension gets a `101` response without it. Compression amplification is a named DoS vector (NFR-SEC-008). Enabling deflate is a deferred decision (§12.1).
- **Text frames only.** Every protocol message is one UTF-8 JSON text message. A binary frame in any state closes `4400` with reason `"binary frames not supported"` (Conduit policy, ambiguity register §5.10).
- **Inbound message bound.** Complete inbound messages are bounded at 512 KiB (FR-SUB-009). A message exceeding the bound closes `4400` with reason `"message too large"`. Close code `1009` is **reserved as the WebSocket library's own backstop limit**, configured to the same value so no larger frame is ever buffered; a `1009` close in testing indicates the Conduit-level check was bypassed and is itself a conformance failure (CONF-024, HOST-004).
- **Fragmentation.** Fragmented text messages are legal; the bound applies to the reassembled message, accounted incrementally so an attacker cannot buffer more than the bound via continuation frames (HOST-008).
- **Control frames.** WebSocket-level ping/pong/close control frames are handled by the transport layer, are distinct from the protocol's `Ping`/ `Pong` messages, and never enter the protocol state machine. Outbound protocol-level `Ping`, `Pong`, `ConnectionAck`, `Error`, `Complete`, and the close frame bypass the data queue and are never dropped by backpressure (FR-CONN-007).

### 2.4 Defaults referenced throughout

| Setting | Default | Close code on breach | Requirement |
| --- | --- | --- | --- |
| `connection_init` timeout | 3 s | 4408 | FR-SUB-003 |
| Server keepalive ping interval | 25 s | none (see idle) | FR-SUB-007 |
| Idle timeout (no inbound traffic; pongs count) | 5 min | 4702 | FR-CONN-002 |
| Maximum connection lifetime | 12 h, jitter ±10% | 4701 | FR-CONN-003 |
| Inbound message bound | 512 KiB | 4400 | FR-SUB-009 |
| Subscription ID bound | 255 bytes | 4400 | FR-SUB-005 |
| Inbound message rate limit | 50 msg/s, burst 100 | 4400-class after warning | FR-CONN-006 |

All values are configurable; the close-code semantics are not.

## 3. Connection State Machine

The state machine is implemented as an explicit typed state table; illegal
transitions are structurally unrepresentable or rejected with typed errors,
and the table is exhaustively unit-tested before any socket integration
(FR-SUB-011). Status: `planned`.

### 3.1 States

| State | Meaning | Timers armed |
| --- | --- | --- |
| `connecting` | TCP accepted; HTTP upgrade being read and validated. No WebSocket exists yet. | slowloris handshake deadline |
| `awaiting_init` | Upgrade complete, subprotocol agreed; waiting for `ConnectionInit`. | init timeout (3 s) |
| `authenticating` | Structurally valid `ConnectionInit` received; credentials handed to the configured auth mode (FR-SUB-004). | auth-mode deadline (custom authorizer timeout per FR-AUTH-003) |
| `ready` | `ConnectionAck` sent; subscriptions legal. | keepalive ping (25 s), idle (5 min), lifetime (12 h ±10%) |
| `draining_connection` | Node drain selected this connection; existing subscriptions still deliver; close scheduled at the paced slot (FR-CONN-010). | drain slot deadline |
| `closing` | Server sent its close frame; awaiting the client's close echo or the closing grace (default 5 s). | closing grace |
| `closed` | Terminal. Registry entry, subscription entries, queues, and quota accounting released atomically (FR-CONN-001). | none |

### 3.2 Input alphabet

Inbound client messages: `ConnectionInit`, `Ping`, `Pong`, `Subscribe`,
`Complete`, a server-only type sent by the client (`ConnectionAck`, `Next`,
`Error`), an invalid or unknown message (malformed JSON, unknown `type`,
missing required field, wrong field type, duplicate JSON keys, invalid
UTF-8), a binary frame, an oversized message, a client-initiated WebSocket
close frame, and TCP error/EOF.

Server events: init timeout, auth result (allow / deny / timeout), keepalive
tick, keepalive miss, idle timeout, principal expiry (ADR-0008), lifetime
expiry, drain signal, drain slot deadline, connection quota breach,
rate-limit breach (post-warning), backpressure `disconnect` policy firing,
internal error.

### 3.3 Transition tables

No cell is blank. "Impossible" means the input cannot occur in that state by
construction; the table states the handling anyway so the implementation's
default arm is specified.

#### 3.3.1 `connecting`

No WebSocket exists; protocol messages and server protocol events are
impossible in this state. All handling is at the HTTP/TCP layer.

| Input | Next state | Outbound | Close code |
| --- | --- | --- | --- |
| Valid upgrade offering `graphql-transport-ws` | `awaiting_init` | HTTP 101 with subprotocol echo | none |
| Upgrade without acceptable subprotocol | `closed` | HTTP 400, body names supported subprotocol | none (HTTP) |
| Malformed HTTP request | `closed` | HTTP 400 | none (HTTP) |
| Non-upgrade `GET /graphql` | `closed` | HTTP 400 | none (HTTP) |
| fd-pressure load shed (FR-CONN-014) | `closed` | HTTP 503 | none (HTTP) |
| Slowloris deadline expiry | `closed` | none (TCP drop) | none |
| TCP error/EOF | `closed` | none | none |
| Any protocol message or binary frame | impossible (no WebSocket); bytes are HTTP and fall under "malformed HTTP request" | HTTP 400 | none |
| Any protocol server event | impossible (no protocol timers armed); reaching this arm is a bug counted by `conduit_protocol_impossible_transitions_total` and handled as internal error | none | none |

#### 3.3.2 `awaiting_init`

| Input | Next state | Outbound | Close code |
| --- | --- | --- | --- |
| `ConnectionInit` (valid; quota passes) | `authenticating` | none yet | none |
| `ConnectionInit` (valid; connection quota exceeded, FR-CONN-004) | `closing` | close with typed reason naming the limit class | 4703 |
| `Ping` | `awaiting_init` | `Pong` echoing payload (§5.2) | none |
| `Pong` | `awaiting_init` | none (counted as traffic; does not reset init timer) | none |
| `Subscribe` | `closing` | close, reason `"Unauthorized"` | 4401 |
| `Complete` | `closing` | close, reason `"Unauthorized"` | 4401 |
| Server-only type from client | `closing` | close, reason `"invalid message"` | 4400 |
| Invalid/unknown message | `closing` | close, typed reason echoing no client bytes (FR-SUB-008) | 4400 |
| Binary frame | `closing` | close, reason `"binary frames not supported"` | 4400 |
| Oversized message | `closing` | close, reason `"message too large"` | 4400 |
| Client close frame | `closing` | close echo per RFC 6455 | client's code |
| TCP error/EOF | `closed` | none | none |
| Init timeout (3 s) | `closing` | close, reason `"Connection initialisation timeout"` | 4408 |
| Drain signal | `closing` | close (no init yet; immediate, not paced) | 4700 |
| Rate-limit breach (post-warning) | `closing` | close, typed reason | 4400 |
| Internal error | `closing` | close | 1011 |
| Keepalive tick / keepalive miss / idle timeout / principal expiry / lifetime expiry / auth result / drain slot deadline / quota breach outside init / backpressure disconnect | impossible (timers not armed, no principal, no subscriptions); counted as impossible transition, handled as internal error | close | 1011 |

#### 3.3.3 `authenticating`

The init timer is cancelled on entry; the auth-mode deadline is armed.
Messages already queued behind `ConnectionInit` on the socket are processed
in order under these rows.

| Input | Next state | Outbound | Close code |
| --- | --- | --- | --- |
| `ConnectionInit` (second) | `closing` | close, reason `"Too many initialisation requests"` | 4429 |
| `Ping` | `authenticating` | `Pong` echoing payload | none |
| `Pong` | `authenticating` | none | none |
| `Subscribe` | `closing` | close, reason `"Unauthorized"` (ack not yet sent) | 4401 |
| `Complete` | `closing` | close, reason `"Unauthorized"` | 4401 |
| Server-only type from client | `closing` | close | 4400 |
| Invalid/unknown message | `closing` | close, typed reason | 4400 |
| Binary frame | `closing` | close, reason `"binary frames not supported"` | 4400 |
| Oversized message | `closing` | close, reason `"message too large"` | 4400 |
| Client close frame | `closing` | close echo | client's code |
| TCP error/EOF | `closed` | none (auth result discarded) | none |
| Auth result: allow | `ready` | `ConnectionAck` | none |
| Auth result: deny | `closing` | close, reason `"Forbidden"` — no detail about which check failed (FR-SUB-004, FR-AUTH-018) | 4403 |
| Auth result: timeout / malformed authorizer response (fail closed, FR-AUTH-003) | `closing` | close, reason `"Forbidden"` | 4403 |
| Drain signal | `closing` | close (immediate; no ack yet) | 4700 |
| Rate-limit breach (post-warning) | `closing` | close, typed reason | 4400 |
| Internal error | `closing` | close | 1011 |
| Init timeout | impossible (timer cancelled on `ConnectionInit`); impossible-transition counter, internal error | close | 1011 |
| Keepalive tick / keepalive miss / idle timeout / principal expiry / lifetime expiry / drain slot deadline / backpressure disconnect / quota breach | impossible (timers armed only in `ready`; principal not yet attached); impossible-transition counter, internal error | close | 1011 |

#### 3.3.4 `ready`

| Input | Next state | Outbound | Close code |
| --- | --- | --- | --- |
| `ConnectionInit` | `closing` | close, reason `"Too many initialisation requests"` | 4429 |
| `Ping` | `ready` | `Pong` echoing payload | none |
| `Pong` | `ready` | none (resets idle timer per FR-CONN-002) | none |
| `Subscribe` (new ID, within bounds and quota) | `ready` | per subscription sub-machine §4.11 | none |
| `Subscribe` (ID already active on this connection) | `closing` | close, reason `"Subscriber for <id> already exists"` — `<id>` is the client's ID, length-bounded and JSON-escaped | 4409 |
| `Subscribe` (ID > 255 bytes or empty string, §5.8) | `closing` | close, typed reason | 4400 |
| `Subscribe` (subscription quota exceeded, FR-CONN-005) | `ready` | `Error` on that ID with typed quota error; connection stays open | none |
| `Complete` (known ID) | `ready` | none in reply; sub-machine §4.11 tears down | none |
| `Complete` (unknown ID) | `ready` | none (ignored and counted, §5.14) | none |
| Server-only type from client | `closing` | close | 4400 |
| Invalid/unknown message | `closing` | close, typed reason | 4400 |
| Binary frame | `closing` | close | 4400 |
| Oversized message | `closing` | close, reason `"message too large"` | 4400 |
| Client close frame | `closing` | close echo; all subscription entries torn down | client's code |
| TCP error/EOF | `closed` | none | none |
| Keepalive tick (25 s) | `ready` | `Ping` (payload optional; carries `conduit.expiresInMs` inside the warning window, §4.9.4) | none |
| Keepalive miss (2 consecutive intervals with zero inbound frames) | `ready` | none; structured event emitted, `conduit_keepalive_miss_total` incremented; closure is idle timeout's job | none |
| Idle timeout (5 min without inbound traffic; pongs count) | `closing` | close, reason `"idle timeout"` | 4702 |
| Principal expiry (ADR-0008) | `closing` | `Error` (`TOKEN_EXPIRED`) on each live subscription, then close, reason `"Forbidden"` | 4403 |
| Grant revocation, principal fully revoked (ADR-0008) | `closing` | `Error` (`GRANT_REVOKED`) on affected subscriptions, then close | 4403 |
| Lifetime expiry (12 h ±10%, after warning ping) | `closing` | close, reason `"connection lifetime exceeded"`, retry-after hint | 4701 |
| Drain signal | `draining_connection` | none yet | none |
| Rate-limit breach (post-warning, FR-CONN-006) | `closing` | close, typed reason naming the limit class | 4400 |
| Backpressure `disconnect` policy fires (FR-CONN-008) | `closing` | close, reason `"slow consumer"` | 4704 |
| Connection quota breach (revoked mid-life quota reduction) | `closing` | close, typed reason | 4703 |
| Internal error | `closing` | close | 1011 |
| Init timeout / auth result / drain slot deadline | impossible in `ready`; impossible-transition counter, internal error | close | 1011 |

#### 3.3.5 `draining_connection`

Existing subscriptions continue delivering until the drain slot deadline;
the connection is scheduled into the paced 4700 schedule (FR-CONN-010).

| Input | Next state | Outbound | Close code |
| --- | --- | --- | --- |
| `ConnectionInit` | `closing` | close | 4429 |
| `Ping` | `draining_connection` | `Pong` echoing payload | none |
| `Pong` | `draining_connection` | none | none |
| `Subscribe` (any) | `draining_connection` | `Error` on that ID with typed `CONNECTION_DRAINING` error; no registration | none |
| `Complete` (known ID) | `draining_connection` | none; sub-machine teardown proceeds | none |
| `Complete` (unknown ID) | `draining_connection` | none (ignored, counted) | none |
| Server-only type from client | `closing` | close | 4400 |
| Invalid/unknown message | `closing` | close | 4400 |
| Binary frame | `closing` | close | 4400 |
| Oversized message | `closing` | close | 4400 |
| Client close frame | `closing` | close echo | client's code |
| TCP error/EOF | `closed` | none | none |
| Keepalive tick | `draining_connection` | `Ping` | none |
| Keepalive miss | `draining_connection` | structured event only | none |
| Idle timeout | `closing` | close | 4702 |
| Principal expiry | `closing` | `Error` per subscription, close | 4403 |
| Lifetime expiry | `closing` | close (drain already imminent; lifetime still wins if it fires first) | 4701 |
| Drain signal (repeat) | `draining_connection` | none (idempotent) | none |
| Drain slot deadline | `closing` | close, reason `"server draining"`, jittered retry-after hint (FR-RESUME-009) | 4700 |
| Rate-limit breach | `closing` | close | 4400 |
| Backpressure `disconnect` fires | `closing` | close | 4704 |
| Quota breach | `closing` | close | 4703 |
| Internal error | `closing` | close | 1011 |
| Init timeout / auth result | impossible; counter, internal error | close | 1011 |

#### 3.3.6 `closing`

The server has sent its close frame. All inbound protocol messages, binary
frames, oversized messages, and invalid messages are read, discarded, and
counted (`conduit_post_close_messages_total`); no outbound protocol message
is sent. The client's close echo, TCP error/EOF, or the closing grace
expiry (default 5 s) transitions to `closed` with TCP teardown. Server
events are all impossible (timers cancelled on entry) and fall to the
impossible-transition counter without further action.

#### 3.3.7 `closed`

Terminal. All inputs and events are impossible; any arrival indicates a
teardown race and is counted, never processed. Registry teardown is atomic
with respect to matching: no delivery is enqueued to a `closed` connection
and no subscription entry survives it (FR-CONN-001).

## 4. Message Reference

Every protocol message is a JSON object with a required `type` field.
Unknown **top-level** fields in an otherwise-valid message are ignored and
counted (`conduit_unknown_fields_total`, §5.16). Duplicate JSON keys are
rejected with `4400` (§5.17). Field presence rules below are normative;
"absent" means the key is omitted, not `null` — an explicit `null` where a
value is required is a wrong-type violation closing `4400`.

Direction key: C→S client to server, S→C server to client, C↔S both.

### 4.1 ConnectionInit

Direction: C→S. Legal states: `awaiting_init` only (first occurrence).
Second occurrence in any state closes `4429`.

```json
{
  "type": "connection_init",
  "payload": {
    "authorization": "Bearer eyJhbGciOiJSUzI1NiIs..."
  }
}
```

```go
type ConnectionInitMessage struct {
    Type    string         `json:"type"`              // required; exactly "connection_init"
    Payload map[string]any `json:"payload,omitempty"` // optional; if present, must be a JSON object
}
```

| Field | Type | Presence | Conduit bound |
| --- | --- | --- | --- |
| `type` | string | required | exact match `"connection_init"` |
| `payload` | object | optional | if present, must be an object (§5.3); contents passed opaquely to the auth handoff (FR-SUB-004); bounded only by the 512 KiB message bound; never logged (NFR-SEC-004) |

Misbehavior mapping: `payload` non-object → `4400`; second
`connection_init` → `4429`; arrives after the 3 s deadline → the connection
is already closed `4408`; credentials rejected → `4403` with no detail
(FR-AUTH-018).

### 4.2 ConnectionAck

Direction: S→C. Legal states: sent exactly once, on the
`authenticating → ready` transition.

```json
{
  "type": "connection_ack"
}
```

```go
type ConnectionAckMessage struct {
    Type    string         `json:"type"`              // required; exactly "connection_ack"
    Payload map[string]any `json:"payload,omitempty"` // optional; Conduit sends no payload in v1
}
```

Conduit sends `connection_ack` with no `payload` in v1; the protocol allows
an optional object and any future use is a versioned extension change
(§11). A client sending `connection_ack` is sending a server-only type:
close `4400`.

### 4.3 Ping

Direction: C↔S. Legal states: any state from `awaiting_init` onward, in
both directions.

```json
{
  "type": "ping",
  "payload": {
    "conduit": { "expiresInMs": 45000 }
  }
}
```

```go
type PingMessage struct {
    Type    string         `json:"type"`              // required; exactly "ping"
    Payload map[string]any `json:"payload,omitempty"` // optional object
}

// Server-sent ping payload extension (§4.9.4); absent outside the
// expiry-warning window.
type PingConduitExt struct {
    ExpiresInMs int64 `json:"expiresInMs"` // required within the ext; > 0
}
```

Server behavior: Conduit pings every 25 s in `ready` and
`draining_connection`. Client behavior expected: none required — the
reference client replies with `Pong` automatically; a client that never
pongs and sends nothing else hits the idle timeout (`4702`). A client
`Ping` at any legal state receives a `Pong` echoing the payload verbatim
(§5.2). Misbehavior: non-object `payload` → `4400`.

### 4.4 Pong

Direction: C↔S. Legal states: any state from `awaiting_init` onward.
Unsolicited `Pong` is legal and ignored (FR-SUB-007, §5.1, §5.15); it
counts as inbound traffic for the idle timer.

```json
{
  "type": "pong",
  "payload": { "reply": "ok" }
}
```

```go
type PongMessage struct {
    Type    string         `json:"type"`              // required; exactly "pong"
    Payload map[string]any `json:"payload,omitempty"` // optional object; ignored by Conduit
}
```

Conduit's reply `Pong` to a client `Ping` echoes the ping's payload
verbatim; Conduit imposes no payload requirement on client pongs and never
inspects their contents. Misbehavior: non-object `payload` → `4400`.

### 4.5 Subscribe

Direction: C→S. Legal states: `ready` only. In `awaiting_init` or
`authenticating` → `4401`; in `draining_connection` → id-scoped `Error`
(`CONNECTION_DRAINING`). Carries subscriptions **and** single-result
operations (queries and mutations) per the protocol; single-result
semantics are §5.11 and FR-GQL-015.

```json
{
  "id": "op-7f3a",
  "type": "subscribe",
  "payload": {
    "operationName": "OrdersEU",
    "query": "subscription OrdersEU($r: String!) { orderUpdated(region: $r) { id status } }",
    "variables": { "r": "eu" },
    "extensions": {
      "conduit": { "resume": { "token": "b64u.opaque.signed" } }
    }
  }
}
```

```go
type SubscribeMessage struct {
    ID      string           `json:"id"`      // required; 1..255 bytes; unique among active IDs on the connection
    Type    string           `json:"type"`    // required; exactly "subscribe"
    Payload SubscribePayload `json:"payload"` // required object
}

type SubscribePayload struct {
    OperationName *string        `json:"operationName,omitempty"` // optional; required when the document defines >1 operation (§5.12)
    Query         string         `json:"query"`                   // required; bounded by FR-GQL-011 (1 MiB doc, 20,000 tokens, parse depth) inside the 512 KiB message bound
    Variables     map[string]any `json:"variables,omitempty"`     // optional object; validated per FR-GQL-013
    Extensions    map[string]any `json:"extensions,omitempty"`    // optional object; "conduit" key per §4.9.5
}
```

| Field | Presence | Conduit bound and misbehavior |
| --- | --- | --- |
| `id` | required | empty string → `4400` (§5.8); > 255 bytes → `4400`; duplicate active ID → `4409`; reuse after terminal `Complete`/`Error` is legal (§5.7) |
| `payload` | required | absent or non-object → `4400` |
| `payload.query` | required | absent or non-string → `4400`; document-level bound violations → id-scoped `Error`, connection stays open (FR-GQL-011) |
| `payload.operationName` | conditional | non-string → `4400`; names no operation in the document → id-scoped `Error` (§5.12) |
| `payload.variables` | optional | non-object → `4400`; coercion failures → id-scoped `Error` (FR-GQL-013) |
| `payload.extensions` | optional | non-object → `4400`; unknown extension keys ignored and counted; malformed `conduit.resume` → §4.9.5 |

Structural violations of the message envelope close the connection
(`4400`/`4409`); violations of the GraphQL operation inside a
well-formed envelope produce an id-scoped `Error` and leave the connection
open. That boundary is normative and pinned by CONF-021 versus CONF-012.

### 4.6 Next

Direction: S→C. Legal states: `ready` and `draining_connection`, only for
an ID in sub-machine state `delivering` (or `completing` for
already-enqueued messages, §4.11). Payload is a spec-shaped
`ExecutionResult` (FR-SUB-006).

```json
{
  "id": "op-7f3a",
  "type": "next",
  "payload": {
    "data": { "orderUpdated": { "id": "o-991", "status": "PACKED" } },
    "extensions": {
      "conduit": {
        "resumePosition": "b64u.opaque.signed",
        "dropped": { "count": 3, "policy": "drop_oldest" }
      }
    }
  }
}
```

```go
type NextMessage struct {
    ID      string          `json:"id"`      // required; the subscribing ID
    Type    string          `json:"type"`    // required; exactly "next"
    Payload ExecutionResult `json:"payload"` // required
}

type ExecutionResult struct {
    Data       json.RawMessage `json:"data,omitempty"`       // present on success paths
    Errors     []GraphQLError  `json:"errors,omitempty"`     // field errors per the GraphQL spec
    Extensions map[string]any  `json:"extensions,omitempty"` // "conduit" key per §4.9.1
}
```

Outbound bound: a serialized `Next` exceeding the outbound message bound is
never sent; the event is dropped, counted, and reported via
`conduit.dropped` with policy `"oversized"` (§5.13). A client can never
legally send `next`: → `4400`.

### 4.7 Error

Direction: S→C. Legal states: `ready` and `draining_connection`. Terminal
for its ID: the payload is a **JSON array** of GraphQL errors, the
subscription entry moves to `errored`, and no further `Next` for that ID
follows (FR-SUB-006). The connection stays open.

```json
{
  "id": "op-7f3a",
  "type": "error",
  "payload": [
    {
      "message": "grant revoked",
      "extensions": { "code": "GRANT_REVOKED" }
    }
  ]
}
```

```go
type ErrorMessage struct {
    ID      string         `json:"id"`      // required
    Type    string         `json:"type"`    // required; exactly "error"
    Payload []GraphQLError `json:"payload"` // required; non-empty array
}

type GraphQLError struct {
    Message    string         `json:"message"`              // required; leaks no internals (FR-GQL-012)
    Path       []any          `json:"path,omitempty"`       // optional
    Locations  []ErrLocation  `json:"locations,omitempty"`  // optional
    Extensions map[string]any `json:"extensions,omitempty"` // "code" is always set by Conduit (NFR-MAINT-003)
}
```

Conduit error codes on this surface: `SUBSCRIBE_INVALID`,
`SUBSCRIBE_DENIED`, `TOKEN_EXPIRED`, `GRANT_REVOKED`, `QUOTA_EXCEEDED`,
`CONNECTION_DRAINING`, `FIELD_REMOVED`, `EXECUTION_ERROR`. The choice rule
between server `Error` and server `Complete` is §5.14. A client sending
`error` → `4400`.

### 4.8 Complete

Direction: C↔S. Legal states: `ready` and `draining_connection`.

- C→S: requests the server stop the operation. The server stops promptly, frees the entry (FR-SUB-006), and sends **no** reply message. `Next` messages already enqueued at processing time may still be written (§4.11); no new `Next` is enqueued afterward. Unknown ID: ignored and counted (§5.14a).
- S→C: signals normal end of the event stream for that ID (source completed, or single-result operation finished per §5.11).

```json
{
  "id": "op-7f3a",
  "type": "complete"
}
```

```go
type CompleteMessage struct {
    ID   string `json:"id"`   // required; 1..255 bytes
    Type string `json:"type"` // required; exactly "complete"
}
```

Misbehavior: missing or non-string `id` → `4400`; a `payload` key present
is an unknown top-level field: ignored and counted (§5.16).

### 4.9 Conduit extension positions

Conduit-specific data rides only in spec-sanctioned extension positions
(PRODUCT_REQUIREMENTS §5.4): `Next.payload.extensions.conduit`,
`Subscribe.payload.extensions.conduit`, and `Ping.payload.conduit`. Field
names in the `conduit` namespace are camelCase on the wire; this document
controls the wire spelling (README conflict rule 5) and normalizes the
warning field ADR-0008 sketched in prose to `expiresInMs`. All extension
objects follow the versioning rules in §11.

#### 4.9.1 `Next.payload.extensions.conduit` (owning gates: R7 resume fields, R6 dropped behavior; wire shape R2)

```go
type NextConduitExt struct {
    ResumePosition string         `json:"resumePosition,omitempty"` // opaque signed token, <= 512 bytes (FR-RESUME-001/002); present on every delivered Next once R7 lands
    Dropped        *DroppedNotice `json:"dropped,omitempty"`        // present on the first Next after policy-caused drops (FR-CONN-009)
    ResumeGap      *ResumeGap     `json:"resumeGap,omitempty"`      // present at most once per subscription, before live delivery (FR-RESUME-005)
    ResumeRejected *ResumeReject  `json:"resumeRejected,omitempty"` // present at most once, on the first Next of a fresh-start subscription (FR-RESUME-007)
}

type DroppedNotice struct {
    Count  int64  `json:"count"`  // required; >= 1
    Policy string `json:"policy"` // required; "drop_oldest" | "coalesce_by_key" | "oversized"
}

type ResumeGap struct {
    FromPosition string `json:"fromPosition"` // required; the position the client requested
    ToPosition   string `json:"toPosition"`   // required; the first position actually served
    Reason       string `json:"reason"`       // required; "horizon_passed" | "epoch_mismatch" | "no_coverage"
}

type ResumeReject struct {
    Reason string `json:"reason"` // required; "bad_signature" | "wrong_tenant" | "wrong_field" | "malformed" | "oversized" | "expired"
}
```

#### 4.9.2 `resumeGap` semantics

Sent before live delivery begins when replay cannot cover the requested
position (FR-RESUME-005): the buffer horizon passed, the node epoch does
not match, or the serving node has no coverage for the field. Conduit never
fabricates completeness (ADR-0007). `resumeGap` rides on the first `Next`;
if no event arrives within the gap-notice deadline (default 5 s), Conduit
synthesizes a `Next` whose payload is `{"data": null}` with only the
extension attached, so the notice is never starved by a quiet field. That
synthesized message is a Conduit decision pinned by CONF-038.

#### 4.9.3 `resumeRejected` semantics

An invalid resume token never closes the connection and never fails the
subscribe: the subscription proceeds as fresh with the notice attached
(same synthesis rule as §4.9.2), and forgery-shaped rejections
(`bad_signature`) are logged server-side (FR-RESUME-007, NFR-SEC-007).

#### 4.9.4 `Ping.payload.conduit` (wire shape R2; enforcement R3)

`{ "expiresInMs": <int64 > 0> }` — attached to the server keepalive ping
when the principal's expiry is within the warning window (default 60 s,
ADR-0008). Well-behaved clients reconnect with fresh credentials and resume
tokens; clients that ignore it are cut at expiry with `Error`
(`TOKEN_EXPIRED`) per subscription and close `4403`.

#### 4.9.5 `Subscribe.payload.extensions.conduit` (owning gate R7)

```go
type SubscribeConduitExt struct {
    Resume *ResumeRequest `json:"resume,omitempty"`
}

type ResumeRequest struct {
    Token string `json:"token"` // required within resume; opaque bytes, <= 512 bytes
}
```

A `resume` object without a `token`, a non-string token, or a token over
512 bytes is a malformed resume request: handled as `resumeRejected`
(`malformed` / `oversized`), never a `4400` — the envelope is well-formed
and the extension is best-effort by design.

#### 4.9.6 Reference-client invisibility invariant

An unmodified reference `graphql-ws` client that ignores every `conduit`
extension remains fully functional: connect, subscribe, receive, complete,
keepalive, and every documented error path work identically with the
extensions present (NFR-COMPAT-001). The conformance suite proves it two
ways: (a) CONF-035 runs the full reference-client scenario set against a
server emitting all extensions on every eligible message and asserts
byte-identical client-visible behavior versus a server with extensions
suppressed; (b) every reference-client row in §7 runs with extensions
enabled, so invisibility is load-bearing across the whole suite, not one
test.

### 4.10 Client-sent server-only types

`connection_ack`, `next`, and `error` from a client are protocol
violations in every state: close `4400`, reason `"invalid message"`. The
reason string never echoes client bytes (FR-SUB-008).

### 4.11 Subscription Lifecycle Sub-Machine

Each subscription ID on a connection runs its own sub-machine, keyed by
`(connection, id, generation)`; `generation` is a server-side counter that
makes ID reuse race-free (§5.7). Status: `planned`; owning gate R2 (resume
splice rows R7).

| State | Meaning |
| --- | --- |
| `requested` | `Subscribe` received; envelope validated; document parsed within bounds. |
| `authorizing` | Subscribe-time authorization in flight at `SubscriptionAuthorizer.AuthorizeSubscribe` (FR-AUTH-006); predicates compiling (FR-FILT-001). |
| `registered` | Entry registered in the connection registry and predicate index; no event delivered yet. Replay (if resuming) begins here. |
| `delivering` | At least one `Next` enqueued; live or replay delivery in progress. |
| `completing` | Teardown in flight: unregistering from the index, discarding queued messages for this generation. |
| `completed` | Terminal, normal end. ID is reusable (freed at the moment the terminal message was processed, §5.7). |
| `errored` | Terminal via server `Error`. ID is reusable. |

Transitions (every transition enumerated; anything not listed is
impossible and counted):

| From | Trigger | To | Outbound |
| --- | --- | --- | --- |
| `requested` | envelope valid, operation is subscription | `authorizing` | none |
| `requested` | envelope valid, single-result operation (§5.11) | `authorizing` | none |
| `requested` | document bound violation / parse failure / unknown operationName | `errored` | `Error` (`SUBSCRIBE_INVALID`) |
| `authorizing` | authorized; predicates compile; resume token absent | `registered` | none |
| `authorizing` | authorized; valid resume token | `registered` | replay scheduled from token position (FR-RESUME-004) |
| `authorizing` | authorized; invalid resume token | `registered` | fresh start; `resumeRejected` attached to first `Next` (§4.9.3) |
| `authorizing` | denied | `errored` | `Error` (`SUBSCRIBE_DENIED`) — no rule detail (FR-AUTH-018) |
| `authorizing` | predicate compile/type failure (FR-FILT-001/004) | `errored` | `Error` (`SUBSCRIBE_INVALID`) |
| `authorizing` | quota exceeded (FR-CONN-005) | `errored` | `Error` (`QUOTA_EXCEEDED`) |
| `authorizing` | client `Complete` arrives | `completing` | none; authorization result discarded, nothing registered |
| `registered` | first matched, authorized event (replay or live) | `delivering` | `Next` |
| `registered` | resume with gap and quiet field (gap-notice deadline) | `delivering` | synthesized `Next` with `resumeGap` (§4.9.2) |
| `registered` | client `Complete` | `completing` | none |
| `registered` | source completes with zero events | `completed` | `Complete` |
| `registered` | publish-time denial escalating to termination (revocation/expiry) | `errored` | `Error` (`GRANT_REVOKED` / `TOKEN_EXPIRED`) |
| `delivering` | matched, authorized event | `delivering` | `Next` |
| `delivering` | replay exhausted, splice to live (FR-RESUME-004) | `delivering` | continuous `Next`, no duplicate, no gap at the splice point |
| `delivering` | client `Complete` (races in-flight `Next`) | `completing` | none; `Next` messages already in the outbound queue may still be written; no new enqueue after the `Complete` is processed in socket order |
| `delivering` | source completes normally | `completed` | `Complete` |
| `delivering` | server error rule fires (§5.14) | `errored` | `Error` |
| `delivering` | schema reload removes/changes the field (FR-OPS-003) | `errored` | `Error` (`FIELD_REMOVED`) |
| `delivering` | oversized `Next` (§5.13) | `delivering` | event dropped; `dropped{policy:"oversized"}` on next delivered message |
| `completing` | teardown finishes (index unregistration, queue purge for this generation) | `completed` | none |
| any non-terminal | connection leaves `ready`/`draining_connection` (close, any code) | `completed` | none; connection close is the terminal signal, no per-ID messages (§3.3) |

Race rules, normative:

- Messages on one connection are processed in strict socket order, so a client that sends `Complete` then `Subscribe` with the same ID always observes the ID as free (§5.7).
- A queued `Next` belonging to an older generation of a reused ID is discarded during `completing`, never delivered under the new generation.
- Client `Complete` racing a server terminal (`Complete`/`Error`) is benign in both orders: the second terminal event finds the entry terminal or `completing` and is a counted no-op.
- During connection drain, per-ID terminal messages are **not** sent; the `4700` close is the terminal signal for every live ID, and the resume token from the last delivered `Next` is the continuity mechanism.

## 5. Ambiguity Register

The `graphql-transport-ws` protocol document is short and leaves real
behavior undefined. Every silence Conduit fills is recorded here: the
ambiguity, the options, the Conduit decision, the rationale, and the
conformance test that pins it. Decisions here are Conduit's, not the
spec's; a future protocol revision that resolves one differently requires a
new ADR and a major or minor contract review per §11. All entries:
`planned`.

### 5.1 Pong without a preceding Ping

- **Ambiguity**: the protocol says `Pong` "can be sent at any time" and may be unsolicited, but does not say what a server should do with one.
- **Options**: ignore; close as protocol error; track against outstanding pings.
- **Decision**: legal and ignored in any post-handshake state; counts as inbound traffic for the idle timer; never correlated to a specific ping.
- **Rationale**: FR-SUB-007 mandates it; correlation adds state for zero protocol value.
- **Pinned by**: CONF-018.

### 5.2 Ping payload echo semantics in Pong

- **Ambiguity**: the protocol permits payloads on both messages but does not require the pong to echo the ping's payload.
- **Options**: echo verbatim; reply with empty pong; reply with server-chosen payload.
- **Decision**: Conduit's reply `Pong` echoes the client ping's payload verbatim (already bounded by the 512 KiB inbound bound). Conduit imposes no echo requirement on client pongs and never inspects them.
- **Rationale**: verbatim echo lets clients implement RTT probes and liveness correlation with zero server state; the payload was already accepted under the inbound bound, so echoing it cannot amplify.
- **Pinned by**: CONF-016.

### 5.3 `connection_init` payload size and shape bounds

- **Ambiguity**: the protocol allows an optional payload but bounds nothing.
- **Options**: unbounded; object-typed with message-level bound; dedicated smaller bound.
- **Decision**: if present, `payload` must be a JSON object (non-object → `4400`); its size is bounded only by the 512 KiB message bound; contents are opaque to the state machine and passed to the auth handoff without logging.
- **Rationale**: credentials legitimately vary in size (JWT chains); NFR-SEC-001 is satisfied by the message bound; a second bound would be a second knob with no attack it uniquely stops.
- **Pinned by**: CONF-005, HOST-003.

### 5.4 Messages in `awaiting_init` other than `connection_init`

- **Ambiguity**: the protocol defines `4401` for `Subscribe` before ack but is silent on `Ping`, `Pong`, and `Complete` before init.
- **Options**: close `4401` for everything; allow keepalive traffic only.
- **Decision**: `Ping` and `Pong` are legal in `awaiting_init` and `authenticating` (the protocol says they may be sent "at any time"); they do not reset the init timer. `Subscribe` and `Complete` close `4401`. Server-only types close `4400`.
- **Rationale**: keepalive legality is the protocol's stated intent; operation traffic before authentication is an authorization boundary and fails closed. Treating pre-ack `Complete` like `Subscribe` keeps the rule "no operation-scoped messages before ack" simple.
- **Pinned by**: CONF-008 (subscribe), scripted variant for complete in the same fixture.

### 5.5 `Subscribe` before `ConnectionAck`

- **Ambiguity**: none on the outcome — the protocol names `4401` Unauthorized — but the spec does not say whether the pending auth result is awaited.
- **Options**: buffer the subscribe until ack; close immediately.
- **Decision**: close `4401` immediately, including during `authenticating`.
- **Rationale**: buffering pre-ack operations creates an unauthenticated queue an attacker can fill; the reference client never subscribes before ack, so nothing interoperable is lost.
- **Pinned by**: CONF-008.

### 5.6 Duplicate `connection_init`

- **Ambiguity**: none on the outcome (`4429: Too many initialisation requests`); silent on whether it applies after ack.
- **Options**: `4429` only during init; `4429` in all post-init states.
- **Decision**: a second `connection_init` closes `4429` in `authenticating`, `ready`, and `draining_connection` alike.
- **Rationale**: one uniform rule; there is no legal re-authentication in this protocol (ADR-0008 records reconnect-based refresh instead).
- **Pinned by**: CONF-007.

### 5.7 Subscription ID reuse after Complete, and the teardown race

- **Ambiguity**: the protocol requires IDs "unique among active operations" but does not define when an ID stops being active, nor what happens when reuse races teardown.
- **Options**: ID freed when teardown fully finishes (reuse can fail `4409` nondeterministically); ID freed at terminal-message processing with generation-tagged teardown.
- **Decision**: reuse after a terminal event is **allowed and is a new subscription**. The ID is freed at the instant the terminal message (client `Complete`, server `Complete`, server `Error`) is processed in socket order; internal teardown continues under a generation number, and queued `Next` messages from the old generation are discarded, never delivered under the reused ID.
- **Rationale**: socket-order determinism means a client that completes then resubscribes with the same ID can never draw `4409` from its own race; generation tagging keeps at-most-once (ADR-0007) intact across reuse.
- **Pinned by**: CONF-014, CONF-015.

### 5.8 Empty-string ID

- **Ambiguity**: the protocol types `id` as a string without forbidding `""`.
- **Options**: accept; reject.
- **Decision**: `""` is invalid: close `4400` with reason `"empty subscription id"`. This is a Conduit restriction, not a spec rule.
- **Rationale**: an empty ID is indistinguishable from an absent field in logs, metrics labels, and admin output, and the reference client always generates non-empty IDs, so nothing interoperable is rejected.
- **Pinned by**: HOST-014 (boundary set includes `""`).

### 5.9 Non-object `payload` where an object is required

- **Ambiguity**: the protocol shows object payloads but does not state the outcome for `"payload": 42` or `"payload": null`.
- **Options**: coerce/ignore; id-scoped error; connection close.
- **Decision**: wrong-typed required structure is an envelope violation: close `4400`. Explicit `null` where a value is required is wrong-typed.
- **Rationale**: envelope violations indicate a broken client, not a broken operation; FR-SUB-008 groups wrong field types with malformed frames.
- **Pinned by**: CONF-021 fixture family.

### 5.10 Binary frames

- **Ambiguity**: the protocol assumes JSON text and never mentions binary opcodes.
- **Options**: WS-layer close `1003` (unacceptable data); protocol-level `4400`.
- **Decision**: Conduit-level close `4400`, reason `"binary frames not supported"` (Conduit policy).
- **Rationale**: keeping every deliberate close inside the documented table (FR-SUB-010) makes client retry logic uniform; `1003` would be a second, library-dependent surface.
- **Pinned by**: CONF-025, HOST-011.

### 5.11 Queries and mutations over the socket: single-result semantics

- **Ambiguity**: the protocol allows single-result operations via `Subscribe` but implementations differ on the message sequence.
- **Options**: one `Next` then `Complete`; `Next` with inline terminal flag; `Complete` carrying the result.
- **Decision**: exactly one `Next` carrying the full `ExecutionResult` (including any field errors), followed by one `Complete`. A request failure before execution yields `Error` with no `Next`. Execution shares the HTTP executor, limits, and authorization path (FR-GQL-015).
- **Rationale**: this is the sequence the reference client implements; anything else fails interop.
- **Pinned by**: CONF-019, CONF-020.

### 5.12 `operationName` selection with multiple operations

- **Ambiguity**: delegated implicitly to the GraphQL spec; the protocol does not say whether a violation is a connection or operation failure.
- **Options**: close `4400`; id-scoped `Error`.
- **Decision**: a document with multiple operations and a missing or non-matching `operationName` produces an id-scoped `Error` (`SUBSCRIBE_INVALID`) with GraphQL locations; the connection stays open.
- **Rationale**: the envelope is well-formed; this is an operation-level GraphQL validation failure per the October 2021 spec (NFR-COMPAT-002).
- **Pinned by**: CONF-012 fixture family.

### 5.13 `Next` serialization exceeding outbound bounds

- **Ambiguity**: the protocol never contemplates a server-side message too large to send.
- **Options**: send anyway (breaks symmetric bounds); close `1009`; terminate the subscription with `Error`; drop the event with an honest notice.
- **Decision**: drop the single oversized event, count it (`conduit_oversized_next_dropped_total`), and report it on the next delivered message via `conduit.dropped` with policy `"oversized"`. The subscription and connection survive.
- **Rationale**: honest failure over implied completeness (PRODUCT_REQUIREMENTS §3.5): one pathological event should not kill a healthy subscription, and silence would fabricate completeness.
- **Pinned by**: CONF-040 fixture family (oversized variant).

### 5.14 Server-initiated `Complete` versus `Error`

- **Ambiguity**: the protocol defines both terminals but no rule for choosing.
- **Options**: implementation whim; a fixed rule.
- **Decision**: fixed rule — `Error` whenever termination represents a fault or denial (subscribe validation failure, authorization denial, revocation, expiry, quota, schema-reload field removal, execution error, drain-time subscribe rejection); `Complete` only when the event stream ends normally (source completion, single-result operation finished, client-requested stop). `Error` payloads always carry `extensions.code`.
- **Rationale**: clients need a deterministic signal for "resubscribe won't help as-is" (`Error` + code) versus "the stream simply ended" (`Complete`).
- **Pinned by**: CONF-011 versus CONF-012.

### 5.14a `Complete` for an unknown ID

- **Ambiguity**: the protocol does not state the outcome.
- **Options**: close `4400`; ignore.
- **Decision**: ignored and counted (`conduit_unknown_complete_total`).
- **Rationale**: client `Complete` legitimately races server terminals (§4.11); punishing the losing side of a legal race would make correct clients fail nondeterministically.
- **Pinned by**: CONF-010 fixture family (post-terminal complete variant).

### 5.15 Unsolicited `Pong`

- **Ambiguity**: explicitly legal in the protocol ("may even be sent unsolicited"), but idle-timer interaction is unspecified.
- **Decision**: ignored; counts as inbound traffic for the idle timer (FR-CONN-002 "pongs count"); a client may therefore hold a quiet connection open with periodic unsolicited pongs, subject to the inbound rate limit.
- **Rationale**: this is the cheapest client-side keepalive and the behavior the reference client relies on.
- **Pinned by**: CONF-018, CONF-029.

### 5.16 Unknown top-level fields in otherwise-valid messages

- **Ambiguity**: the protocol neither forbids nor blesses extra fields.
- **Options**: close `4400` (strict); ignore silently; ignore and count.
- **Decision**: ignore and count (`conduit_unknown_fields_total`, bounded cardinality: counter per message type, not per field name).
- **Rationale**: strictness here would break forward compatibility with future protocol revisions and with clients adding diagnostic fields; silent ignoring would hide a misconfigured client fleet from operators. Counting makes drift visible without breaking interop.
- **Pinned by**: CONF-028.

### 5.17 Duplicate JSON keys

- **Ambiguity**: JSON itself (RFC 8259) leaves duplicate-key behavior undefined; the protocol inherits the hole.
- **Options**: last-wins (Go default); first-wins; reject.
- **Decision**: reject: a message with duplicate keys at any nesting level closes `4400`, reason `"duplicate json key"`.
- **Rationale**: duplicate keys are a classic parser-differential attack (one component sees the first value, another the second); Conduit's bounded decoder rejects them so no two components can disagree (NFR-SEC-001).
- **Pinned by**: CONF-027, HOST-001 corpus.

### 5.18 Invalid UTF-8 in a text message

- **Ambiguity**: RFC 6455 prescribes failing the connection (the library would use `1002`/`1007`); the protocol document says nothing.
- **Options**: let the WebSocket library close `1002`/`1007`; validate in Conduit's bounded reader and close `4400`.
- **Decision**: Conduit validates UTF-8 in its bounded reader before JSON decoding and closes `4400`, reason `"invalid utf-8"`. The library-level `1002` remains only as a backstop for frames Conduit's reader never saw.
- **Rationale**: same reasoning as §5.10 — one documented close surface (FR-SUB-010); Conduit-level validation also guarantees the check happens before any allocation-proportional processing (NFR-SEC-001) regardless of library behavior.
- **Pinned by**: CONF-026, HOST-015.

## 6. Close-Code Reference

Every close uses this table; no code outside it is ever sent deliberately,
and the conformance suite enumerates all of them (FR-SUB-010). Retry
semantics vocabulary: `retry-with-backoff` (same credentials, jittered
exponential backoff), `reconnect-with-new-credentials`,
`reconnect-later-with-resume` (reconnect with resume tokens, honoring any
retry-after hint), `do-not-retry` (a client bug or permanent condition;
automated retry is wrong).

| Code | Name | Sender | Trigger | Client retry semantics |
| --- | --- | --- | --- | --- |
| 1000 | Normal closure | either | client finished; or server closing a completed session cleanly | do-not-retry (reconnect only on new demand) |
| 1001 | Going away | either | endpoint leaving (browser tab close; abrupt server exit path where 4700 was not reachable) | reconnect-later-with-resume |
| 1002 | Protocol error | WS library (backstop) | WebSocket-layer violation Conduit's reader never saw | do-not-retry |
| 1009 | Message too big | WS library (backstop) | frame exceeding the library limit; must be unreachable while the Conduit 512 KiB check works (§2.3) | do-not-retry |
| 1011 | Internal error | server | unexpected server fault, including impossible-transition arms | retry-with-backoff |
| 4400 | Bad Request | server | malformed JSON, unknown type, missing/wrong-typed fields, duplicate keys, invalid UTF-8, binary frame, oversized message, empty/oversized subscription ID, rate-limit abuse after warning | do-not-retry |
| 4401 | Unauthorized | server | `Subscribe`/`Complete` before `connection_ack` | do-not-retry (fix client ordering) |
| 4403 | Forbidden | server | authentication rejected at init; principal expiry; full revocation (ADR-0008) | reconnect-with-new-credentials |
| 4406 | Subprotocol not acceptable | server | WebSocket established without `graphql-transport-ws` agreed (ADR-0002) | do-not-retry |
| 4408 | Connection initialisation timeout | server | no `connection_init` within 3 s | retry-with-backoff |
| 4409 | Subscriber for `<id>` already exists | server | duplicate active subscription ID | do-not-retry |
| 4429 | Too many initialisation requests | server | second `connection_init` | do-not-retry |
| 4700 | Server draining | server | paced drain close during shutdown/deploy (FR-CONN-010) | reconnect-later-with-resume |
| 4701 | Connection lifetime exceeded | server | 12 h ±10% lifetime reached after warning ping (FR-CONN-003) | reconnect-later-with-resume |
| 4702 | Idle timeout | server | 5 min without inbound traffic, pongs counting (FR-CONN-002) | retry-with-backoff |
| 4703 | Quota exceeded | server | connection quota per principal/tenant breached at init or by mid-life quota reduction (FR-CONN-004) | retry-with-backoff |
| 4704 | Slow consumer disconnected | server | `disconnect` backpressure policy fired (FR-CONN-008) | reconnect-later-with-resume |

### 6.1 Conduit-assigned codes, prose

- **4700 Server draining.** Sent on a paced schedule across the drain window (default 60 s) so a node's connections do not stampede the fleet. The close reason carries a jittered retry-after hint (FR-RESUME-009). Clients should reconnect through the load balancer with resume tokens; remaining capacity serves them. Sent immediately (unpaced) to connections still in `awaiting_init`/`authenticating`, which hold no resumable state.
- **4701 Connection lifetime exceeded.** Bounds credential and resource lifetime on long-lived sockets. The ±10% jitter prevents co-created cohorts (a deploy's reconnect wave) from expiring simultaneously; a warning ping precedes the close so clients can reconnect on their own schedule. Carries a retry-after hint.
- **4702 Idle timeout.** Fires only when the client has sent nothing — not even a pong — for the idle window. Because the server pings every 25 s and the reference client auto-pongs, a healthy reference client never sees 4702; observing one in the wild indicates a half-open connection or a client that suppressed pongs.
- **4703 Quota exceeded.** Refers to connection-level quotas only; subscription quotas are id-scoped `Error` messages, not closes (FR-CONN-005). The reason names the limit class, never the limit value where that would aid abuse (PRODUCT_REQUIREMENTS §8).
- **4704 Slow consumer disconnected.** The `disconnect` backpressure policy's close. Delivered with a best-effort close frame; a consumer too slow to read the close frame still gets TCP teardown at the closing grace. The client should fix its consumption rate before reconnecting with resume tokens; the replay buffer will cover what the horizon allows and announce a `resumeGap` for the rest.

## 7. Conformance Suite Design

Status: `planned`. Owning gate R2 except rows marked R6/R7.

### 7.1 Architecture

A Go test harness (`test/conformance`) drives three client drivers against
a Conduit node running with the in-process bus and an injected clock:

- **ref**: the unmodified, pinned `graphql-ws` client in Node, inside a container fixture (§10), driven over a stdin/stdout JSON-lines control channel; the client library code is never patched or monkey-patched.
- **scr**: a Conduit-owned scripted protocol client in Go with byte-level control over frames, fragmentation, timing, and malformed payloads.
- **host**: the hostile client (§8), a scripted-client extension whose scenarios are adversarial by construction.

Flake policy classes: **D** (deterministic: injected clock, in-process
transport where possible, zero retries — any flake is a release-blocking
bug per NFR-MAINT-006); **C** (container interop: one retry permitted,
every flake recorded; a test exceeding 1% weekly flake rate is quarantined
and its owning gate is blocked until it is fixed — quarantine is never
acceptance).

### 7.2 Scenario catalogue

| Test ID | Scenario | First failing condition | Required passing assertion | Harness | Runtime budget | CI or nightly | Flake policy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| CONF-001 | Upgrade offering `graphql-transport-ws` | server exists, handshake path absent | 101 with subprotocol echoed exactly once | scr | 1 s | CI | D |
| CONF-002 | Upgrade with no subprotocol | rejection path absent (upgrade accepted) | HTTP 400, body names supported subprotocol, no WS frames sent | scr | 1 s | CI | D |
| CONF-003 | Upgrade offering only legacy `graphql-ws` string, real legacy client fixture | legacy accepted | HTTP 400 pre-handshake per ADR-0002 | ref | 10 s | CI | C |
| CONF-004 | WS established without agreed subprotocol (forced library edge) | frame processed before subprotocol check | close 4406 before any protocol message is read | scr | 1 s | CI | D |
| CONF-005 | `connection_init` with object payload → ack | init handling absent | exactly one `connection_ack`, payload absent, within budget | ref | 5 s | CI | C |
| CONF-006 | No init within 3 s (injected clock) | timer absent (connection lingers) | close 4408 at exactly the deadline tick | scr | 1 s | CI | D |
| CONF-007 | Second `connection_init` (before and after ack) | second init tolerated | close 4429 in both variants | scr | 1 s | CI | D |
| CONF-008 | `Subscribe` and `Complete` before ack | pre-ack operation accepted | close 4401 in both variants | scr | 1 s | CI | D |
| CONF-009 | Subscribe, publish matching event, receive | delivery path absent | `Next` with spec-shaped `ExecutionResult`, correct `id` | ref | 10 s | CI | C |
| CONF-010 | Client `Complete` stops delivery; repeat for already-terminal ID | delivery continues after complete | no `Next` enqueued after in-order processing; unknown-id variant ignored and counted | scr | 2 s | CI | D |
| CONF-011 | Source completes normally | terminal missing | server `Complete`, entry freed, ID reusable | scr | 2 s | CI | D |
| CONF-012 | Invalid operation in valid envelope (parse failure; multi-op doc without `operationName`) | connection closed instead | id-scoped `Error` with `extensions.code`, connection stays open | ref | 10 s | CI | C |
| CONF-013 | Duplicate active ID | duplicate registered | close 4409 with bounded, escaped `<id>` in reason | scr | 1 s | CI | D |
| CONF-014 | ID reuse after clean `Complete` | reuse rejected | new subscription registered and delivering | scr | 2 s | CI | D |
| CONF-015 | Same-socket `Complete` then `Subscribe` same ID while old generation tears down | 4409 from own race, or stale `Next` under new ID | reuse always succeeds; zero old-generation deliveries across 1,000 iterations under race detector | scr | 30 s | CI | D |
| CONF-016 | Client `Ping` with payload | no pong or payload not echoed | `Pong` echoing payload verbatim, in every post-handshake state | scr | 1 s | CI | D |
| CONF-017 | Server keepalive at 25 s ticks (injected clock) | no server pings | `Ping` per tick; reference client auto-pongs; connection outlives 100 intervals | ref | 15 s | CI | C |
| CONF-018 | Unsolicited `Pong` stream | closed as protocol error | ignored, counted, idle timer reset each time | scr | 1 s | CI | D |
| CONF-019 | Query over WS | wrong sequence | exactly one `Next` (full result) then `Complete` | ref | 10 s | CI | C |
| CONF-020 | Mutation over WS with publish mapping | mutation path diverges from HTTP | one `Next` + `Complete`; identical result to HTTP execution of same document | scr | 5 s | CI | D |
| CONF-021 | Malformed JSON; missing `type`; wrong-typed `payload`; explicit `null` payload | any variant tolerated | close 4400, reason echoes no client bytes | scr | 2 s | CI | D |
| CONF-022 | Unknown `type` value | tolerated or crash | close 4400 | scr | 1 s | CI | D |
| CONF-023 | Client sends `connection_ack`, `next`, `error` | server-only type processed | close 4400 for each | scr | 1 s | CI | D |
| CONF-024 | Message at 512 KiB (accepted) and 512 KiB + 1 (closed) | bound absent or off-by-one | boundary message delivered; +1 closes 4400 `"message too large"`; 1009 never observed | scr | 5 s | CI | D |
| CONF-025 | Binary frame in each post-handshake state | binary processed | close 4400 `"binary frames not supported"` | scr | 1 s | CI | D |
| CONF-026 | Invalid UTF-8 text message | library 1002/1007 or crash | Conduit close 4400 `"invalid utf-8"` | scr | 1 s | CI | D |
| CONF-027 | Duplicate JSON keys at top level and nested | last-wins parsing | close 4400 `"duplicate json key"` | scr | 1 s | CI | D |
| CONF-028 | Valid message with unknown top-level field | rejected | processed normally; `conduit_unknown_fields_total` incremented | scr | 1 s | CI | D |
| CONF-029 | Silence except unsolicited pongs, then full silence (injected clock) | idle closes despite pongs, or never closes | alive past 5 min while ponging; close 4702 exactly 5 min after last inbound | scr | 2 s | CI | D |
| CONF-030 | Lifetime expiry across 1,000 simulated connections (injected clock) | no close, or synchronized closes | 4701 after warning ping; close times spread within ±10% jitter band | scr | 10 s | CI | D |
| CONF-031 | Node drain with 100 connections | stampede or no closes | paced 4700 with retry-after hints across drain window; `Subscribe` during drain gets `Error` `CONNECTION_DRAINING` | scr | 10 s | CI | D |
| CONF-032 | Connection quota breach at init | over-quota connection acked | close 4703 naming limit class, not value | scr | 1 s | CI | D |
| CONF-033 | 50 msg/s sustained (allowed) vs burst-exhausting flood | premature or absent close | warning then 4400-class close only after bucket exhausted; compliant rate never closed | scr | 10 s | CI | D |
| CONF-034 | `disconnect` policy under stalled reads | unbounded buffering | close 4704 at queue bound; memory within per-connection budget | scr | 10 s | CI | D |
| CONF-035 | Full reference scenario set with all `conduit` extensions emitted vs suppressed | client-visible divergence | identical reference-client observable behavior both runs (NFR-COMPAT-001) | ref | 60 s | CI | C |
| CONF-036 | Principal expiry (stub auth, injected clock) | delivery after expiry | warning ping carries `expiresInMs`; at expiry `Error` `TOKEN_EXPIRED` per subscription then close 4403 (enforcement gate R3) | scr | 2 s | CI | D |
| CONF-037 | Reconnect with valid resume token mid-stream (gate R7) | duplicate or gap at splice | replay after token position, splice to live, per-publisher order, zero duplicate/gap over 10,000 events | scr | 30 s | CI | D |
| CONF-038 | Resume past buffer horizon; quiet-field variant (gate R7) | fabricated completeness | `resumeGap` with correct range before live delivery; synthesized `Next` on quiet field at deadline | scr | 5 s | CI | D |
| CONF-039 | Tampered/foreign/oversized/expired resume tokens (gate R7) | close, subscribe failure, or replay honored | fresh start with `resumeRejected` reason per variant; forgery logged; constant-time verification (NFR-SEC-007) | scr | 5 s | CI | D |
| CONF-040 | `drop_oldest` under burst; oversized-`Next` variant (gate R6) | silent drops | drops counted; `conduit.dropped {count, policy}` on next delivered message, policy `"oversized"` in variant | scr | 10 s | CI | D |
| CONF-041 | Client closes 1000/1001 mid-subscription; TCP RST mid-`Subscribe` processing | orphan entries or delivery to closed connection | registry and index entries released atomically; zero deliveries post-close under race detector | scr | 10 s | CI | D |
| CONF-042 | Injected internal fault on delivery path | wrong close code or hang | close 1011; other connections unaffected | scr | 2 s | CI | D |
| CONF-043 | Reference client end-to-end error paths: server `Error`, 4409, mid-subscribe server kill and reconnect | client library misinterprets Conduit's bytes | reference client surfaces each condition through its documented API; reconnect succeeds | ref | 60 s | nightly | C |

## 8. Hostile Client Suite

Status: `planned`. Owning gate R2 (FR-SUB-012, NFR-SEC-008 evidence).
Every row also asserts the global invariants: the node does not crash, RSS
stays within the configured budget, and concurrently connected well-behaved
control connections keep their delivery latency targets.

| Test ID | Scenario | First failing condition | Required passing assertion | Harness | Runtime budget | CI or nightly | Flake policy |
| --- | --- | --- | --- | --- | --- | --- | --- |
| HOST-001 | Malformed-JSON flood (corpus-driven, 10,000 payloads) | crash or leak | every payload draws 4400 or is discarded post-close; zero panics | host | 60 s | CI | D |
| HOST-002 | Unknown-`type` flood with valid JSON | per-message allocation growth | 4400 per connection; allocations bounded per NFR-SEC-001 | host | 30 s | CI | D |
| HOST-003 | Frames at 512 KiB − 1, 512 KiB, 512 KiB + 1, and init payload at same boundaries | off-by-one or buffering past bound | −1 and exact accepted; +1 closes 4400; buffered bytes never exceed bound + frame overhead | host | 10 s | CI | D |
| HOST-004 | Bypass attempt at the WS library limit (crafted length headers, reserved bits) | 1009 observed or over-read | library backstop configured to 512 KiB; Conduit check fires first; 1009 count is zero | host | 10 s | CI | D |
| HOST-005 | Rapid duplicate-ID subscribes across 1,000 connections | registry corruption | each duplicate draws 4409; entry counts exact afterward | host | 30 s | CI | D |
| HOST-006 | Init flood: 5,000 connections that never send `connection_init` | fd or memory exhaustion | each closed 4408 at deadline; fd count returns to baseline | host | 60 s | CI | D |
| HOST-007 | Rate-limit abuse at exactly 50 msg/s, burst 100, and 2× | false positive or no enforcement | compliant connection survives; abuser warned then closed 4400-class | host | 30 s | CI | D |
| HOST-008 | One message fragmented across 10,000 continuation frames, 1 byte each, slowly | reassembly buffer unbounded or read stall | incremental bound accounting; slow-write deadline closes the connection; memory flat | host | 30 s | CI | D |
| HOST-009 | Interleaved garbage between valid messages on one socket | valid-message state corrupted | first garbage frame closes 4400; prior valid operations were processed exactly once | host | 10 s | CI | D |
| HOST-010 | Slowloris handshake: HTTP upgrade dribbled byte-by-byte from 1,000 sockets | accept loop starvation | handshake deadline drops each at TCP level; concurrent legitimate upgrades complete within budget | host | 60 s | CI | D |
| HOST-011 | Binary-frame flood pre- and post-ack | frames buffered or executed | immediate 4400 per connection; zero payload bytes reach the JSON decoder | host | 10 s | CI | D |
| HOST-012 | `permessage-deflate` offered; then compressed frames sent anyway | extension negotiated or inflated | 101 response omits the extension; RSV1-set frame closes the connection; no inflation code path exists | host | 10 s | CI | D |
| HOST-013 | `Subscribe` with query at 512 KiB envelope ceiling and pathological token density | AST allocated before bounds | typed rejection with zero AST allocation (FR-GQL-011); message bound applies first | host | 10 s | CI | D |
| HOST-014 | ID boundary set: `""`, 255 bytes, 256 bytes, multi-byte UTF-8 straddling 255 | wrong boundary or byte/rune confusion | `""` and >255 bytes close 4400; 255-byte ID (byte-measured) accepted | host | 5 s | CI | D |
| HOST-015 | Invalid UTF-8 corpus: overlong encodings, lone surrogates, truncated sequences, BOM abuse | decoder divergence | uniform 4400 `"invalid utf-8"`; no variant reaches JSON decoding | host | 10 s | CI | D |
| HOST-016 | Churn loop: connect/init/subscribe/RST at 500 conn/s for 5 min | leak per churned connection | RSS and fd count flat after warmup; established control connections meet latency targets (NFR-SCALE-006 precursor) | host | 6 min | nightly | D |
| HOST-017 | Stalled reader during 10,000-event burst against each backpressure policy | per-connection memory exceeds budget | queue bound holds; policy fires per configuration; other connections unaffected | host | 60 s | CI | D |

## 9. Fuzzing

Status: `planned`. Engine: Go native fuzzing (`go test -fuzz`) with
committed corpora under `test/fuzz/corpus/`.

- **Targets**: (a) the frame reader wrapper (fragmentation, length accounting, UTF-8 validation); (b) the JSON protocol-message decoder (envelope shapes, duplicate keys, depth); (c) the `Subscribe` payload decoder (document bounds, variables, extensions, `conduit.resume`); (d) the resume token decoder (gate R7: version byte, HMAC frame, truncation, oversize).
- **Corpus seeding**: every wire message observed in a CONF/HOST suite run is auto-captured into the corpus; plus the protocol document's examples, the §4 JSON examples, and every HOST malformed payload.
- **Cadence**: CI runs each target for 30 s per pull request (regression-guard mode, corpus only); nightly runs each target for 15 min across 4 workers with coverage-guided mutation.
- **Crash triage**: every crasher is reduced, committed as a named regression test within 2 business days, and classified (parser bug / bound bypass / panic-on-input); a bound-bypass class finding triggers the security response protocol and a THREAT_MODEL review.
- **Gate relationship**: R2 entry criteria include zero known crashers on targets (a), (b), (c); target (d) gates R7 identically. A quarantined crasher is a blocked gate, not a footnote.

## 10. Reference Client Interop Matrix

Status: `planned`.

- **Pinned client**: `graphql-ws` exact-pinned at `5.16.0`, accepted range `>=5.14.0 <6.0.0`. The exact pin is what CI runs; the range is what the compatibility claim covers once each range member has passed the suite.
- **Node versions**: 20 LTS and 22 LTS, both in CI; the container images are digest-pinned.
- **Fixture layout**: `test/conformance/fixtures/reference-client/` with `package.json` (exact version), `package-lock.json` (committed), `Dockerfile` (digest-pinned Node base, offline install from the lock file), and `driver.mjs` (scenario runner speaking JSON lines on stdin/stdout; it composes only public `graphql-ws` APIs).
- **Pin advance rule**: a candidate version runs the full CONF suite green twice consecutively before the pin moves; old and new pins then run in nightly for a two-week overlap before the old pin is dropped. A candidate failure is triaged as Conduit bug versus client regression before any pin or code change; the triage record is required evidence.
- The suite runs the reference client unmodified; any test that needs behavior the public client API cannot express belongs to the scripted client, never to a patched reference client.

## 11. Versioning

Status: `planned`. Contracts here follow NFR-COMPAT-003: versioned before
any compatibility promise.

- **Namespace rule**: all Conduit data on the wire lives under the single `conduit` key in the extension positions of §4.9. The namespace carries contract version `conduit-ext/1`; the version string is documented, not transmitted per message — an incompatible revision transmits under a new key (`conduitV2`), never by mutating `conduit` field semantics.
- **Minor bump** (contract `1.x`): adding an optional field to any §4.9 object, adding an enum value to `dropped.policy`, `resumeGap.reason`, or `resumeRejected.reason`, or adding a new extension object. Clients must ignore unknown fields and unknown enum values inside `conduit` objects; that requirement is part of the contract from `1.0`.
- **Major bump** (new namespace key): removing or renaming a field, changing a field's type or meaning, changing when an object is present, or changing resume token verification rules incompatibly.
- **Mixed-version fleet** (NFR-COMPAT-005): releases N and N+1 may differ only by minor bumps in this namespace during the rolling-upgrade window; resume tokens minted by N verify on N+1 and vice versa (token version byte per FR-RESUME-002); cross-version fixtures are release-blocking from the first tagged release. No client sees a protocol behavior change mid-connection during a rollout (PRODUCT_REQUIREMENTS §6.7).
- The `graphql-transport-ws` message surface itself (§4.1–§4.8) is not Conduit's to version; Conduit tracks the protocol document and records any upstream change as a new ADR before adopting it.

## 12. Deferrals and Requirements Traceability

### 12.1 Explicit deferrals

| Deferral | Status | Recorded in |
| --- | --- | --- |
| Legacy `subscriptions-transport-ws` support | deferred | ADR-0002 (reopen requires a superseding ADR) |
| SSE / HTTP-polling transports | deferred | OPEN_QUESTIONS (reopen trigger: a concrete user population that cannot use WebSockets, per ADR-0002) |
| `permessage-deflate` | deferred | OPEN_QUESTIONS (reopen trigger: measured bandwidth need with an amplification-safe design; NFR-SEC-008 controls) |
| Protocol-level in-band auth refresh | deferred | ADR-0008 / OPEN_QUESTIONS (reopen triggers recorded in ADR-0008) |
| `@defer`/`@stream` over the socket | deferred | OPEN_QUESTIONS (GraphQL spec drafts beyond October 2021, PRODUCT_REQUIREMENTS §4.3) |
| `connection_ack` payload use | deferred | §4.2; any use is a versioned change per §11 |

### 12.2 Requirements traced in this document

| Requirement | Touchpoints here | Owning gate |
| --- | --- | --- |
| FR-SUB-001 | §2.1, §2.2, CONF-002/003/004 | R2 |
| FR-SUB-002 | §7.1 ref driver, CONF-005/009/012/017/019/035/043, §10 | R2 |
| FR-SUB-003 | §3.3.2, §5.4–§5.6, CONF-006/007/008 | R2 |
| FR-SUB-004 | §3.3.3, §4.1, CONF-005, auth denial row | R2 |
| FR-SUB-005 | §4.5, §5.7, §5.8, CONF-013/014/015, HOST-014 | R2 |
| FR-SUB-006 | §4.6–§4.8, §4.11, §5.14, CONF-010/011/012 | R2 |
| FR-SUB-007 | §4.3, §4.4, §5.1, §5.2, §5.15, CONF-016/017/018 | R2 |
| FR-SUB-008 | §4 misbehavior maps, §5.9, §5.17, §5.18, CONF-021–027 | R2 |
| FR-SUB-009 | §2.3, CONF-024, HOST-003/004 | R2 |
| FR-SUB-010 | §6 (complete table), every close-code row in §7 | R2 |
| FR-SUB-011 | §3, §4.11 explicit typed tables, CONF-041/042 | R2 |
| FR-SUB-012 | §8 entire suite, §9 | R2 |
| FR-RESUME-001 | §4.9.1 `resumePosition` | R7 |
| FR-RESUME-002 | §4.9.1/§4.9.5 token bounds, §9 target (d), CONF-039 | R7 |
| FR-RESUME-004 | §4.11 splice rows, CONF-037 | R7 |
| FR-RESUME-005 | §4.9.2, CONF-038 | R7 |
| FR-RESUME-007 | §4.9.3, §4.9.5, CONF-039 | R7 |
| NFR-COMPAT-001 | §4.9.6, CONF-035, §10 | R2 |
| NFR-COMPAT-002 | §5.11, §5.12, CONF-019/020 | R2 |
| NFR-SEC-001 | §2.2, §2.3, §5.17, §5.18, §9, HOST-002/013 | R2 |
| NFR-SEC-008 | §2.3 no-deflate, §8, §9, HOST-006/010/012 | R6 (protocol-surface evidence advanced at R2) |

Touchpoint rows advance a requirement; only the owning gate's evidence
checklist closes it (PRODUCT_REQUIREMENTS §10.2). Where BUILD_PLAN §19
disagrees with the owning-gate column above, BUILD_PLAN §19 controls.
