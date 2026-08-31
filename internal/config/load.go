package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// MaxFileBytes bounds configuration input before YAML parsing.
const MaxFileBytes = 1 << 20

// LoadOptions supplies the three override layers in the four-layer precedence
// chain. Environment contains process-style names; unrelated non-CONDUIT
// entries are ignored. Flags are keyed by schema path.
type LoadOptions struct {
	FilePath    string
	Environment map[string]string
	Flags       map[string]string
}

// Load parses, merges, and validates phases 1 through 3. On every failure it
// returns the zero Config so no partial effective configuration can escape.
func Load(options LoadOptions) (Config, error) {
	state := newLoadState()
	if options.FilePath != "" {
		if err := state.applyFile(options.FilePath); err != nil {
			return Config{}, err
		}
	}
	if err := state.applyEnvironment(options.Environment); err != nil {
		return Config{}, err
	}
	if err := state.applyFlags(options.Flags); err != nil {
		return Config{}, err
	}
	if err := state.validateCrossFields(); err != nil {
		return Config{}, err
	}
	return state.config, nil
}

func (state *loadState) applyFile(path string) error {
	data, err := readBoundedFile(path)
	if err != nil {
		return newValidationError(
			PhaseFileParse,
			"config",
			path,
			"existing readable configuration file",
			err,
		)
	}

	root, err := parseYAMLDocument(data)
	if err != nil {
		return newValidationError(PhaseFileParse, "$", path, "valid YAML document", err)
	}
	if root == nil {
		return nil
	}
	if containsAlias(root) {
		return newValidationError(PhaseFileParse, "$", path, "YAML document without aliases", nil)
	}
	root = resolvedNode(root)
	if root.Kind == yaml.ScalarNode && root.ShortTag() == "!!null" {
		return nil
	}
	if root.Kind != yaml.MappingNode {
		return newValidationError(
			PhaseFileParse,
			"$",
			path,
			"YAML mapping at the document root",
			nil,
		)
	}

	if err := validateTopLevel(root, path); err != nil {
		return err
	}
	return state.walkYAMLMapping(root, "", path)
}

func readBoundedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("configuration exceeds %d-byte bound", MaxFileBytes)
	}
	return data, nil
}

func parseYAMLDocument(data []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}

	var extra yaml.Node
	err := decoder.Decode(&extra)
	if err == nil {
		return nil, errors.New("multiple YAML documents are not allowed")
	}
	if !errors.Is(err, io.EOF) {
		return nil, err
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) == 0 {
		return nil, nil
	}
	return document.Content[0], nil
}

func validateTopLevel(root *yaml.Node, source string) error {
	sections := topLevelSections()
	seen := make(map[string]struct{}, len(root.Content)/2)
	for index := 0; index+1 < len(root.Content); index += 2 {
		keyNode := resolvedNode(root.Content[index])
		if keyNode.Kind != yaml.ScalarNode || keyNode.ShortTag() != "!!str" {
			return newValidationError(
				PhaseFileParse,
				"$",
				source,
				"string top-level configuration key",
				nil,
			)
		}
		key := keyNode.Value
		if _, duplicate := seen[key]; duplicate {
			return newValidationError(
				PhaseFileParse,
				key,
				source,
				"unique top-level configuration key",
				nil,
			)
		}
		seen[key] = struct{}{}
		if _, known := sections[key]; !known {
			return newValidationError(
				PhaseFileParse,
				key,
				source,
				"known top-level configuration key",
				nil,
			)
		}
	}
	return nil
}

func (state *loadState) walkYAMLMapping(node *yaml.Node, prefix, source string) error {
	node = resolvedNode(node)
	if node.Kind != yaml.MappingNode {
		key := prefix
		if key == "" {
			key = "$"
		}
		return newValidationError(PhaseSchemaValidation, key, source, "mapping", nil)
	}

	seen := make(map[string]struct{}, len(node.Content)/2)
	for index := 0; index+1 < len(node.Content); index += 2 {
		keyNode := resolvedNode(node.Content[index])
		valueNode := resolvedNode(node.Content[index+1])
		if keyNode.Kind != yaml.ScalarNode || keyNode.ShortTag() != "!!str" {
			key := prefix
			if key == "" {
				key = "$"
			}
			return newValidationError(PhaseSchemaValidation, key, source, "string configuration key", nil)
		}

		name := keyNode.Value
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if _, duplicate := seen[name]; duplicate {
			return newValidationError(PhaseSchemaValidation, path, source, "unique configuration key", nil)
		}
		seen[name] = struct{}{}

		switch {
		case schemaContains(path):
			if err := state.applyYAMLField(path, valueNode, source); err != nil {
				return err
			}
		case schemaHasDescendant(path):
			if valueNode.Kind != yaml.MappingNode {
				return newValidationError(PhaseSchemaValidation, path, source, "mapping", nil)
			}
			if err := state.walkYAMLMapping(valueNode, path, source); err != nil {
				return err
			}
		default:
			return newValidationError(PhaseSchemaValidation, path, source, "known configuration key", nil)
		}
	}
	return nil
}

func (state *loadState) applyYAMLField(path string, node *yaml.Node, source string) error {
	node = resolvedNode(node)
	var expectation string
	var cause error

	switch path {
	case "listener.client.port", "limits.max_message_bytes", "limits.outbound_queue_bytes":
		var value int
		if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!int" {
			expectation = "integer"
			break
		}
		if cause = node.Decode(&value); cause != nil {
			expectation = "integer"
			break
		}
		expectation = state.setInteger(path, value)
	case "listener.client.transport", "auth.mode":
		if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!str" {
			expectation = "string"
			break
		}
		expectation = state.setString(path, node.Value)
	case "listener.client.plaintext_acknowledged", "auth.development_acknowledged", "tenancy.enabled":
		var value bool
		if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!bool" {
			expectation = "boolean"
			break
		}
		if cause = node.Decode(&value); cause != nil {
			expectation = "boolean"
			break
		}
		state.setBoolean(path, value)
	case "connection.keepalive", "connection.idle_timeout", "connection.drain_window",
		"auth.expiry_warning_window", "auth.token_lifetime_floor":
		if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!str" {
			expectation = "duration"
			break
		}
		var value time.Duration
		value, cause = time.ParseDuration(node.Value)
		if cause != nil {
			expectation = "duration"
			break
		}
		expectation = state.setDuration(path, value)
	case "tenancy.schema_sets":
		if node.Kind != yaml.SequenceNode {
			expectation = "list of strings"
			break
		}
		values := make([]string, len(node.Content))
		for index, item := range node.Content {
			item = resolvedNode(item)
			if item.Kind != yaml.ScalarNode || item.ShortTag() != "!!str" {
				expectation = "list of strings"
				break
			}
			values[index] = item.Value
		}
		if expectation == "" {
			state.config.Tenancy.SchemaSets = values
		}
	default:
		expectation = "known configuration key"
	}

	if expectation != "" {
		return newValidationError(PhaseSchemaValidation, path, source, expectation, cause)
	}
	state.provenance[path] = source
	return nil
}

func (state *loadState) applyEnvironment(environment map[string]string) error {
	keys := sortedKeys(environment)
	for _, name := range keys {
		if name == ConfigPathEnvironment || !strings.HasPrefix(name, "CONDUIT_") {
			continue
		}
		path, ok := PathForEnvironment(name)
		if !ok {
			return newValidationError(
				PhaseSchemaValidation,
				name,
				"environment "+name,
				"environment key generated from the configuration schema",
				nil,
			)
		}
		if err := state.applyTextField(path, environment[name], "environment "+name); err != nil {
			return err
		}
	}
	return nil
}

func (state *loadState) applyFlags(flags map[string]string) error {
	keys := sortedKeys(flags)
	for _, path := range keys {
		source := "flag " + path
		if !schemaContains(path) {
			return newValidationError(
				PhaseSchemaValidation,
				path,
				source,
				"configuration path from the schema",
				nil,
			)
		}
		if err := state.applyTextField(path, flags[path], source); err != nil {
			return err
		}
	}
	return nil
}

func (state *loadState) applyTextField(path, raw, source string) error {
	value := strings.TrimSpace(raw)
	var expectation string
	var cause error

	switch path {
	case "listener.client.port", "limits.max_message_bytes", "limits.outbound_queue_bytes":
		var integer int
		integer, cause = strconv.Atoi(value)
		if cause != nil {
			expectation = "integer"
			break
		}
		expectation = state.setInteger(path, integer)
	case "listener.client.transport", "auth.mode":
		expectation = state.setString(path, value)
	case "listener.client.plaintext_acknowledged", "auth.development_acknowledged", "tenancy.enabled":
		var boolean bool
		switch strings.ToLower(value) {
		case "true":
			boolean = true
		case "false":
			boolean = false
		default:
			expectation = "boolean"
		}
		if expectation == "" {
			state.setBoolean(path, boolean)
		}
	case "connection.keepalive", "connection.idle_timeout", "connection.drain_window",
		"auth.expiry_warning_window", "auth.token_lifetime_floor":
		var duration time.Duration
		duration, cause = time.ParseDuration(value)
		if cause != nil {
			expectation = "duration"
			break
		}
		expectation = state.setDuration(path, duration)
	case "tenancy.schema_sets":
		var values []string
		if value != "" {
			for _, item := range strings.Split(value, ",") {
				item = strings.TrimSpace(item)
				if item == "" {
					expectation = "comma-separated list of non-empty strings"
					break
				}
				values = append(values, item)
			}
		}
		if expectation == "" {
			state.config.Tenancy.SchemaSets = values
		}
	default:
		expectation = "known configuration key"
	}

	if expectation != "" {
		return newValidationError(PhaseSchemaValidation, path, source, expectation, cause)
	}
	state.provenance[path] = source
	return nil
}

func (state *loadState) setInteger(path string, value int) string {
	switch path {
	case "listener.client.port":
		if value < 1 || value > 65535 {
			return "integer between 1 and 65535"
		}
		state.config.Listener.Client.Port = value
	case "limits.max_message_bytes":
		if value <= 0 {
			return "positive integer"
		}
		state.config.Limits.MaxMessageBytes = value
	case "limits.outbound_queue_bytes":
		if value <= 0 {
			return "positive integer"
		}
		state.config.Limits.OutboundQueueBytes = value
	default:
		return "known integer configuration key"
	}
	return ""
}

func (state *loadState) setString(path, value string) string {
	switch path {
	case "listener.client.transport":
		switch value {
		case "tls", "trusted_proxy", "plaintext":
			state.config.Listener.Client.Transport = value
		default:
			return "one of tls, trusted_proxy, or plaintext"
		}
	case "auth.mode":
		switch value {
		case "oidc", "apikey", "custom", "none":
			state.config.Auth.Mode = value
		default:
			return "one of oidc, apikey, custom, or none"
		}
	default:
		return "known string configuration key"
	}
	return ""
}

func (state *loadState) setBoolean(path string, value bool) {
	switch path {
	case "listener.client.plaintext_acknowledged":
		state.config.Listener.Client.PlaintextAcknowledged = value
	case "auth.development_acknowledged":
		state.config.Auth.DevelopmentAcknowledged = value
	case "tenancy.enabled":
		state.config.Tenancy.Enabled = value
	}
}

func (state *loadState) setDuration(path string, value time.Duration) string {
	switch path {
	case "connection.keepalive":
		if value <= 0 {
			return "duration greater than zero"
		}
		state.config.Connection.Keepalive = value
	case "connection.idle_timeout":
		if value <= 0 {
			return "duration greater than zero"
		}
		state.config.Connection.IdleTimeout = value
	case "connection.drain_window":
		state.config.Connection.DrainWindow = value
	case "auth.expiry_warning_window":
		if value < 0 {
			return "duration greater than or equal to zero"
		}
		state.config.Auth.ExpiryWarningWindow = value
	case "auth.token_lifetime_floor":
		if value <= 0 {
			return "duration greater than zero"
		}
		state.config.Auth.TokenLifetimeFloor = value
	default:
		return "known duration configuration key"
	}
	return ""
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func resolvedNode(node *yaml.Node) *yaml.Node {
	for depth := 0; node != nil && node.Kind == yaml.AliasNode && node.Alias != nil && depth < 32; depth++ {
		node = node.Alias
	}
	return node
}

func containsAlias(root *yaml.Node) bool {
	pending := []*yaml.Node{root}
	for len(pending) != 0 {
		last := len(pending) - 1
		node := pending[last]
		pending = pending[:last]
		if node == nil {
			continue
		}
		if node.Kind == yaml.AliasNode {
			return true
		}
		pending = append(pending, node.Content...)
	}
	return false
}
