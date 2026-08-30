# ADR-0002: `graphql-transport-ws` Is the Only Subscription Protocol

- Status: accepted
- Date: 2026-08-30
- Related findings or requirements: FR-SUB-001, NFR-COMPAT-001, gate R2

## Context

Two WebSocket subprotocols exist in the GraphQL ecosystem: the modern
`graphql-transport-ws` (the `graphql-ws` npm package's protocol) and the
legacy `subscriptions-transport-ws` (`graphql-ws` subprotocol string
`graphql-ws`, from the deprecated Apollo-era library). Supporting both doubles
the protocol state machine surface, the conformance suite, the close-code
matrix, and the hostile-client fuzz corpus — all of which are R2 acceptance
evidence. Interoperability with the unmodified reference `graphql-ws` client
is a hard acceptance requirement (FR-SUB-002).

## Decision

Conduit implements `graphql-transport-ws` exactly as specified in the
protocol document shipped with `graphql-ws`, and nothing else. A WebSocket
upgrade offering only the legacy `graphql-ws` subprotocol string, or no
recognized subprotocol, is rejected during the HTTP upgrade with status 400
and a body naming the supported subprotocol; if the handshake already
completed, the connection is closed with close code 4406 `Subprotocol not
acceptable`. The rejection is tested in the conformance suite with a real
legacy client fixture.

## Alternatives Considered

- **Support both protocols**: rejected. The legacy protocol has ambiguous
  `connection_ack` semantics, no `ping`/`pong`, overloaded `error` behavior,
  and an unmaintained reference implementation; every ambiguity would need a
  documented decision and adversarial tests, roughly doubling R2 for a
  protocol its own author deprecated in 2021.
- **Legacy-to-modern translation shim**: rejected; a shim is a second
  protocol implementation with extra failure modes, and it would silently
  misrepresent keepalive and error semantics to legacy clients.
- **Additional SSE or HTTP-polling transport in v1**: rejected as scope; the
  product thesis is the WebSocket fanout path. Recorded in OPEN_QUESTIONS
  with a reopen trigger (a concrete user population that cannot use
  WebSockets).

## Consequences

Clients on the deprecated library must migrate before adopting Conduit; the
root README states this plainly. The conformance suite targets one state
machine, which keeps the hostile-client corpus and close-code matrix exact.
If a later ADR adds a transport, it must add a parallel conformance suite of
the same rigor, not extend this one.
