# Conduit Authorization Model

## 1. Status and Scope

- Status: every deliverable in this document is `planned`. Nothing described
  here is implemented; no claim in this document is an implementation claim.
- Last revised: 2026-08-30.
- Owning gates: R3 owns subscribe-time and publish-time enforcement, including
  revocation and expiry behavior and the adversarial evidence in §14. R5 owns
  the fleet-wide revocation-propagation SLO measurement (FR-AUTH-014,
  FR-AUTH-015 fleet behavior). R2 owns the protocol framing these decisions
  ride on (`connection_init`, close codes). R7 owns resume replay, which
  re-enters this document's publish-time decision point.
- Companion documents: [PRODUCT_REQUIREMENTS.md](PRODUCT_REQUIREMENTS.md) (§7.3
  mints every FR-AUTH ID cited here); [GLOSSARY.md](GLOSSARY.md); binding ADRs
  [0008](decisions/ADR-0008-reconnect-refresh-and-bus-propagated-revocation.md)
  (expiry, revocation, caching) and
  [0009](decisions/ADR-0009-tenant-scoped-namespace-isolation.md) (tenancy);
  context ADRs [0004](decisions/ADR-0004-nats-reference-bus-behind-port.md)
  (bus, control subjects),
  [0005](decisions/ADR-0005-sticky-connections-no-shared-registry.md) (local
  matching), and
  [0007](decisions/ADR-0007-at-most-once-live-with-bounded-resume.md) (resume
  replay through publish-time authorization).
- Per the documentation-set conflict rules, this document controls
  authorization semantics that do not conflict with accepted ADRs,
  PRODUCT_REQUIREMENTS.md, or BUILD_PLAN.md. Where this document restates an
  ADR, the ADR's wording controls.

Rule of evidence: no security claim here stands on assertion. Every claim
binds to a named enforcement point (§2.3) and a named adversarial test (§14).
A sentence of the form "authorization is checked" without both bindings is a
documentation defect (NFR-SEC-002).

## 2. Model Overview

### 2.1 The two decision points

Conduit evaluates authorization at exactly two decision points on the
subscription path, plus a per-field decision point on the query/mutation path
(§7):

1. **Subscribe-time** (`SubscriptionAuthorizer.AuthorizeSubscribe`): when a
   `Subscribe` message arrives, before any registry or index registration
   occurs (FR-AUTH-006). Decides whether the principal may hold this
   subscription at all, given the field and the concrete arguments.
2. **Publish-time** (`SubscriptionAuthorizer.AuthorizePublish`): for every
   candidate delivery — every subscription entry the predicate index returns
   for a publish envelope — on the subscriber's node, against the current
   grant state and the concrete event payload, before enqueue (FR-AUTH-010).

### 2.2 Why subscribe-time alone is insufficient

A subscription is a standing grant of future data, and grants change while
subscriptions are live: tokens expire, keys rotate, scopes are removed, users
are disabled (ADR-0008 context). Checking only at subscribe time converts
every credential compromise into an indefinite feed that outlives the
credential: a 15-minute JWT must not fund a 6-hour subscription; a key rotated
after a leak must stop deliveries fleet-wide within the propagation SLO
(FR-AUTH-014), not at the next reconnect; an agent whose `orders:read` scope
is removed at 14:00 must not receive the 14:01 order event. Publish-time
re-evaluation is therefore the primary control; subscribe-time is an admission
optimization and a fast-failure courtesy. The decision cache (§6.3) makes
re-evaluation affordable; the grant-state epoch (§4.3) guarantees the cache
never serves a decision older than the newest grant change (FR-AUTH-011).

### 2.3 Enforcement-point registry

This subsection is the "§enforcement" registry that NFR-SEC-002 cites. An
enforcement point is named `<Component>.<Method>`, matching the real Go
component and method that implements it. The names are contracts: a rename
must update this table, the audit vocabulary (§13), and every test that
asserts on the name.

| Enforcement point | Decision | Gate | Adversarial evidence (§14) |
| --- | --- | --- | --- |
| `AuthMode.Authenticate` | connection admission; mints the principal | R3 (framing R2) | AUTHZ-004, 011, 012, 014–019 |
| `SubscriptionAuthorizer.AuthorizeSubscribe` | subscription registration | R3 | AUTHZ-004, 006, 007 |
| `SubscriptionAuthorizer.AuthorizePublish` | per-candidate delivery enqueue | R3 (fleet SLO R5) | AUTHZ-001, 002, 003, 005, 013, 020 |
| `FieldAuthorizer.AuthorizeField` | per-field query/mutation execution | R3 | AUTHZ-023, 027 |
| `ExpirySweeper.Enforce` | fail-closed at credential expiry | R3 | AUTHZ-005, 013 |
| `RevocationApplier.Apply` | revocation-set update + epoch advance | R3 (propagation R5) | AUTHZ-001, 010, 026 |
| `AdminAuthenticator.Authenticate` | admin listener admission | R3 (surface R8) | AUTHZ-022 |
| `ErrorRedactor.Redact` | uniform authorization failures | R3 | AUTHZ-023 |
| `SinkRedactor.Scrub` | secret/payload exclusion at every log, metric, trace, and diagnostics sink | R3 (per-sink canaries re-run R6/R8) | AUTHZ-024 |

### 2.4 Authorization rules: the v1 condition language

Rules in v1 are named, structured YAML condition documents (FR-AUTH-008;
FR-AUTH-009: structured decisions, no string-interpolated policy). There is no
expression language in v1; CEL is deliberately deferred (§15.1). Anything the
grammar below cannot express delegates to the custom authorizer hook (§3.4)
via the `custom.authorize` leaf.

A rule has:

- `name`: unique per tenant schema, `[a-z0-9_-]{1,64}`;
- `effect`: `allow` or `deny`. The model is default-deny: absence of an
  allowing rule denies. `deny` rules exist only as explicit shortcuts — when
  a `deny` rule's condition matches, evaluation stops immediately and the
  decision is deny, regardless of any `allow` rule that would also match (all
  `deny` rules for a field evaluate before any `allow` rule; AUTHZ-027);
- `when`: a condition tree. Composition nodes are `allOf`, `anyOf`, and
  `not`, nesting depth at most 4 (deeper fails startup validation);
- leaf conditions, drawn exclusively from this closed set:

| Context | Leaf | Operators | Meaning |
| --- | --- | --- | --- |
| principal | `principal.claim` | `equals`, `oneOf`, `contains` | compare the named claim to literal value(s); `contains` applies to list-valued claims |
| principal | `principal.scope` | `present` | the named scope is in the principal's scope set |
| principal | `principal.subject` | `equals` | subject equals a literal |
| principal | `principal.tenant` | `equals` | tenant equals a literal |
| field | `field.argument` | `equalsClaim`, `oneOfClaim` | the named argument's concrete value equals the referenced claim's value, or is a member of the referenced list-valued claim (argument-claim binding) |
| field | `field.parentType` | `equals` | parent type name equals a literal |
| field | `field.name` | `equals`, `oneOf` | field name matches |
| delegation | `custom.authorize` | `hook` | evaluate by calling the configured custom authorizer decision endpoint (§3.4); timeout or malformed response evaluates false (fail-closed) |

At publish time the same rule evaluates with the concrete event payload as the
field's resolved-value context; `field.argument` leaves evaluate against the
subscription's registered argument bindings (§5.2), so a rule means the same
thing at both decision points.

### 2.5 Worked examples

Rules live under `authorization.rules` in operator configuration and are
referenced from SDL via `@auth(rule: "<name>")`.

Example 1 — tenant-scoped field (defense in depth on top of structural
tenancy, §11), attached as `inventoryChanged(sku: String @filterable):
InventoryEvent @auth(rule: "same-tenant-only")`:

```yaml
authorization:
  rules:
    - name: same-tenant-only
      effect: allow
      when:
        principal.tenant: { equals: "acme" }
```

Example 2 — owner-only subscription via argument-claim binding. The client
must pass its own customer ID; a spoofed ID fails both decision points
(AUTHZ-007):

```yaml
    - name: own-orders-only
      effect: allow
      when:
        allOf:
          - principal.scope: { present: "orders:read" }
          - field.argument: { name: "customerId", equalsClaim: "customer_id" }
```

```graphql
type Subscription {
  orderUpdates(customerId: ID! @filterable): OrderEvent
    @auth(rule: "own-orders-only")
}
```

Example 3 — scope-gated mutation, attached to
`submitOrder(input: OrderInput!): OrderResult`:

```yaml
    - name: can-publish-orders
      effect: allow
      when:
        principal.scope: { present: "orders:write" }
```

Example 4 — admin-only introspection (FR-GQL-010 policy hook; introspection
fields carry this rule when introspection is enabled with auth):

```yaml
    - name: introspection-admins
      effect: allow
      when:
        principal.claim: { name: "roles", contains: "schema-admin" }
```

Example 5 — deny shortcut. Service accounts may never hold live subscriptions
on fields carrying this rule, even where a broad allow rule would admit them;
the deny evaluates first and stops evaluation:

```yaml
    - name: no-service-accounts
      effect: deny
      when:
        principal.claim: { name: "account_type", equals: "service" }
```

Example 6 — custom-authorizer delegation, composed with `anyOf`/`not` inside
the depth bound, for logic the grammar cannot express (row-level entitlement
lookup):

```yaml
    - name: entitled-portfolios
      effect: allow
      when:
        allOf:
          - anyOf:
              - principal.claim: { name: "roles", contains: "analyst" }
              - field.argument: { name: "accountId", oneOfClaim: "owned_accounts" }
          - not:
              principal.claim: { name: "status", equals: "suspended" }
          - custom.authorize: { hook: "entitlements" }
```

### 2.6 Startup validation of rules

`conduit validate` and process startup run identical validation (FR-OPS-002
pattern). Rules validation is fail-fast:

1. **Undefined rule reference fails startup.** Any `@auth(rule: X)` in SDL
   where `X` is not defined under `authorization.rules` for that tenant's
   schema aborts startup, naming the SDL location and the rule name
   (FR-AUTH-008; AUTHZ-006).
2. **Unknown claim reference warns.** A leaf naming a claim absent from the
   auth mode's declared claim mapping (§3.2) logs a startup warning naming
   rule and claim — a warning, not an error, because custom authorizers may
   mint claims not statically declared. At evaluation time an absent claim
   makes the leaf false (fail-closed), never an error.
3. **Type mismatches fail startup.** `oneOfClaim` or `contains` against a
   claim declared scalar; `equalsClaim` binding an argument whose GraphQL
   type cannot compare to the claim's declared type; `field.argument` naming
   an argument the annotated field does not declare — all abort startup
   naming rule, leaf, and both types.
4. **Structural bounds fail startup.** Nesting depth > 4, duplicate rule
   names, empty `when`, unknown leaf or operator keys, `oneOf` lists over
   100 entries.
5. **Delegation references fail startup** when `custom.authorize.hook` names
   a hook with no configured endpoint.

## 3. Auth Modes

Auth modes are configured per tenant/listener; at most one mode authenticates
a given connection (FR-AUTH-004). Every mode implements one interface and
produces the same principal shape (§4), so the decision points are
mode-agnostic.

### 3.1 The `AuthMode` interface

```go
// AuthMode authenticates one credential presentation into a Principal.
// Implementations: OIDCMode, APIKeyMode, CustomAuthorizerMode, NoneMode.
type AuthMode interface {
	// Name returns the stable mode identifier: "oidc", "api_key",
	// "custom", or "none".
	Name() string

	// Revocable reports whether grants minted by this mode can be revoked
	// mid-lifetime: false only for "none". Degraded-mode suspension (§10)
	// applies only to revocable-mode principals.
	Revocable() bool

	// Authenticate validates the presented credential and returns a new
	// immutable Principal or a typed denial, never a partial Principal.
	// Credential material must not be retained beyond this call
	// (FR-AUTH-005: the principal never contains raw credentials).
	Authenticate(ctx context.Context, cred Credential, meta ConnMeta) (*Principal, *AuthDenial)
}

// Credential carries the presentation-instant material, zeroed by the
// caller immediately after Authenticate returns. Kind is one of
// KindBearerHeader, KindInitPayload, KindAPIKeyHeader.
type Credential struct {
	Kind     CredentialKind
	Material []byte
}

// ConnMeta is the non-secret connection context: RemoteAddr, Listener,
// TLSServerName, UserAgent, and TenantHint (from listener config, never
// client-asserted).
type ConnMeta struct {
	RemoteAddr, Listener, TLSServerName, UserAgent, TenantHint string
}
```

### 3.2 OIDC/JWT mode

Validates a bearer token from the `connection_init` payload (WebSocket) or the
`Authorization` header (HTTP) against a cached JWKS (FR-AUTH-001).

**JWKS caching and rotation algorithm** (bounded refresh, kid-miss
single-flight):

1. At startup, fetch the configured JWKS URL. A fetch failure is a startup
   failure when `auth.oidc.require_jwks_at_start: true` (default); otherwise
   the mode starts deny-all until the first successful fetch, and `/readyz`
   reports it.
2. Cache the key set with its fetch timestamp. All validations are served
   from cache; no validation performs a network call inline except step 4.
3. Refresh in the background every `jwks.refresh_interval` (default 300 s),
   jitter ±10 %. A failed refresh keeps the previous set and increments
   `conduit_auth_jwks_refresh_failures_total`; after `jwks.max_stale`
   (default 24 h) without success, the mode fails closed (new
   authentications denied) and readiness reports it.
4. On a token whose `kid` is absent from cache (rotation signal): perform one
   single-flight refetch — concurrent kid-misses coalesce onto one in-flight
   request and share its result — rate-limited to one per
   `jwks.kid_miss_min_interval` (default 10 s) per issuer, so a flood of
   forged kids cannot amplify into JWKS-endpoint load (AUTHZ-015). If the
   kid is still unknown after refetch, deny.
5. Keys removed from the fetched set drop from cache at refresh, and
   `GrantEpoch` advances (§4.3) so publish-time decisions cached under the
   removed key's assumptions re-evaluate (JWKS-forced invalidation).

**Validation checklist** — every item must pass or the token is denied with
the uniform failure of FR-AUTH-018 (the deny audit record names the failing
check; the client never sees it):

1. Parse: compact JWS, one signature; tokens over 8 KiB rejected before
   parse (NFR-SEC-001).
2. `alg` in the configured allowlist (default `RS256`, `ES256`); `none` and
   HMAC algorithms never accepted for OIDC mode (AUTHZ-014).
3. Signature verifies against the cached key for `kid` (step 4 on miss).
4. `iss` equals the configured issuer exactly (AUTHZ-016).
5. `aud` contains the configured audience (AUTHZ-016).
6. `exp` present and in the future within clock skew ±30 s (default;
   `auth.clock_skew` configurable) (AUTHZ-004).
7. `nbf`, when present, in the past within the same skew (AUTHZ-017).
8. The configured tenant claim resolves per §11.2 (missing claim rejects
   4403 when multi-tenancy is enabled).

**Claim mapping** is explicit configuration; unmapped claims are dropped,
never implicitly forwarded into the principal:

```yaml
auth:
  mode: oidc
  oidc:
    issuer: "https://id.example.com"
    audience: "conduit"
    jwks_url: "https://id.example.com/.well-known/jwks.json"
    subject_claim: "sub"
    tenant_claim: "org"
    scope_claim: "scope"          # space-delimited string or list
    claims:                       # declared claims copied into the principal
      - { name: "customer_id",    type: string }
      - { name: "owned_accounts", type: string_list }
      - { name: "roles",          type: string_list }
      - { name: "account_type",   type: string }
      - { name: "status",         type: string }
```

### 3.3 API key mode

Validates presented keys against a store of salted hashes with per-key
metadata; plaintext keys exist only in the presentation instant (FR-AUTH-002).

**Presentation forms.** Exactly two, carrying the same key string: HTTP via
`Authorization: Bearer ck_<key>` or `X-Api-Key: ck_<key>`; WebSocket via the
`connection_init` payload field `apiKey: "ck_<key>"`. Browser clients cannot
set arbitrary headers on a WebSocket upgrade, which is why the init-payload
form exists; both forms cross the same `AuthMode.Authenticate` enforcement
point.

**Store.** Each record: key ID (public prefix, first 8 chars after `ck_`),
Argon2id hash of the remainder with a per-key salt, tenant, scope list,
optional claims, expiry, created/rotated timestamps, and revocation state.
Lookup is by key ID; comparison of computed hash against stored hash is
constant-time (AUTHZ-018). The plaintext is zeroed after hashing and never
reaches the principal, logs, or the store (NFR-SEC-004).

**Rotation.** A rotation creates a new key record and marks the old one
`rotated` with a configurable overlap window (default 24 h) in which both
validate; at window end the old record flips to `revoked` and a `kind: key`
revocation (§9) propagates so live connections funded by the old key are
swept. Immediate rotation (leak response) skips the overlap: revoke old,
issue new.

### 3.4 Custom authorizer mode

Calls an operator endpoint with a versioned request and receives a versioned
decision; timeout or malformed response fails closed (FR-AUTH-003).
Transport: Unix domain socket (`unix:///path`) or loopback HTTPS
(`https://127.0.0.1:<port>` with pinned server certificate). Any other host
is a startup configuration error: the authorizer is a sidecar, not a remote
dependency on the delivery path.

Request (Conduit → authorizer), `POST /v1/authenticate`:

```json
{
  "version": 1,
  "request_id": "01J9XW7Q4R8Z",
  "tenant_hint": "acme",
  "credential": { "kind": "init_payload", "material": "opaque-client-string" },
  "connection": { "remote_addr": "203.0.113.9:52114", "listener": "wss-main",
                  "tls_server_name": "gw.example.com", "user_agent": "app/4.2" }
}
```

Response (authorizer → Conduit):

```json
{
  "version": 1,
  "request_id": "01J9XW7Q4R8Z",
  "decision": "allow",
  "principal": {
    "subject": "user-8812", "tenant": "acme", "scopes": ["orders:read"],
    "claims": { "customer_id": "8812", "roles": ["analyst"] },
    "expires_at": "2026-08-30T15:04:05Z"
  },
  "ttl_ms": 30000,
  "deny_reason_class": null
}
```

Contract rules:

1. Timeout 2 s (configurable down, never above 5 s); timeout, non-200,
   unknown `version`, schema-invalid body, or `decision` other than
   `allow`/`deny` all fail closed (AUTHZ-008).
2. `ttl_ms` bounds decision reuse for the `custom.authorize` leaf (§2.4):
   per-entry hook results cache for `ttl_ms`, additionally invalidated by
   any epoch advance. `ttl_ms` never extends `expires_at`.
3. `deny_reason_class` feeds the audit record only; the client sees the
   uniform failure (FR-AUTH-018).
4. The same endpoint may expose a revocation feed: `GET /v1/revocations`
   long-poll returning ADR-0008 revocation records, which Conduit's admin
   plane republishes on the bus control subject exactly as admin-API
   revocations (§9.1). The authorizer never publishes to the bus directly.
5. The per-decision hook for the `custom.authorize` leaf is
   `POST /v1/authorize` with the same envelope plus `rule`, `field`,
   `parent_type`, `arguments`, and `principal_subject` fields, returning a
   boolean `decision` with its own `ttl_ms`.

### 3.5 Mode `none`

Development only. Requires `development_acknowledged: true` in configuration;
absent that key, startup fails; present, startup logs a warning naming the
listener (FR-AUTH-004; AUTHZ-025). Mode `none` mints a principal with subject
`anonymous`, the listener's tenant, no scopes, no expiry, and
`Revocable() == false`. Rules still evaluate (default-deny still applies);
degraded-mode suspension does not apply because there is nothing to revoke.

## 4. Principal Model

### 4.1 The struct

```go
// Principal is the authenticated identity attached to a connection.
// Normative per FR-AUTH-005: immutable per connection, never containing
// raw credential material.
type Principal struct {
	Subject string // authenticated identity string
	// Tenant is the isolation unit (ADR-0009). Always set: the implicit
	// "default" tenant is materialized here in single-tenant mode.
	Tenant string
	Scopes []string // sorted, deduplicated scope set
	// Claims is the declared-and-mapped claim set (§3.2). Values are
	// typed: string, string list, int64, or bool.
	Claims map[string]ClaimValue
	// ExpiresAt is the credential expiry instant. Zero means no expiry
	// (mode "none" only).
	ExpiresAt time.Time
	Mode      string // AuthMode name that minted this principal
	// Epoch is the node-local grant-state epoch observed when the
	// principal was minted. Decision caching keys on the node's current
	// epoch (§6.3), not this field; this field lets audit records state
	// what the principal was minted under.
	Epoch uint64
}
```

### 4.2 Immutability rule

A `Principal` is constructed once, by `AuthMode.Authenticate`, and never
mutated. There is no re-authentication path on a live connection: the protocol
admits one `connection_init` (a second closes 4429, gate R2; AUTHZ-012), and
no server code path writes a principal field after construction. Grant-state
changes are represented outside the principal — in the revocation set and the
epoch counter — never by editing it. Consequence: any code holding a
`*Principal` may read it without synchronization, and an audit record can
cite its exact content by reference.

### 4.3 The grant-state epoch

`GrantEpoch` is a node-local, monotonic `uint64` counter, advanced (atomically
incremented, never set) by exactly three events:

1. `RevocationApplier.Apply` accepting a first-seen revocation (§9.1);
2. an atomic policy/SDL reload that changes any rule (FR-OPS-003 cutover);
3. a JWKS refresh that removes a key from the cached set (§3.2 step 5).

The epoch never decreases; no API, admin action, or bus message writes a
specific epoch value, which makes the rollback attack structurally
unexpressible (AUTHZ-010). The epoch is per-node and carries no cross-node
meaning; fleet agreement comes from every node applying the same revocation
stream, not from sharing counters.

## 5. Subscribe-Time Decision Point

Enforcement point: `SubscriptionAuthorizer.AuthorizeSubscribe` (FR-AUTH-006).
Status: planned, gate R3.

### 5.1 Algorithm

Input: an acknowledged connection with principal `p`, a `Subscribe` message
(id, document, variables).

1. **Protocol precondition.** The connection is post-`connection_ack`; a
   `Subscribe` before acknowledgment closed 4401 in the R2 state machine
   before reaching this component (AUTHZ-011). This algorithm never runs
   without a principal.
2. **Parse and validate** the document against the tenant's schema (R1
   executor). Failure → typed `error` on the subscription id; stop.
   Bounded-input rules (FR-SUB-009, NFR-SEC-001) applied upstream.
3. **Expiry check.** If `p.ExpiresAt` is nonzero and not after now, deny:
   the connection is at or past expiry and the race with the sweeper
   resolves fail-closed (AUTHZ-004). Emit `TOKEN_EXPIRED`; the sweeper owns
   the 4403 close.
4. **Revocation check.** Consult the node-local revocation set (§9) for `p`
   (by principal, subject, key, and each scope the field's rule requires).
   A hit denies with the uniform failure.
5. **Rule lookup.** Resolve the `@auth(rule:)` reference on the requested
   subscription field. A field without `@auth` has no allowing rule; the
   model is default-deny, so the subscribe is denied unless the field is in
   `authorization.unauthenticated_fields` (explicit allowlist, empty by
   default). Undefined references cannot occur here — they failed startup
   (§2.6).
6. **Evaluate** deny-shortcut rules for the field first, then the allow
   rule: the §2.4 condition tree against `p` and the field context (field
   name, parent type, concrete post-variable argument values).
   `custom.authorize` leaves call the hook under the 2 s fail-closed
   contract.
7. **Decision.** Deny → one spec-shaped `error` on the subscription id,
   `extensions.code: "FORBIDDEN"` (or `TOKEN_EXPIRED` from step 3), no rule
   name, no claim name, no indication whether the field exists when
   introspection is disabled (FR-AUTH-018; AUTHZ-023); deny audit record
   (§13); stop — nothing was registered. The connection stays open: a
   denied subscribe is not a protocol violation. Allow → proceed.
8. **Registration.** Compile predicates (FR-FILT-001) and register the
   subscription entry in the connection registry and predicate index. Only
   now does the entry exist, binding the items in §5.2.

Edge cases: a variable resolving to null for an argument used in a
`field.argument` binding evaluates the leaf false (deny), never "matches" a
null claim; multiple root subscription fields were already rejected by
validation; a revocation landing between steps 4 and 8 is closed by the
publish-time point — the entry registers, but its first candidate delivery
re-checks under the advanced epoch and the sweep removes it (the AUTHZ-001
race family covers this interleaving).

### 5.2 What is bound into the subscription entry

The registered entry carries, immutably:

- **principal ref**: a pointer to the connection's immutable `Principal`;
- **rule ref**: the resolved rule (and its deny-shortcut set) as compiled
  condition trees — not a name looked up again later, so a policy reload
  cannot silently swap semantics under a live entry; a reload advances the
  epoch and the entry re-evaluates under the new policy via the sweep (§9.1
  step 6);
- **argument bindings**: the concrete post-variable argument values the
  subscribe was authorized with, used by `field.argument` leaves at publish
  time. A client cannot re-bind arguments without a new `Subscribe`, which
  re-enters this decision point (AUTHZ-007).

## 6. Publish-Time Decision Point

Enforcement point: `SubscriptionAuthorizer.AuthorizePublish` (FR-AUTH-010).
Status: planned, gate R3; fleet-propagation evidence R5. Position in the
delivery path: bus envelope → tenant-sharded predicate index → candidate set
→ **AuthorizePublish per candidate** → backpressure/enqueue, on the
subscriber's node (ADR-0005: matching is local). GLOSSARY "delivery" begins
only after this point allows.

### 6.1 Algorithm (per candidate delivery)

Input: subscription entry `e` (principal ref `p`, compiled rule, argument
bindings), publish envelope `env`.

1. **Read epoch.** `epoch := GrantEpoch.Load()`.
2. **Expiry gate.** If `p.ExpiresAt` is nonzero and not after now, suppress
   with counter `conduit_deliveries_suppressed_total{reason="expired"}`; the
   `ExpirySweeper` owns the error/close (§8). No cache is consulted: the
   comparison is cheaper than a probe and never cacheable.
3. **Cache probe** with key `(e.ID, epoch)` (FR-AUTH-011). The cache is
   node-local, size-bounded, and stores only allow/deny — never claim
   values or payload bytes. Hit(allow) → step 7. Hit(deny) → suppress with
   counter, stop. Miss → continue.
4. **Revocation check.** Consult the revocation set for `p` (principal,
   subject, key, required scopes). Hit → cache deny under `epoch`, suppress
   with `reason="revoked"`, stop. The sweep (§9.1 step 6) — not this hot
   path — emits `GRANT_REVOKED` and closes; this path only guarantees no
   delivery after node-local application (FR-AUTH-013).
5. **Evaluate the compiled rule** against `p` (the immutable struct — the
   current principal state), the entry's argument bindings, and the
   concrete event payload of `env`. `custom.authorize` leaves use the hook
   cache within `ttl_ms`, calling out on miss under the 2 s fail-closed
   rule. Deny-shortcuts evaluate first, as always.
6. **Cache** the outcome under `(e.ID, epoch)`.
7. **Epoch check at enqueue.** Immediately before enqueue, re-read
   `GrantEpoch`. If it no longer equals `epoch`, discard the decision and
   re-run from step 1 (at most once; a second mismatch suppresses and
   defers to the sweep). Also re-check the connection's expired flag
   (§8.2). This rule closes the probe-to-enqueue races (AUTHZ-002,
   AUTHZ-013): no delivery is enqueued under an epoch that is no longer
   current.
8. **Enqueue** onto the connection's outbound queue; the backpressure
   policy applies downstream. Allow-side audit sampling per §13.

Edge cases: a candidate whose connection is mid-close suppresses silently
(registry removal racing normally); unknown-version envelopes were rejected
before matching (FR-FAN-002); duplicates were deduplicated before this point
(FR-FAN-008) — the decision cache is not a dedupe mechanism and dedupe is not
an authorization mechanism.

### 6.2 The decision cache, precisely

- Key: `(subscription entry ID, grant-state epoch)` — nothing else. No
  time-based validity: a cached decision lives exactly as long as its epoch
  is current and the entry exists.
- Every revocation, policy reload, or JWKS key removal advances the epoch
  (§4.3), orphaning the entire cache generation at once. Orphaned
  generations are swept lazily; a stale entry can never be probed because
  probes always use the current epoch (AUTHZ-002).
- Rules whose outcome can vary per event (a `custom.authorize` hook may;
  `field.argument` leaves cannot — bindings are fixed) are marked
  non-cacheable at compile time; step 3 skips the probe and step 6 skips
  the fill for them, bounded instead by the hook `ttl_ms`.

### 6.3 No configuration skips it

For revocable auth modes (`oidc`, `api_key`, `custom`) there is no
configuration key, fast path, or per-tenant override that bypasses
`AuthorizePublish` (FR-AUTH-010). The only mode that shortcuts evaluation is
`none`, whose principals are non-revocable by construction and which cannot
be enabled without the development acknowledgment (§3.5). The R3 suite
includes a configuration-matrix test asserting the enforcement point is on
the delivery path in every supported configuration (AUTHZ-003 runs under all
of them).

### 6.4 Resume replay passes the same call

Replayed envelopes from the resume buffer traverse the same
`AuthorizePublish` call as live envelopes (ADR-0007 decision item 2): the
replay loop is a second producer into the same per-candidate path, not a
parallel path. Consequences: a subscription resumed after a revocation
replays nothing (step 4 denies); expiry during replay aborts the replay
mid-stream fail-closed (§8.3; AUTHZ-005); and a rule change between
disconnect and resume applies to replayed events, because the resumed
subscription re-registered through `AuthorizeSubscribe` and compiled the
current rule. Gate R7 owns replay mechanics; R3's enforcement assertions run
inside R7's replay tests.

## 7. Field-Level Authorization for Queries and Mutations

Enforcement point: `FieldAuthorizer.AuthorizeField` (FR-AUTH-007). Status:
planned, gate R3.

1. **Evaluation order.** After validation and before resolver execution,
   the executor walks the operation's selection set; for each field
   carrying `@auth`, deny-shortcuts then the allow rule evaluate against
   the principal and the field context (concrete arguments, parent type).
   Fields without `@auth` follow the default-deny rule with the
   `unauthenticated_fields` allowlist exactly as §5.1 step 5. The check
   runs before the field's resolver: a denied field never executes its
   resolver, so denial leaks no resolver side effects or timing.
2. **Error shape.** A denied field yields a spec-shaped error at that path:
   `{"message": "...", "path": ["order", "customerEmail"], "extensions":
   {"code": "FORBIDDEN"}}`, and the field's value is null.
3. **Null propagation.** Standard GraphQL rules: a denied `Non-Null` field
   propagates null to the nearest nullable ancestor; a denied nullable
   field nulls only itself. The rest of the operation executes normally —
   one denied field does not fail sibling fields.
4. **Uniform errors** (FR-AUTH-018): message and code are identical for
   "rule denied" and "no rule and not allowlisted"; when introspection is
   disabled, a denied field and a nonexistent field produce the same shape
   the schema gives any unknown field, so denial is not an existence
   oracle (AUTHZ-023). No rule name, claim name, or required scope appears
   in any client-visible error; `ErrorRedactor.Redact` is the single exit
   for error payloads.
5. **Mutations** are additionally the publish origin: a mutation denied at
   field level never executes, so it never publishes (FR-FAN-001 starts
   from a successful mutation). The admin publish endpoint does not bypass
   this — see §12.5.

## 8. Expiry Mid-Subscription

Owner: `ExpirySweeper.Enforce` on a shared timing wheel (ADR-0008,
FR-AUTH-012). Status: planned, gate R3.

### 8.1 Timeline walkthrough

For a principal with `ExpiresAt = T`, warning window 60 s (default,
`auth.expiry_warning_window`):

| Instant | Behavior |
| --- | --- |
| admission | the connection's expiry registers on the timing wheel at `T` and `T−60 s` |
| `T − 60 s` | the server sends a protocol `ping` whose payload carries the expiry extension at JSON path `conduit.expires_in_ms` per ADR-0008: `{"conduit":{"expires_in_ms":60000}}`. Well-behaved clients reconnect preemptively with a fresh token and resume tokens (ADR-0007 continuity). The ping is advisory; nothing depends on the client honoring it |
| `T` | fail-closed immediately: the connection's expired flag is set; `AuthorizePublish` step 2 and the enqueue gate (step 7) suppress all further deliveries for this principal from this instant |
| `T` (same sweep) | the sweeper sends one `error` per live subscription with typed code `TOKEN_EXPIRED`, then closes the connection with close code 4403 (`Credential expired`) |
| after `T` | reconnect with a fresh credential is the only refresh path — no in-band refresh in v1 (ADR-0008); resume tokens restore continuity through `AuthorizeSubscribe` and replay (§6.4) |

The gap between flag-set and close-frame flush is bounded by one sweep tick
(100 ms wheel resolution); during that gap the flag has already stopped
deliveries, so lateness of the close is a UX concern, not a data-exposure
concern (AUTHZ-013 asserts zero deliveries enqueued at or after `T`).

### 8.2 Mechanics

The timing wheel is shared per node (one goroutine, hierarchical buckets,
100 ms resolution), one entry per connection with finite expiry. The expired
flag is a per-connection atomic read at `AuthorizePublish` step 2 and again
at the enqueue gate — the **epoch-check-at-enqueue rule** covers both: a
delivery may enqueue only if the epoch read at probe time is still current
and the expired flag is unset at the enqueue instant. Expiry does not advance
the node epoch (it is per-connection state, not grant-state), which is why it
has its own flag rather than riding the epoch.

### 8.3 Edge cases

- **Expiry during replay** (AUTHZ-005): replayed envelopes pass
  `AuthorizePublish`; step 2 suppresses from `T` exactly as for live
  events. The replay loop observes the suppression, aborts, and the
  sweep's `TOKEN_EXPIRED` + 4403 follow. No `resume_gap` is fabricated:
  the client's next resume attempt (with a fresh credential) states the
  true covered range.
- **Expiry between cache probe and enqueue** (AUTHZ-013): closed by the
  enqueue gate. The R3 test freezes the injected clock between probe and
  enqueue and asserts suppression.
- **Clock skew policy**: the ±30 s allowance applies once, at token
  validation (§3.2 items 6–7), effectively extending accepted `exp` at
  admission. Enforcement at `T` uses the node's own clock with no
  additional grace: skew is a validation allowance, not a delivery
  extension. Operators must run NTP-synced nodes; `conduit doctor` checks
  clock sync (FR-OPS-013).
- **Expiry storm**: mass simultaneous expiry converts to reconnect load;
  the R9 harness includes an expiry-storm scenario (ADR-0008
  consequences). Not an R3 concern beyond correctness under load.

## 9. Revocation Mid-Subscription

Owners: `RevocationApplier.Apply` (node-local application, R3);
fleet-propagation SLO measurement, R5 (FR-AUTH-013, FR-AUTH-014).

### 9.1 End-to-end sequence

1. **Origin.** A revocation enters through exactly two doors: the admin API
   (`POST /admin/v1/revocations`, authenticated per FR-ADMIN-001) or the
   custom authorizer's revocation feed (§3.4 rule 4), which the admin plane
   republishes. There is no third door.
2. **Audit record.** The admin mutation produces a structured audit record
   with actor identity and request ID (FR-ADMIN-008) before anything
   propagates. The admin call acknowledges only after the record is written
   and the bus publish succeeds; the SLO clock starts at this
   acknowledgment (ADR-0008).
3. **Bus publish** on the control subject `conduit.<tenant>.ctl.revoke`
   with the ADR-0008 record:

   ```json
   { "kind": "scope", "id": "user-8812:orders:read",
     "issued_at": "2026-08-30T14:00:02Z", "revocation_id": "01J9XYAG5T2K" }
   ```

   `kind` ∈ {`principal`, `subject`, `key`, `scope`}. `id` encoding per
   kind: `principal` → `<mode>:<tenant>:<subject>`; `subject` → the subject
   string; `key` → the API key ID; `scope` → `<subject>:<scope>`.
   `revocation_id` is a ULID; `issued_at` drives set-entry expiry (entry
   lifetime = the revoked grant's maximum remaining lifetime plus slack,
   keeping the set bounded per ADR-0008; set size is a published metric).
4. **Node apply.** Every node serving the tenant receives the message and
   calls `RevocationApplier.Apply`: insert into the in-memory revocation
   set, idempotent on `revocation_id` — a redelivered record neither
   double-advances the epoch nor removes anything (AUTHZ-010).
5. **Epoch advance.** A first-seen `revocation_id` advances `GrantEpoch`
   once. From this instant the decision-cache generation is orphaned and
   `AuthorizePublish` step 4 denies matching candidates: **no delivery
   after node-local application** (FR-AUTH-013; AUTHZ-001).
6. **Sweep.** A background sweep (same wheel infrastructure as §8) walks
   local entries and connections against the applied record: entries whose
   principal no longer passes their rule (scope revocation) or whose
   principal/subject/key is revoked receive one `error` with typed code
   `GRANT_REVOKED` and are unregistered; connections whose principal is
   fully revoked are closed 4403 after the errors; connections with
   surviving subscriptions stay open (§9.2).
7. **Fleet SLO.** p99 application latency — admin acknowledgment to the
   last node's epoch advance — ≤ 2 s (FR-AUTH-014). R5 measurement method:
   the harness timestamps the admin 200 response; each node emits a
   `conduit_revocation_applied` event carrying `revocation_id` and a
   monotonic-clock capture; the fleet suite computes the distribution
   across ≥ 1,000 revocations under reference publish load. R3 proves
   node-local correctness; only R5 evidence may state the fleet number
   (never promoted from single-node results).

### 9.2 Partial-revocation kinds

| Kind | Suppresses | Sweep action | Connection fate |
| --- | --- | --- | --- |
| `principal` | all deliveries for the exact principal | `GRANT_REVOKED` on every subscription | close 4403 |
| `subject` | all deliveries for the subject in the tenant, all modes | `GRANT_REVOKED` on every subscription of every matching connection | close 4403 |
| `key` | all deliveries funded by the API key | `GRANT_REVOKED` on that connection's subscriptions | close 4403 |
| `scope` | deliveries whose allow rule requires the revoked scope for that subject | `GRANT_REVOKED` on the affected subscriptions only (AUTHZ-020) | stays open if any subscription survives |

Scope revocation is evaluated by re-running the entry's compiled rule with
the revocation set consulted for `principal.scope` leaves: a `present` leaf
naming a revoked `<subject>:<scope>` pair evaluates false. The principal
struct is not edited (§4.2).

### 9.3 Replay interaction

A resume presented after a revocation re-enters `AuthorizeSubscribe` (denied
for revoked principals at step 4) and, for surviving subscriptions, replays
through `AuthorizePublish` (§6.4), whose step 4 consults the same set. No
replay path predates the revocation check: buffered envelopes are
authorization-naive bytes until candidate evaluation (ADR-0007; AUTHZ-021
covers token misuse; the AUTHZ-001 replay variant covers revoked replay).

## 10. Degraded Mode

Trigger: control-channel loss (FR-AUTH-015). Status: planned; node-local
behavior R3, fleet behavior with a real broker R5.

### 10.1 Detection

The control plane publishes a heartbeat on `conduit.<tenant>.ctl.heartbeat`
every 2 s. A node that has received no control heartbeat for the timeout
(default 10 s, `auth.degraded.heartbeat_timeout`) enters degraded mode for
the affected tenants. Bus-connection loss reported by the bus port (ADR-0004
connection-state events) short-circuits detection immediately rather than
waiting out the timeout. Recovery detection is the inverse: a fresh heartbeat
plus completed reconciliation (§10.4).

### 10.2 The two policies, exactly

Policy is explicit, logged operator configuration with no silent default
change ever; changing it requires restart or an audited admin action
(FR-AUTH-016):

```yaml
auth:
  degraded:
    policy: fail_closed          # default
    # policy: fail_open_bounded
    # staleness_ceiling: 60s     # required iff fail_open_bounded
    heartbeat_timeout: 10s
```

**`fail_closed` (default, NFR-SEC-003):**

| Concern | Behavior in degraded mode |
| --- | --- |
| live deliveries, revocable-mode principals | suspended: `AuthorizePublish` suppresses with `reason="degraded"`; events are not queued for later (at-most-once holds; gap rules apply) |
| live deliveries, mode `none` principals | unaffected (nothing revocable, §3.5) |
| new `connection_init`, revocable modes | JWT/API-key validation still works locally; admission allowed, but deliveries are suspended like the rest |
| custom authorizer mode | admission continues (the hook is loopback-local, not bus-dependent); deliveries suspended |
| new subscribes | accepted and registered; delivery begins on recovery |
| local-publish-to-local-subscriber (bus partition, FR-FAN-006) | still suspended for revocable-mode principals — authorization staleness, not envelope transport, is the reason |

**`fail_open_bounded`:**

| Concern | Behavior in degraded mode |
| --- | --- |
| live deliveries | continue normally for at most `staleness_ceiling` from degraded entry, measured on the monotonic clock |
| at ceiling expiry | identical to `fail_closed` from that instant (AUTHZ-009 asserts the flip) |
| revocations already applied | remain fully in force throughout — degraded mode never un-applies anything |
| ceiling accounting | per tenant, from that tenant's degraded-entry instant; not reset by a transient heartbeat without full reconciliation |

### 10.3 Health and readiness surfacing

Degraded entry and exit are visible before they are visible as client harm
(PRODUCT_REQUIREMENTS §8): `/readyz` fails for the affected tenants with
reason `auth_degraded` (FR-ADMIN-005), the
`conduit_auth_degraded{tenant,policy}` gauge flips to 1, a structured log
records entry with policy and timeout values, and suspended deliveries accrue
to `conduit_deliveries_suppressed_total{reason="degraded"}`.

### 10.4 Recovery on heal

Heartbeats resuming is necessary but not sufficient: the node may have missed
revocations while partitioned. Recovery is full revocation-set reconciliation
via a state snapshot over the bus:

1. On the first post-gap heartbeat, the node publishes on
   `conduit.<tenant>.ctl.snapshot_req`:

   ```json
   { "version": 1, "kind": "revocation_snapshot_request",
     "request_id": "01J9Z0FQ8N3M", "node_id": "gw-7", "tenant": "acme",
     "high_watermark_revocation_id": "01J9XYAG5T2K" }
   ```

2. Peer nodes serving the tenant (and the admin plane, which holds the
   authoritative journal) respond on `conduit.<tenant>.ctl.snapshot`:

   ```json
   { "version": 1, "kind": "revocation_snapshot",
     "request_id": "01J9Z0FQ8N3M", "responder_node_id": "gw-2",
     "complete": true, "high_watermark_revocation_id": "01J9Z0G11PXA",
     "entries": [
       { "kind": "scope", "id": "user-8812:orders:read",
         "issued_at": "2026-08-30T14:00:02Z",
         "revocation_id": "01J9XYAG5T2K",
         "expires_at": "2026-08-30T15:00:02Z" } ] }
   ```

   Responses over 1 MiB chunk with `complete: false` continuations sharing
   the `request_id`; the final chunk carries `complete: true`.
3. The recovering node collects responses for a bounded window (default
   5 s) and applies the **union** of all received entries through the
   normal idempotent `RevocationApplier.Apply` (epoch advances once per
   first-seen `revocation_id`). Union-only application means a forged or
   truncated snapshot can add pending revocations but can never remove or
   shrink anything (AUTHZ-026); bus access itself is credentialed and
   TLS-protected per NFR-SEC-005.
4. Only after the window closes does the node exit degraded mode: resume
   deliveries, flip `/readyz` and the gauge, log recovery with the count of
   newly applied entries. Under `fail_closed` the suspension window is
   partition + timeout + reconciliation window, all visible in metrics.

## 11. Tenancy

Per ADR-0009, binding. Status: planned; structural from R1, adversarial
evidence R3.

### 11.1 The structural-isolation argument

Tenant isolation is not a runtime `if` that could be skipped; it is the
absence of a code path (FR-AUTH-017, NFR-SEC-006). Concretely:

- the predicate index is sharded per tenant; the shard map is keyed by the
  tenant of the connection's principal at registration and by the envelope's
  single tenant at match time. The index exposes no API that takes a tenant
  list, no global iteration on the match path, and no shard lookup that
  consults client-supplied data — only the principal's tenant
  (server-minted, §3) and the envelope's tenant (validated, FR-FAN-002);
- bus subjects are tenant-scoped (`conduit.<tenant>.pub.<field>`,
  `conduit.<tenant>.ctl.*`, FR-FAN-009); a node never subscribes to a
  tenant it does not serve, so cross-tenant envelopes do not arrive;
- replay buffers, quotas, revocation sets, and admin visibility are all
  tenant-keyed the same way.

A tenant-A subscription can therefore appear in a tenant-B candidate set only
if the shard map itself is corrupted — which is what the AUTHZ-003
instrumented-index probe hunts, per ADR-0009's consequence clause, rather
than trusting this argument.

### 11.2 Tenant resolution and configuration modes

Principal tenant comes from the configured OIDC claim, the API-key record, or
the custom authorizer response — never from a client-writable header or init
field. When multi-tenancy is enabled and the tenant claim is missing or
empty, `connection_init` is rejected with close 4403 (ADR-0009; AUTHZ-003
admission variant). When multi-tenancy is disabled, a missing tenant resolves
to the implicit `default` tenant.

| Mode | Schema | Isolation surfaces |
| --- | --- | --- |
| single-tenant | one SDL set | implicit `default` tenant everywhere; the tenant dimension still exists in every structure (ADR-0009: carrying the key costs little now) |
| multi-tenant, shared schema | one SDL set for all tenants | per-tenant index shards, subjects, quotas, replay buffers; rules may reference `principal.tenant` |
| multi-tenant, per-tenant schema | distinct SDL set per tenant | everything above, plus per-tenant rules and `@auth` bindings validated per schema at startup |

### 11.3 Explicit non-guarantees

Verbatim commitments from ADR-0009: tenants share process memory, CPU, GC
behavior, and bus bandwidth. A noisy tenant can degrade latency for others up
to the protection quotas provide. Compliance regimes requiring hard isolation
must deploy per-tenant fleets. This document makes no cross-tenant
performance-isolation claim, and per the claims-ladder rules neither may any
README or marketing text.

## 12. Bypass-Resistance Argument

NFR-SEC-002 requires this section: every path by which event or query data
can reach a client, the named enforcement point or redaction rule it crosses,
and the adversarial test that proves it. The enumeration is claimed complete;
adding a data path to Conduit requires adding a subsection here in the same
change, and the R3 evidence checklist includes a review item asserting the
enumeration matches the code's egress points.

### 12.1 Query/mutation execution over HTTP

Path: HTTP POST → `AuthMode.Authenticate` (per request) → executor →
`FieldAuthorizer.AuthorizeField` per field before its resolver (§7). There is
no unauthenticated HTTP execution path: a request without a credential fails
authentication unless the operation touches only `unauthenticated_fields`,
and even then it passes the same `FieldAuthorizer` walk (default-deny with an
allowlist is not a bypass). Evidence: AUTHZ-023 (denial uniformity,
existence-oracle probe), AUTHZ-027 (deny-shortcut ordering), plus R1 executor
conformance for null propagation.

### 12.2 Query/mutation execution over WebSocket

Path: `Subscribe` message carrying a query/mutation document → the same
executor, the same `FieldAuthorizer.AuthorizeField`. The transport differs;
the execution pipeline is one code path (single-pipeline claim, checked by an
architectural test that the WS handler has no executor entry point of its
own). The principal is the connection's, from `connection_init`. Evidence:
AUTHZ-011 (no execution before `connection_ack`), AUTHZ-012 (no credential
swap via second init), AUTHZ-023 re-run over WS transport.

### 12.3 Subscription live delivery

Path: bus envelope → tenant shard match → candidate →
`SubscriptionAuthorizer.AuthorizePublish` → enqueue (§6). No candidate
reaches a queue without the call (§6.3); the epoch-check-at-enqueue rule
closes the probe/enqueue races. Evidence: AUTHZ-001 (revoke-then-publish),
AUTHZ-002 (stale cache), AUTHZ-003 (cross-tenant), AUTHZ-013 (expiry race),
AUTHZ-020 (scope partial revocation).

### 12.4 Resume replay

Path: resume token → token verification (HMAC, versioned, gate R7) →
`AuthorizeSubscribe` re-registration → buffered envelopes through
`AuthorizePublish` (§6.4). A resume token is a position claim, never an
authorization claim: it can only narrow (which envelopes to replay), never
widen (whether any envelope may be delivered), because delivery authority
comes exclusively from the fresh credential presented at reconnect.
Evidence: AUTHZ-005 (expiry mid-replay), AUTHZ-021 (cross-tenant and
cross-field token reuse), AUTHZ-001 replay variant (revoked principal
replays nothing).

### 12.5 Admin inspection and injection endpoints

Path: separate admin listener → `AdminAuthenticator.Authenticate` (bearer or
mTLS, FR-ADMIN-001) → endpoint handlers. Two exposures:

- **Inspection** (`/admin/v1/connections`, `/subscriptions`,
  `/diagnostics`): responses carry principal subject, tenant, counts, queue
  depths, ages — payload contents are never exposed (FR-ADMIN-002,
  FR-ADMIN-007), enforced by response types that contain no payload field,
  not by scrubbing.
- **Injection** (`/admin/v1/publish`): injected envelopes traverse the same
  validation, matching, and `AuthorizePublish` path as mutation-driven
  publishes (FR-FAN-010) — an operator can inject events, but cannot make
  an unauthorized client receive one.

Admin endpoints never exist on the client listener (architecturally checked:
the client listener's mux has no admin routes registered). Evidence:
AUTHZ-022 (client-listener and unauthenticated admin probes), AUTHZ-024
(diagnostics-bundle canary).

### 12.6 Error messages

Path: any failure → `ErrorRedactor.Redact` → client. Uniform authorization
failures (FR-AUTH-018): typed category only (`FORBIDDEN`, `TOKEN_EXPIRED`,
`GRANT_REVOKED`), no rule names, no claim names, no scope lists, no existence
oracles for hidden fields when introspection is disabled. Close reasons echo
no client bytes (FR-SUB-008 pattern) and carry no grant detail beyond the
close code. The redactor is the single construction site for client-visible
error payloads; handlers cannot format their own. Evidence: AUTHZ-023, plus
the R1 error-redaction rows (FR-GQL-012).

### 12.7 Metrics, logs, and traces

Path: every sink → `SinkRedactor.Scrub`. No secret material — credentials,
tokens, key bytes, JWKS keys, API-key plaintext — and no event payload bytes
appear in logs, traces, metrics, errors, or diagnostics bundles
(NFR-SEC-004). Metrics labels come from a closed vocabulary (tenant within
the cardinality budget, field, reason codes); no label value is ever
client-supplied free text. Audit records (§13) carry subjects and rule names
— operator-facing, on the admin/log plane, never in client-visible output.
Evidence: AUTHZ-024 (canary secrets at every sink; NFR-SEC-004 is
canary-tested per sink); log-amplification bounds under NFR-SEC-009 are
owned by R6.

## 13. Audit

FR-AUTH-009: every decision produces an auditable trace record class under
the logging budget. Status: planned, gate R3 (budget mechanics R8).

Decision trace record (structured, `slog` JSON):

```json
{ "ts": "2026-08-30T14:01:07.412Z", "record": "authz_decision",
  "decision_point": "SubscriptionAuthorizer.AuthorizePublish",
  "decision": "deny", "reason_class": "revoked",
  "rule": "own-orders-only", "tenant": "acme",
  "principal_subject": "user-8812", "auth_mode": "oidc", "epoch": 4211,
  "field": "orderUpdates", "subscription_id": "c81-s4",
  "connection_id": "c81", "revocation_id": "01J9XYAG5T2K",
  "latency_us": 14 }
```

Rules:

- **Deny is always recorded.** Every deny at every decision point emits a
  record. Deny records are rate-limited per (tenant, reason_class) with an
  explicit drop counter
  (`conduit_audit_records_dropped_total{class="deny"}`) so a hostile client
  cannot weaponize denials into log amplification (NFR-SEC-009); dropped
  records are counted, never silent.
- **Allow is sampled.** Default 1 in 100 per (tenant, decision_point),
  configurable per tenant: steady state must not pay full audit cost at
  fanout rates. Sampled-out allows still increment
  `conduit_authz_decisions_total{decision="allow",...}`, so aggregate
  counts stay exact while per-decision records are sampled.
- **Inputs are structured** (FR-AUTH-009): the record carries references —
  rule name, epoch, revocation_id — never claim values, credential
  material, or payload bytes (`SinkRedactor.Scrub` applies here too).
- **Budget interaction**: audit records draw from the same logging budget
  as all structured logs (OPERATIONS_TEST_PLAN §observability owns the
  budget table); the deny rate-limit and allow sampling are the two knobs
  that keep worst-case audit volume inside it, and both knobs' effective
  values appear in `/admin/v1/config` output.
- Admin-originated mutations (revoke, publish, drain) carry actor identity
  and request ID per FR-ADMIN-008, correlating with `revocation_id` in
  decision records.

## 14. Adversarial Evidence Table

Every row is a planned test in the owning gate's evidence checklist. Test IDs
are stable contracts cited by §2.3 and §12. "First failing condition" names
what breaks first in a correct implementation when the attack runs.

| Test ID | Attack | First failing condition | Required passing assertion | Gate |
| --- | --- | --- | --- | --- |
| AUTHZ-001 | revoke-then-publish race: revoke a principal, publish a matching event within the same millisecond window, repeat under load; the replay variant runs the same race against resume replay | `AuthorizePublish` step 4 finds the revocation-set entry (epoch already advanced by `RevocationApplier.Apply`) | zero deliveries with enqueue timestamp ≥ node-local apply timestamp, across ≥ 10^5 interleaved trials under the race detector | R3 |
| AUTHZ-002 | stale-cache probe: warm the decision cache with an allow, revoke, republish the identical envelope | cache probe key `(entry, epoch)` misses because the epoch advanced | no cache hit under an orphaned epoch, ever; suppression counter increments; re-evaluation denies | R3 |
| AUTHZ-003 | cross-tenant probe: tenant-A subscription with predicates identical to tenant-B's, publish in B; admission variant presents a tenant-less token with multi-tenancy enabled | per-tenant shard lookup returns only B's shard; A's entry is structurally unreachable; tenant-less init rejected 4403 | instrumented index proves A's entry was never visited; zero A deliveries across the full predicate grammar; re-run in every supported auth configuration (§6.3) | R3 |
| AUTHZ-004 | expired-token subscribe: JWT with `exp` past (beyond ±30 s skew) at init, and a token expiring between init and `Subscribe` | validation checklist item 6 at init; `AuthorizeSubscribe` step 3 for the mid-window case | init rejected / subscribe denied `TOKEN_EXPIRED`; no registry or index registration occurred | R3 |
| AUTHZ-005 | expiry mid-replay: resume with a credential that expires while buffered envelopes are still replaying | `AuthorizePublish` step 2 suppresses the first post-`T` replayed envelope | replay halts at `T`; `TOKEN_EXPIRED` errors then close 4403; zero replayed deliveries timestamped ≥ `T` | R7 (asserting R3 point) |
| AUTHZ-006 | forged rule reference: SDL with `@auth(rule: "nonexistent")` via startup and via hot reload | startup/reload validation rule 1 (§2.6) | startup aborts naming SDL location and rule; hot reload rejects atomically, old schema keeps serving (FR-OPS-003) | R3 |
| AUTHZ-007 | argument spoofing against claim binding: subscribe to `orderUpdates(customerId: "victim")` holding `customer_id: "attacker"` | `field.argument equalsClaim` leaf false at `AuthorizeSubscribe` step 6 | subscribe denied; a forged entry injected directly into the index (test-only seam) still yields zero deliveries via publish-time re-evaluation of the same leaf | R3 |
| AUTHZ-008 | authorizer timeout: custom authorizer hangs, drips bytes, returns 200 with malformed JSON, or returns an unknown version | 2 s deadline / response schema validation (§3.4 rule 1) | all four variants deny (fail-closed) at both admission and `custom.authorize` leaves; no goroutine leak after 10^4 timeouts | R3 |
| AUTHZ-009 | degraded-mode fail-open ceiling: partition the control channel under `fail_open_bounded`, hold past `staleness_ceiling` | ceiling timer flips the tenant to suspend at exactly `staleness_ceiling` on the monotonic clock | deliveries continue during the window, suspend at ceiling; already-applied revocations enforced throughout; flip visible in `/readyz`, gauge, and logs | R5 |
| AUTHZ-010 | epoch rollback attempt: replay captured `ctl.revoke` messages, redeliver duplicates, send stale snapshots with old high-watermarks | `Apply` idempotency on `revocation_id`; epoch is increment-only with no assignment path | epoch never decreases; a duplicate `revocation_id` advances the epoch zero additional times; the revocation set never shrinks from any bus input | R3 (bus redelivery R5) |
| AUTHZ-011 | pre-ack subscribe: send `Subscribe` before `connection_init` / before `connection_ack` | R2 state table rejects the transition | close 4401; `AuthorizeSubscribe` never invoked (asserted by instrumentation); no principal-less evaluation exists | R2 |
| AUTHZ-012 | credential swap: second `connection_init` with a higher-privilege token on a live connection | R2 duplicate-init rule | close 4429; original principal unchanged (§4.2); no subscription re-evaluates under new claims | R2 |
| AUTHZ-013 | exact-instant expiry delivery: freeze the injected clock so expiry lands between cache probe and enqueue | enqueue gate (§6.1 step 7) re-checks the expired flag | zero deliveries with enqueue timestamp ≥ `T` under exhaustive interleavings of probe/expiry/enqueue | R3 |
| AUTHZ-014 | JWT `alg` confusion: `alg: none`, HMAC-signed token against an RSA JWKS key, stripped signature | validation checklist item 2 (allowlist) before any verification | all variants denied at parse/header stage; no signature verification attempted with a confused key type | R3 |
| AUTHZ-015 | kid flood: tokens with thousands of distinct unknown `kid` values | single-flight + `kid_miss_min_interval` rate limit (§3.2 step 4) | at most one JWKS fetch per interval per issuer regardless of flood size; all unknown-kid tokens denied; JWKS request count bounded and asserted | R3 |
| AUTHZ-016 | issuer/audience confusion: valid signature from the right key but wrong `iss`, or `aud` lacking the configured audience | checklist items 4–5 | denied; the deny audit record names the failing check; the client sees only the uniform failure | R3 |
| AUTHZ-017 | `nbf` abuse: token valid only in the future beyond skew, presented now | checklist item 7 | denied at init; accepted once the injected clock advances within skew | R3 |
| AUTHZ-018 | API-key timing oracle: measure response-time distributions for wrong-key-ID vs right-ID-wrong-secret presentations | constant-time hash comparison; uniform work per attempt | statistical timing test (≥ 10^6 samples) shows no distinguishable distribution between the two failure classes | R3 |
| AUTHZ-019 | revoked API key on a fresh connection: present a key revoked seconds ago on a node that received the revocation via bus | `Authenticate` consults revocation state; `AuthorizeSubscribe` step 4 backstops | admission denied on every fleet node once applied; the R5 variant asserts within-SLO denial fleet-wide | R3, R5 |
| AUTHZ-020 | scope-revocation blast radius: subject holds subscriptions under `orders:read` and `inventory:read`; revoke `<subject>:orders:read` | rule re-evaluation with the revoked scope pair false | orders subscriptions get `GRANT_REVOKED` and unregister; inventory subscriptions receive every subsequent event; connection stays open | R3 |
| AUTHZ-021 | resume-token misuse: replay a valid token on a different tenant's connection, a different field, a different subject, and with a tampered position | token verification (tenant/field binding, HMAC) then `AuthorizeSubscribe` on the fresh credential | typed rejection with `resume_rejected`, subscription proceeds fresh (FR-RESUME-007); zero replayed envelopes cross tenants or fields; forgeries logged | R7 |
| AUTHZ-022 | admin-surface probe: request every `/admin/v1/*` route on the client listener; request admin routes with missing or expired admin credentials | client listener mux has no admin routes; `AdminAuthenticator.Authenticate` on the admin listener | client listener returns a generic 404 for all admin paths (no existence oracle); admin listener returns 401 with zero state change; both probes appear in audit | R3 (surface R8) |
| AUTHZ-023 | error-oracle probe: compare responses for (a) denied existing field, (b) nonexistent field, (c) denied subscribe, with introspection disabled | `ErrorRedactor.Redact` uniform construction | byte-identical error shape and code for (a) and (b); no rule/claim/scope names in ≥ 10^3 generated denial variants (property test) | R3 |
| AUTHZ-024 | secret-canary sweep: plant canaries as a JWT, an API key, a JWKS private component, and an event-payload marker; drive all failure paths; harvest every sink | `SinkRedactor.Scrub` at each sink; payload-free admin response types | zero canary bytes in logs, metrics output, traces, error responses, close reasons, `/admin/v1/*` responses, and diagnostics bundles (NFR-SEC-004) | R3 (sinks added later re-run R6/R8) |
| AUTHZ-025 | mode-none smuggling: configuration with `auth.mode: none` lacking `development_acknowledged: true`; and with it, verify visibility | startup config validation (FR-AUTH-004) | startup fails naming the missing key; with the key, a startup warning names the listener and `/admin/v1/config` shows the mode | R3 |
| AUTHZ-026 | snapshot forgery/truncation: during heal, respond with a snapshot omitting known revocations and one containing fabricated extras | union-only application through idempotent `Apply` (§10.4 step 3) | the set never shrinks; omissions cannot remove entries other peers report; fabricated extras only add suppression (fail-closed direction); the node exits degraded only after the full window | R5 |
| AUTHZ-027 | deny-shortcut ordering bypass: craft a principal matching both a broad `allow` and a `deny` shortcut; permute rule declaration order in configuration | deny rules evaluate before any allow, independent of declaration order (§2.4) | denied under every declaration-order permutation at all three decision points; property test over generated rule sets | R3 |

## 15. Deferrals and Requirements Traced

### 15.1 Explicit deferrals

Each entry is `deferred` per the status vocabulary and forbidden from being
used to claim any gate complete; reopen mechanics live in OPEN_QUESTIONS.

1. **CEL expression language for rules.** v1 rules are the closed YAML
   condition grammar of §2.4 by design: a full expression language
   multiplies the audit, validation, and bypass-analysis surface before the
   two decision points have adversarial evidence behind them. Operators
   needing more use the custom authorizer hook. Reopen trigger recorded in
   OPEN_QUESTIONS (operator demand the hook's latency profile cannot serve).
2. **In-band token refresh.** No protocol message refreshes a live
   connection's credential in v1 (ADR-0008): refresh is reconnect with
   resume-token continuity. Reopen triggers per ADR-0008: a protocol-level
   refresh adopted upstream in `graphql-transport-ws`, or measured
   reconnect load from expiry churn exceeding 5 % of connection churn.
3. **Per-field masking within event payloads.** v1-unsupported, stated as a
   rule: an event field either passes the field rule or the delivery is
   suppressed entirely. Conduit never rewrites, redacts, or partially
   delivers an event payload — delivery is all-or-nothing per candidate.
   Operators wanting per-recipient shaping publish distinct fields, each
   gated by its own rule. Deferred because payload rewriting on the fanout
   hot path is a latency and correctness surface (a masking bug is a data
   leak) that v1 refuses to carry.

### 15.2 Requirements traced

Terminal gate ownership per PRODUCT_REQUIREMENTS §10.3: R3 owns 16 of 18
FR-AUTH requirements; R5 owns FR-AUTH-014 and the fleet behavior of
FR-AUTH-015. Status of every row: planned.

| Requirement | Where addressed | Owning gate |
| --- | --- | --- |
| FR-AUTH-001 | §3.2 (JWKS algorithm, validation checklist, claim mapping) | R3 |
| FR-AUTH-002 | §3.3 (salted-hash store, presentation forms, rotation) | R3 |
| FR-AUTH-003 | §3.4 (versioned contract, 2 s fail-closed, TTL) | R3 |
| FR-AUTH-004 | §3 intro, §3.5 (per-listener modes, `none` acknowledgment) | R3 |
| FR-AUTH-005 | §4.1–4.2 (normative principal, immutability, no credentials) | R3 |
| FR-AUTH-006 | §5 (`AuthorizeSubscribe` before registration) | R3 |
| FR-AUTH-007 | §7 (per-field evaluation, null propagation) | R3 |
| FR-AUTH-008 | §2.4–2.6 (named structured rules, startup validation) | R3 |
| FR-AUTH-009 | §2.4 (structured decisions), §13 (trace records, budget) | R3 |
| FR-AUTH-010 | §6 (`AuthorizePublish`, no-skip rule) | R3 |
| FR-AUTH-011 | §6.2, §4.3 (epoch-keyed cache, invalidation) | R3 |
| FR-AUTH-012 | §8 (warning ping, fail-closed, `TOKEN_EXPIRED`, 4403) | R3 |
| FR-AUTH-013 | §9.1–9.2 (`GRANT_REVOKED`, 4403, no delivery after apply) | R3 |
| FR-AUTH-014 | §9.1 step 7 (control subject, p99 ≤ 2 s, R5 measurement) | R5 |
| FR-AUTH-015 | §10.1–10.3 (heartbeat timeout, both policies, visibility) | R3 node-local, R5 fleet |
| FR-AUTH-016 | §10.2 (explicit config, no silent default change) | R3 |
| FR-AUTH-017 | §11.1 (structural isolation, no cross-tenant path) | R3 |
| FR-AUTH-018 | §5.1 step 7, §7 item 4, §12.6 (uniform failures, no oracles) | R3 |

NFR touchpoints:

| Requirement | Touchpoint in this document |
| --- | --- |
| NFR-SEC-002 | §2.3 is the required "§enforcement" registry; §12 gives the per-path bypass argument; §14 binds every claim to adversarial evidence in its owning gate |
| NFR-SEC-003 | §9.1 step 7 (SLO and its R5 measurement method); §10.2 (`fail_closed` default, stated in exactly one place — the configuration block — to prevent drift) |
| NFR-SEC-006 | §11 (structural argument, config modes, ADR-0009 non-guarantees); AUTHZ-003 and the instrumented-index probe as gate evidence |

Anything in this document that later conflicts with an accepted ADR or with
PRODUCT_REQUIREMENTS.md resolves by the documentation set's conflict rules
(README §Conflict and Status Rules); a deliberate change to a decision
recorded in ADR-0008 or ADR-0009 requires a superseding ADR, not an edit
here.
