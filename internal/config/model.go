package config

import (
	"strings"
	"time"
)

// Config is the R0 effective-configuration tree. Later gates extend this
// tree and its schema without changing the loading and validation contract.
type Config struct {
	Listener   ListenerConfig
	Limits     LimitsConfig
	Connection ConnectionConfig
	Auth       AuthConfig
	Tenancy    TenancyConfig
}

// ListenerConfig contains listener settings.
type ListenerConfig struct {
	Client ClientListenerConfig
}

// ClientListenerConfig contains client-listener settings.
type ClientListenerConfig struct {
	Port                  int
	Transport             string
	PlaintextAcknowledged bool
}

// LimitsConfig contains bounded-input and queue settings.
type LimitsConfig struct {
	MaxMessageBytes    int
	OutboundQueueBytes int
}

// ConnectionConfig contains connection-lifecycle durations.
type ConnectionConfig struct {
	Keepalive   time.Duration
	IdleTimeout time.Duration
	DrainWindow time.Duration
}

// AuthConfig contains the R0 authentication validation surface.
type AuthConfig struct {
	Mode                    string
	DevelopmentAcknowledged bool
	ExpiryWarningWindow     time.Duration
	TokenLifetimeFloor      time.Duration
}

// TenancyConfig contains the R0 tenant-mode validation surface.
type TenancyConfig struct {
	Enabled    bool
	SchemaSets []string
}

// FieldSpec is the generated public metadata for one schema leaf.
type FieldSpec struct {
	Path        string
	Environment string
}

var schemaPaths = []string{
	"listener.client.port",
	"listener.client.transport",
	"listener.client.plaintext_acknowledged",
	"limits.max_message_bytes",
	"limits.outbound_queue_bytes",
	"connection.keepalive",
	"connection.idle_timeout",
	"connection.drain_window",
	"auth.mode",
	"auth.development_acknowledged",
	"auth.expiry_warning_window",
	"auth.token_lifetime_floor",
	"tenancy.enabled",
	"tenancy.schema_sets",
}

var environmentPaths = func() map[string]string {
	paths := make(map[string]string, len(schemaPaths))
	for _, path := range schemaPaths {
		paths[environmentName(path)] = path
	}
	return paths
}()

// Defaults returns a fresh configuration populated with the documented R0
// defaults. Returned slices never alias package state.
func Defaults() Config {
	return Config{
		Listener: ListenerConfig{
			Client: ClientListenerConfig{
				Port:      8080,
				Transport: "tls",
			},
		},
		Limits: LimitsConfig{
			MaxMessageBytes:    512 * 1024,
			OutboundQueueBytes: 1024 * 1024,
		},
		Connection: ConnectionConfig{
			Keepalive:   25 * time.Second,
			IdleTimeout: 5 * time.Minute,
			DrainWindow: time.Minute,
		},
		Auth: AuthConfig{
			Mode:                "oidc",
			ExpiryWarningWindow: time.Minute,
			TokenLifetimeFloor:  5 * time.Minute,
		},
	}
}

// SchemaFields returns an immutable snapshot of schema-derived field
// metadata. Environment names are generated from paths, never maintained in
// a parallel table.
func SchemaFields() []FieldSpec {
	fields := make([]FieldSpec, len(schemaPaths))
	for i, path := range schemaPaths {
		fields[i] = FieldSpec{Path: path, Environment: environmentName(path)}
	}
	return fields
}

// PathForEnvironment resolves a generated CONDUIT_* name to its schema path.
func PathForEnvironment(environment string) (string, bool) {
	path, ok := environmentPaths[environment]
	return path, ok
}

func environmentName(path string) string {
	return "CONDUIT_" + strings.ToUpper(strings.ReplaceAll(path, ".", "_"))
}

func schemaContains(path string) bool {
	for _, candidate := range schemaPaths {
		if candidate == path {
			return true
		}
	}
	return false
}

func schemaHasDescendant(path string) bool {
	prefix := path + "."
	for _, candidate := range schemaPaths {
		if strings.HasPrefix(candidate, prefix) {
			return true
		}
	}
	return false
}

func topLevelSections() map[string]struct{} {
	sections := make(map[string]struct{})
	for _, path := range schemaPaths {
		section, _, _ := strings.Cut(path, ".")
		sections[section] = struct{}{}
	}
	return sections
}
