package main

// DependencyPolicy is one approved direct runtime module, the first gate at
// which it may appear, and the sole package allowed to import it.
type DependencyPolicy struct {
	Module          string
	EarliestGate    string
	AllowedPackages []string
}

var runtimePolicies = []DependencyPolicy{
	{Module: "github.com/coder/websocket", EarliestGate: "R2", AllowedPackages: []string{"internal/transport"}},
	{Module: "github.com/jackc/pgx/v5", EarliestGate: "R1", AllowedPackages: []string{"internal/datasource/postgres"}},
	{Module: "github.com/lestrrat-go/jwx/v2", EarliestGate: "R3", AllowedPackages: []string{"internal/auth/oidc"}},
	{Module: "github.com/nats-io/nats.go", EarliestGate: "R5", AllowedPackages: []string{"internal/bus/nats"}},
	{Module: "github.com/prometheus/client_golang", EarliestGate: "R8", AllowedPackages: []string{"internal/observability", "cmd/conduit"}},
	{Module: "github.com/vektah/gqlparser/v2", EarliestGate: "R1", AllowedPackages: []string{"internal/graphql/ast"}},
	{Module: "go.opentelemetry.io/otel", EarliestGate: "R8", AllowedPackages: []string{"internal/observability", "cmd/conduit"}},
	{Module: "go.opentelemetry.io/otel/exporters/prometheus", EarliestGate: "R8", AllowedPackages: []string{"internal/observability", "cmd/conduit"}},
	{Module: "go.opentelemetry.io/otel/sdk", EarliestGate: "R8", AllowedPackages: []string{"internal/observability", "cmd/conduit"}},
	{Module: "gopkg.in/yaml.v3", EarliestGate: "R0", AllowedPackages: []string{"internal/config"}},
}

var licenseAllowlist = []string{"Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC", "MIT"}

// RuntimeAllowlist returns a defensive copy of the exact direct runtime
// dependency policy.
func RuntimeAllowlist() []DependencyPolicy {
	policies := make([]DependencyPolicy, len(runtimePolicies))
	for i, policy := range runtimePolicies {
		policies[i] = policy
		policies[i].AllowedPackages = append([]string(nil), policy.AllowedPackages...)
	}
	return policies
}

// AllowedLicenses returns a defensive copy of the automatic license
// allowlist. MPL-2.0 intentionally is not automatic: it needs a recorded
// maintainer review.
func AllowedLicenses() []string {
	return append([]string(nil), licenseAllowlist...)
}
