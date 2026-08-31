package config

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// SourceLocation is a parser-neutral location in an operator document.
type SourceLocation struct {
	File   string
	Line   int
	Column int
}

// BindingDocument is the strictly decoded structural binding configuration.
// GraphQL coordinates and schema cross-references are deliberately compiled
// by internal/graphql/binding after this configuration-owned YAML boundary.
type BindingDocument struct {
	Name         string
	RootLocation SourceLocation
	Entries      []BindingEntry
}

// BindingEntry retains structural target presence and exact source locations
// so the GraphQL compiler can produce cross-document diagnostics.
type BindingEntry struct {
	Field          string
	Source         string
	Parent         []string
	HasSource      bool
	HasParent      bool
	EntryLocation  SourceLocation
	FieldLocation  SourceLocation
	TargetLocation SourceLocation
}

// BindingDocumentError is one deterministic structural binding failure.
type BindingDocumentError struct {
	Rule     string
	Location SourceLocation
	Message  string
}

// Error renders the stable structural failure without leaking parser details.
func (failure *BindingDocumentError) Error() string {
	if failure == nil {
		return "invalid binding document"
	}
	return failure.Rule + ": " + failure.Message
}

// DecodeBindingDocument owns YAML parsing for the resolver-binding subtree.
// Callers must enforce their byte and UTF-8 preflight bounds first.
func DecodeBindingDocument(name string, input []byte) (BindingDocument, error) {
	root, failure := decodeBindingYAML(name, input)
	if failure != nil {
		return BindingDocument{}, failure
	}
	if alias := firstBindingAlias(root); alias != nil {
		return BindingDocument{}, bindingFailure(
			"binding.document.alias", name, alias,
			"YAML aliases are not allowed in binding documents",
		)
	}
	if root == nil || root.Kind != yaml.MappingNode {
		return BindingDocument{}, bindingFailure(
			"binding.document.root_mapping", name, root,
			"binding document root must be a mapping",
		)
	}

	result := BindingDocument{Name: name, RootLocation: bindingNodeLocation(name, root)}
	var bindingsNode *yaml.Node
	seenRoot := make(map[string]struct{}, len(root.Content)/2)
	for index := 0; index+1 < len(root.Content); index += 2 {
		keyNode := root.Content[index]
		valueNode := root.Content[index+1]
		if !bindingStringNode(keyNode) {
			return BindingDocument{}, bindingFailure(
				"binding.document.root_key", name, keyNode,
				"binding document keys must be strings",
			)
		}
		if _, duplicate := seenRoot[keyNode.Value]; duplicate {
			return BindingDocument{}, bindingFailure(
				"binding.document.root_duplicate", name, keyNode,
				"binding document keys must be unique",
			)
		}
		seenRoot[keyNode.Value] = struct{}{}
		if keyNode.Value != "bindings" {
			return BindingDocument{}, bindingFailure(
				"binding.document.root_key", name, keyNode,
				"only the bindings root key is allowed",
			)
		}
		bindingsNode = valueNode
	}
	if bindingsNode == nil {
		return BindingDocument{}, bindingFailure(
			"binding.document.bindings_required", name, root,
			"binding document requires the bindings key",
		)
	}
	if bindingsNode.Kind != yaml.SequenceNode {
		return BindingDocument{}, bindingFailure(
			"binding.document.bindings_sequence", name, bindingsNode,
			"bindings must be a sequence",
		)
	}

	for _, entryNode := range bindingsNode.Content {
		entry, entryFailure := decodeBindingEntry(name, entryNode)
		if entryFailure != nil {
			return BindingDocument{}, entryFailure
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

func decodeBindingYAML(name string, input []byte) (*yaml.Node, *BindingDocumentError) {
	decoder := yaml.NewDecoder(bytes.NewReader(input))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, bindingFailure(
				"binding.document.root_mapping", name, nil,
				"binding document root must be a mapping",
			)
		}
		return nil, bindingFailure(
			"binding.document.yaml", name, nil,
			"binding document must be valid YAML",
		)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return nil, bindingFailure(
			"binding.document.multiple_documents", name, nil,
			"exactly one YAML document is allowed",
		)
	} else if !errors.Is(err, io.EOF) {
		return nil, bindingFailure(
			"binding.document.yaml", name, nil,
			"binding document must be valid YAML",
		)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) == 0 {
		return nil, nil
	}
	return document.Content[0], nil
}

func decodeBindingEntry(name string, node *yaml.Node) (BindingEntry, *BindingDocumentError) {
	if node == nil || node.Kind != yaml.MappingNode {
		return BindingEntry{}, bindingFailure(
			"binding.entry.mapping", name, node,
			"each binding must be a mapping",
		)
	}
	values := make(map[string]*yaml.Node, len(node.Content)/2)
	keys := make(map[string]*yaml.Node, len(node.Content)/2)
	for index := 0; index+1 < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		valueNode := node.Content[index+1]
		if !bindingStringNode(keyNode) {
			return BindingEntry{}, bindingFailure(
				"binding.entry.key", name, keyNode,
				"binding keys must be strings",
			)
		}
		if _, duplicate := values[keyNode.Value]; duplicate {
			return BindingEntry{}, bindingFailure(
				"binding.entry.duplicate_key", name, keyNode,
				"binding keys must be unique",
			)
		}
		switch keyNode.Value {
		case "field", "source", "parent":
		default:
			return BindingEntry{}, bindingFailure(
				"binding.entry.key", name, keyNode,
				"binding keys must be field, source, or parent",
			)
		}
		values[keyNode.Value] = valueNode
		keys[keyNode.Value] = keyNode
	}

	fieldNode, hasField := values["field"]
	if !hasField {
		return BindingEntry{}, bindingFailure(
			"binding.entry.field_required", name, node,
			"binding requires a field coordinate",
		)
	}
	if !bindingStringNode(fieldNode) {
		return BindingEntry{}, bindingFailure(
			"binding.entry.field_string", name, fieldNode,
			"binding field must be a string",
		)
	}

	sourceNode, hasSource := values["source"]
	parentNode, hasParent := values["parent"]
	if !hasSource && !hasParent {
		return BindingEntry{}, bindingFailure(
			"binding.entry.target_required", name, node,
			"binding requires exactly one of source or parent",
		)
	}
	if hasSource && hasParent {
		return BindingEntry{}, bindingFailure(
			"binding.entry.target_exclusive", name, keys["parent"],
			"binding source and parent targets are mutually exclusive",
		)
	}

	entry := BindingEntry{
		Field:         fieldNode.Value,
		HasSource:     hasSource,
		HasParent:     hasParent,
		EntryLocation: bindingNodeLocation(name, node),
		FieldLocation: bindingNodeLocation(name, fieldNode),
	}
	if hasSource {
		if !bindingStringNode(sourceNode) {
			return BindingEntry{}, bindingFailure(
				"binding.entry.source_string", name, sourceNode,
				"binding source must be a string",
			)
		}
		if strings.TrimSpace(sourceNode.Value) == "" {
			return BindingEntry{}, bindingFailure(
				"binding.entry.source_nonempty", name, sourceNode,
				"binding source must be nonempty",
			)
		}
		entry.Source = sourceNode.Value
		entry.TargetLocation = bindingNodeLocation(name, sourceNode)
		return entry, nil
	}

	if parentNode.Kind != yaml.SequenceNode {
		return BindingEntry{}, bindingFailure(
			"binding.entry.parent_sequence", name, parentNode,
			"binding parent must be a sequence of literal object keys",
		)
	}
	entry.Parent = make([]string, len(parentNode.Content))
	for index, segment := range parentNode.Content {
		if !bindingStringNode(segment) {
			return BindingEntry{}, bindingFailure(
				"binding.entry.parent_segment_string", name, segment,
				"parent path segments must be strings",
			)
		}
		if segment.Value == "" {
			return BindingEntry{}, bindingFailure(
				"binding.entry.parent_segment_nonempty", name, segment,
				"parent path segments must be nonempty literal object keys",
			)
		}
		entry.Parent[index] = segment.Value
	}
	entry.TargetLocation = bindingNodeLocation(name, parentNode)
	return entry, nil
}

func bindingStringNode(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str"
}

func bindingNodeLocation(name string, node *yaml.Node) SourceLocation {
	location := SourceLocation{File: name, Line: 1, Column: 1}
	if node != nil {
		if node.Line > 0 {
			location.Line = node.Line
		}
		if node.Column > 0 {
			location.Column = node.Column
		}
	}
	return location
}

func bindingFailure(rule, name string, node *yaml.Node, message string) *BindingDocumentError {
	return &BindingDocumentError{
		Rule: rule, Location: bindingNodeLocation(name, node), Message: message,
	}
}

func firstBindingAlias(root *yaml.Node) *yaml.Node {
	pending := []*yaml.Node{root}
	for len(pending) > 0 {
		last := len(pending) - 1
		node := pending[last]
		pending = pending[:last]
		if node == nil {
			continue
		}
		if node.Kind == yaml.AliasNode {
			return node
		}
		for index := len(node.Content) - 1; index >= 0; index-- {
			pending = append(pending, node.Content[index])
		}
	}
	return nil
}
