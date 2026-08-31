// Package schema owns immutable SDL loading and Conduit directive metadata.
// R1.02 validates @auth and @backpressure but does not enforce them. It stores
// a nonempty backpressure coalesceKey without compiling a response path (R6),
// and restricts complexity multipliers to same-field Int arguments without
// performing request-time arithmetic (R1.05). Config binding begins in R1.03.
package schema
