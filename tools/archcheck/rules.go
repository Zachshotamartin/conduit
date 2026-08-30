package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Rule is executable architecture policy. Package is a package-selector: a
// literal matches one package, /** includes descendants, braces express
// alternatives, and a leading ! excludes a pattern. MayImport confines a
// target to matching packages, MustNotImport denies matching targets from
// matching packages, and MustImport requires matching packages to have a
// direct import. Targets are import-path patterns or syntax: pseudo-paths.
type Rule struct {
	ID            string
	Package       string
	MayImport     []string
	MustNotImport []string
	MustImport    []string
	Reason        string
}

var architectureRules = []Rule{
	{ID: "ARCH-01", Package: "{internal/transport,test/**}", MayImport: []string{"github.com/coder/websocket/**"}, Reason: "coder/websocket must be imported only by internal/transport"},
	{ID: "ARCH-02", Package: "{internal/graphql/ast,test/**}", MayImport: []string{"github.com/vektah/gqlparser/v2/**"}, Reason: "gqlparser/v2 must be imported only by internal/graphql/ast"},
	{ID: "ARCH-03", Package: "{internal/bus/nats,test/**}", MayImport: []string{"github.com/nats-io/nats.go/**"}, Reason: "nats.go must be imported only by internal/bus/nats"},
	{ID: "ARCH-04", Package: "{internal/datasource/postgres,test/**}", MayImport: []string{"github.com/jackc/pgx/v5/**"}, Reason: "pgx/v5 must be imported only by internal/datasource/postgres"},
	{ID: "ARCH-05", Package: "internal/{transport,protocol}/**", MustNotImport: []string{"internal/graphql/**"}, Reason: "transport and protocol must pass operations upward instead of importing internal/graphql"},
	{ID: "ARCH-06", Package: "internal/graphql/**", MustNotImport: []string{"internal/transport/**", "internal/protocol/**", "internal/queue/**"}, Reason: "internal/graphql must emit results instead of importing transport, protocol, or queue"},
	{ID: "ARCH-07", Package: "internal/platform/**", MayImport: []string{"syntax:build-constraint", "syntax:selector:runtime.GOOS"}, Reason: "build tags and runtime.GOOS checks must be confined to internal/platform"},
	{ID: "ARCH-08", Package: "internal/{transport,protocol}/**", MustNotImport: []string{"internal/admin/**"}, Reason: "client transport and protocol must not depend on internal/admin"},
	{
		ID:      "ARCH-09",
		Package: "{cmd/conduit,cmd/conduit-loadgen,test/**}",
		MayImport: []string{
			"internal/datasource/{postgres,http,function}/**",
			"internal/bus/{nats,memory}/**",
			"internal/auth/{oidc,apikey,custom}/**",
		},
		Reason: "consumers must import ports, never adapter implementations",
	},
	{ID: "ARCH-10", Package: "{internal/observability,cmd/conduit,test/**}", MayImport: []string{"go.opentelemetry.io/**", "github.com/prometheus/client_golang/**"}, Reason: "OTel and Prometheus SDKs must be confined to internal/observability and cmd/conduit"},
	{ID: "ARCH-11", Package: "internal/**", MustNotImport: []string{"test/**"}, Reason: "internal packages must not import test packages"},
	{
		ID:      "ARCH-12",
		Package: "{internal/**,!internal/clock/**}",
		MustNotImport: []string{
			"syntax:selector:time.Now",
			"syntax:selector:time.After",
			"syntax:call:math/rand.*",
			"syntax:call:crypto/rand.*",
		},
		Reason: "domain packages must use injected clocks and seeded randomness",
	},
	{
		ID:      "ARCH-13",
		Package: "cmd/conduit-loadgen/**",
		MustNotImport: []string{
			"internal/registry/**",
			"internal/fanout/**",
			"internal/queue/**",
			"internal/datasource/{postgres,http,function}/**",
			"internal/bus/nats/**",
			"internal/auth/{oidc,apikey,custom}/**",
		},
		Reason: "conduit-loadgen is a client and must not import registry, fanout, or queue",
	},
	{ID: "ARCH-14", Package: "internal/protocol/**", MustNotImport: []string{"internal/datasource/**"}, Reason: "internal/protocol must not import internal/datasource"},
	{ID: "ARCH-15", Package: "internal/{queue,registry}/**", MustNotImport: []string{"internal/bus/**"}, Reason: "internal/queue and internal/registry must not import internal/bus"},
	{ID: "ARCH-16", Package: "internal/admin/**", MustNotImport: []string{"internal/transport/**"}, Reason: "internal/admin must not import internal/transport because the admin listener stack is separate"},
}

// sinkOwnerInventory is exhaustive for the sink-owning package boundaries
// named by the specifications. A candidate becomes active when it gains any
// production Go file other than doc.go and must then move into Owners.
type sinkOwnerInventory struct {
	SchemaVersion int      `json:"schema_version"`
	Candidates    []string `json:"candidates"`
	Owners        []string `json:"owners"`
}

//go:embed sink-owners.json
var sinkOwnerInventoryJSON []byte

func loadDefaultRules() ([]Rule, error) {
	rules, _, err := loadDefaultPolicy()
	return rules, err
}

func loadDefaultPolicy() ([]Rule, sinkOwnerInventory, error) {
	inventory, err := parseSinkOwnerInventory(sinkOwnerInventoryJSON)
	if err != nil {
		return nil, sinkOwnerInventory{}, fmt.Errorf("load sink-owner inventory: %w", err)
	}
	rules := cloneRules(architectureRules)
	rules = append(rules, redactionRule(inventory.Owners))
	return rules, inventory, nil
}

func redactionRule(owners []string) Rule {
	return Rule{
		ID:         "ARCH-17",
		Package:    ownerSelector(owners),
		MustImport: []string{"internal/observability/redaction"},
		Reason:     "every declared sink owner must directly import internal/observability/redaction",
	}
}

func parseSinkOwnerInventory(data []byte) (sinkOwnerInventory, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var inventory sinkOwnerInventory
	if err := decoder.Decode(&inventory); err != nil {
		return sinkOwnerInventory{}, fmt.Errorf("decode JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return sinkOwnerInventory{}, err
	}
	if err := validateSinkOwnerDefinition(inventory); err != nil {
		return sinkOwnerInventory{}, err
	}
	return inventory, nil
}

func validateSinkOwnerDefinition(inventory sinkOwnerInventory) error {
	if inventory.SchemaVersion != 1 {
		return fmt.Errorf("schema_version = %d, want 1", inventory.SchemaVersion)
	}
	if inventory.Candidates == nil {
		return fmt.Errorf("candidates must be present as an array")
	}
	if len(inventory.Candidates) == 0 {
		return fmt.Errorf("candidates must name the normative sink-owner packages")
	}
	if inventory.Owners == nil {
		return fmt.Errorf("owners must be present as an array")
	}
	candidates, err := validateInventoryPaths("candidate", inventory.Candidates)
	if err != nil {
		return err
	}
	owners, err := validateInventoryPaths("owner", inventory.Owners)
	if err != nil {
		return err
	}
	for owner := range owners {
		if _, declared := candidates[owner]; !declared {
			return fmt.Errorf("owner %q is not declared in candidates", owner)
		}
	}
	return nil
}

func validateInventoryPaths(kind string, paths []string) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(paths))
	for _, packagePath := range paths {
		if !validOwnerPath(packagePath) {
			return nil, fmt.Errorf("%s %q is not an exact internal package path", kind, packagePath)
		}
		if _, duplicate := seen[packagePath]; duplicate {
			return nil, fmt.Errorf("%s %q is duplicated", kind, packagePath)
		}
		seen[packagePath] = struct{}{}
	}
	if !sort.StringsAreSorted(paths) {
		return nil, fmt.Errorf("%ss must be sorted", kind)
	}
	return seen, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return fmt.Errorf("inventory contains trailing JSON values")
}

func validOwnerPath(owner string) bool {
	if !strings.HasPrefix(owner, "internal/") || strings.HasSuffix(owner, "/") {
		return false
	}
	for _, part := range strings.Split(owner, "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "{}!*") {
			return false
		}
	}
	return true
}

func ownerSelector(owners []string) string {
	if len(owners) == 0 {
		return "!**"
	}
	if len(owners) == 1 {
		return owners[0]
	}
	return "{" + strings.Join(owners, ",") + "}"
}

// DefaultRules returns a defensive copy of the normative rule set. A malformed
// embedded inventory is a programmer error and fails immediately; CheckModule
// reports the same condition as an ordinary checker error.
func DefaultRules() []Rule {
	rules, err := loadDefaultRules()
	if err != nil {
		panic(err)
	}
	return rules
}

func cloneRules(source []Rule) []Rule {
	rules := make([]Rule, len(source))
	for i, rule := range source {
		rules[i] = rule
		rules[i].MayImport = append([]string(nil), rule.MayImport...)
		rules[i].MustNotImport = append([]string(nil), rule.MustNotImport...)
		rules[i].MustImport = append([]string(nil), rule.MustImport...)
	}
	return rules
}
