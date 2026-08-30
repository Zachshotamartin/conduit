package main

// Rule is the checked-in representation of one architecture boundary.
// Package, MayImport, and MustNotImport are path patterns interpreted by the
// checker; Reason is always included in a reported violation.
type Rule struct {
	ID            string
	Package       string
	MayImport     []string
	MustNotImport []string
	Reason        string
}

var architectureRules = []Rule{
	{
		ID:        "ARCH-01",
		Package:   "internal/transport",
		MayImport: []string{"github.com/coder/websocket/**"},
		Reason:    "coder/websocket must be imported only by internal/transport",
	},
	{
		ID:        "ARCH-02",
		Package:   "internal/graphql/ast",
		MayImport: []string{"github.com/vektah/gqlparser/v2/**"},
		Reason:    "gqlparser/v2 must be imported only by internal/graphql/ast",
	},
	{
		ID:        "ARCH-03",
		Package:   "internal/bus/nats",
		MayImport: []string{"github.com/nats-io/nats.go/**"},
		Reason:    "nats.go must be imported only by internal/bus/nats",
	},
	{
		ID:        "ARCH-04",
		Package:   "internal/datasource/postgres",
		MayImport: []string{"github.com/jackc/pgx/v5/**"},
		Reason:    "pgx/v5 must be imported only by internal/datasource/postgres",
	},
	{
		ID:            "ARCH-05",
		Package:       "internal/{transport,protocol}",
		MustNotImport: []string{"internal/graphql/**"},
		Reason:        "transport and protocol must pass operations upward instead of importing internal/graphql",
	},
	{
		ID:            "ARCH-06",
		Package:       "internal/graphql/**",
		MustNotImport: []string{"internal/transport/**", "internal/protocol/**", "internal/queue/**"},
		Reason:        "internal/graphql must emit results instead of importing transport, protocol, or queue",
	},
	{
		ID:            "ARCH-07",
		Package:       "** except internal/platform",
		MustNotImport: []string{"//go:build", "// +build", "runtime.GOOS"},
		Reason:        "build tags and runtime.GOOS checks must be confined to internal/platform",
	},
	{
		ID:            "ARCH-08",
		Package:       "internal/{transport,protocol}",
		MustNotImport: []string{"internal/admin/**"},
		Reason:        "client transport and protocol must not depend on internal/admin",
	},
	{
		ID:            "ARCH-09",
		Package:       "** except composition roots and tests",
		MustNotImport: []string{"internal/datasource/{postgres,http,function}/**", "internal/bus/{nats,memory}/**", "internal/auth/{oidc,apikey,custom}/**"},
		Reason:        "consumers must import ports, never adapter implementations",
	},
	{
		ID:        "ARCH-10",
		Package:   "internal/observability or cmd/conduit",
		MayImport: []string{"go.opentelemetry.io/**", "github.com/prometheus/client_golang/**"},
		Reason:    "OTel and Prometheus SDKs must be confined to internal/observability and cmd/conduit",
	},
	{
		ID:            "ARCH-11",
		Package:       "internal/**",
		MustNotImport: []string{"test/**"},
		Reason:        "internal packages must not import test packages",
	},
	{
		ID:            "ARCH-12",
		Package:       "internal/** except internal/clock",
		MustNotImport: []string{"time.Now", "time.After", "math/rand package functions", "crypto/rand package functions"},
		Reason:        "domain packages must use injected clocks and seeded randomness",
	},
	{
		ID:            "ARCH-13",
		Package:       "cmd/conduit-loadgen",
		MayImport:     []string{"wire codecs", "internal/bus/memory"},
		MustNotImport: []string{"internal/registry/**", "internal/fanout/**", "internal/queue/**"},
		Reason:        "conduit-loadgen is a client and must not import registry, fanout, or queue",
	},
}

var rulesByID = func() map[string]Rule {
	rules := make(map[string]Rule, len(architectureRules))
	for _, rule := range architectureRules {
		rules[rule.ID] = rule
	}
	return rules
}()

// DefaultRules returns a defensive copy of the normative rule set.
func DefaultRules() []Rule {
	rules := make([]Rule, len(architectureRules))
	for i, rule := range architectureRules {
		rules[i] = rule
		rules[i].MayImport = append([]string(nil), rule.MayImport...)
		rules[i].MustNotImport = append([]string(nil), rule.MustNotImport...)
	}
	return rules
}
