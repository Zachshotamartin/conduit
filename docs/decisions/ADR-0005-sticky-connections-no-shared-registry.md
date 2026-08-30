# ADR-0005: Sticky Connection Ownership, No Shared Registry

- Status: accepted
- Date: 2026-08-30
- Related findings or requirements: FR-FAN-002, FR-FAN-006, FR-RESUME-004,
  NFR-SCALE-003, gate R5

## Context

Subscription state must live somewhere. The candidates: (a) each connection's
subscriptions live only on the node holding the socket, and every node
receives every publish envelope and matches locally; (b) a shared external
registry (e.g. Redis) maps subscription predicates to nodes so publishes are
routed only to interested nodes; (c) subscriptions are assigned to nodes by
consistent hashing independent of the socket, with intra-fleet forwarding.

The deciding pressures: what node loss costs, what subscribe/unsubscribe
churn costs, whether matching state can be stale, and how much the load
balancer must know.

## Decision

Connection state is sticky to the node that owns the socket. There is no
shared registry. Every node subscribes to all tenant publish subjects on the
bus and matches every envelope against its local predicate index. The load
balancer needs no subscription awareness — only ordinary long-lived-TCP
affinity by connection.

What this costs on node loss, stated as contract: all connections on the
lost node drop; their subscription state is gone with them; clients recover
by reconnecting (through the load balancer, to any surviving node) and
presenting resume tokens; the gap window rules of ADR-0007 apply. No state
migration, hand-off, or fleet rebalancing occurs.

## Alternatives Considered

- **Shared registry (Redis/etcd)**: rejected. Every subscribe/unsubscribe
  becomes a fleet-visible write; registry staleness during partitions
  creates matching decisions based on state the owning node no longer has;
  the registry becomes a second availability domain that can take down
  fanout; and per-node local matching is still required for correctness, so
  the registry only optimizes bus traffic — an optimization the R9 numbers
  can justify later if envelope broadcast measurably saturates the bus.
- **Consistent-hash subscription placement with forwarding**: rejected.
  It adds an intra-fleet hop on the delivery path (latency budget), makes
  node loss reassign subscription ranges while sockets stay elsewhere, and
  couples correctness to ring agreement during exactly the partitions R5
  must survive.
- **Sticky routing at the LB by subscription content**: rejected; it pushes
  Conduit semantics into infrastructure Conduit does not ship.

## Consequences

Bus traffic scales with fleet-wide publish rate times node count (every node
sees every envelope). This is the accepted cost; the R9 capacity model must
publish measured bus bandwidth per node at the target publish rate so
operators can size fleets honestly. Reconnect storms after node loss are a
first-class load case: R7 owns paced-reconnect mitigation and R9 measures a
node-loss reconnect surge. Matching cost per node is bounded by the predicate
index (ADR-0006), which is what makes envelope broadcast viable.
