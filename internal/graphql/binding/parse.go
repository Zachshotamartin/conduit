package binding

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/Zachshotamartin/conduit/internal/datasource"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
	"gopkg.in/yaml.v3"
)

type parsedDocument struct {
	name         string
	rootPosition graphqlast.SourcePosition
	entries      []parsedEntry
}

type parsedEntry struct {
	binding        Binding
	fieldPosition  graphqlast.SourcePosition
	targetPosition graphqlast.SourcePosition
}

func parse(document Document) (parsedDocument, []graphqlast.Diagnostic) {
	name, ok := graphqlast.NormalizeSchemaSourceName(document.Name)
	if !ok {
		return parsedDocument{}, one("binding.document.logical_name", document.Name, 1, 1,
			"binding logical name must be a normalized relative path")
	}
	if len(document.Input) > MaxDocumentBytes {
		return parsedDocument{}, one("binding.document.bytes", name, 1, 1,
			fmt.Sprintf("binding document exceeds %d-byte limit", MaxDocumentBytes))
	}
	if !utf8.Valid(document.Input) {
		return parsedDocument{}, one("binding.document.utf8", name, 1, 1,
			"binding document must be valid UTF-8")
	}

	root, rule, message := decodeOneDocument(document.Input)
	if rule != "" {
		return parsedDocument{}, one(rule, name, 1, 1, message)
	}
	if containsAlias(root) {
		alias := firstAlias(root)
		return parsedDocument{}, diagnosticAt("binding.document.alias", name, alias,
			"YAML aliases are not allowed in binding documents")
	}
	if root == nil || root.Kind != yaml.MappingNode {
		return parsedDocument{}, diagnosticAt("binding.document.root_mapping", name, root,
			"binding document root must be a mapping")
	}

	result := parsedDocument{name: name, rootPosition: nodePosition(name, root)}
	var bindingsNode *yaml.Node
	seenRoot := make(map[string]struct{}, len(root.Content)/2)
	for index := 0; index+1 < len(root.Content); index += 2 {
		keyNode := root.Content[index]
		valueNode := root.Content[index+1]
		if !stringNode(keyNode) {
			return parsedDocument{}, diagnosticAt("binding.document.root_key", name, keyNode,
				"binding document keys must be strings")
		}
		if _, duplicate := seenRoot[keyNode.Value]; duplicate {
			return parsedDocument{}, diagnosticAt("binding.document.root_duplicate", name, keyNode,
				"binding document keys must be unique")
		}
		seenRoot[keyNode.Value] = struct{}{}
		if keyNode.Value != "bindings" {
			return parsedDocument{}, diagnosticAt("binding.document.root_key", name, keyNode,
				"only the bindings root key is allowed")
		}
		bindingsNode = valueNode
	}
	if bindingsNode == nil {
		return parsedDocument{}, diagnosticAt("binding.document.bindings_required", name, root,
			"binding document requires the bindings key")
	}
	if bindingsNode.Kind != yaml.SequenceNode {
		return parsedDocument{}, diagnosticAt("binding.document.bindings_sequence", name, bindingsNode,
			"bindings must be a sequence")
	}

	firstByField := make(map[datasource.FieldRef]graphqlast.SourcePosition, len(bindingsNode.Content))
	for _, entryNode := range bindingsNode.Content {
		entry, diagnostics := parseEntry(name, entryNode)
		if len(diagnostics) > 0 {
			return parsedDocument{}, diagnostics
		}
		if first, duplicate := firstByField[entry.binding.Field]; duplicate {
			diagnostic := diagnosticAt("binding.entry.duplicate_field", name, entryNode,
				"each field coordinate may appear only once")[0]
			diagnostic.Related = []graphqlast.RelatedLocation{{
				File: first.File, Line: first.Line, Column: first.Column, Message: "first binding declared here",
			}}
			return parsedDocument{}, []graphqlast.Diagnostic{diagnostic}
		}
		firstByField[entry.binding.Field] = entry.fieldPosition
		result.entries = append(result.entries, entry)
	}
	return result, nil
}

func decodeOneDocument(input []byte) (*yaml.Node, string, string) {
	decoder := yaml.NewDecoder(bytes.NewReader(input))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "binding.document.root_mapping", "binding document root must be a mapping"
		}
		return nil, "binding.document.yaml", "binding document must be valid YAML"
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return nil, "binding.document.multiple_documents", "exactly one YAML document is allowed"
	} else if !errors.Is(err, io.EOF) {
		return nil, "binding.document.yaml", "binding document must be valid YAML"
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) == 0 {
		return nil, "", ""
	}
	return document.Content[0], "", ""
}

func parseEntry(name string, node *yaml.Node) (parsedEntry, []graphqlast.Diagnostic) {
	if node == nil || node.Kind != yaml.MappingNode {
		return parsedEntry{}, diagnosticAt("binding.entry.mapping", name, node,
			"each binding must be a mapping")
	}
	values := make(map[string]*yaml.Node, len(node.Content)/2)
	keys := make(map[string]*yaml.Node, len(node.Content)/2)
	for index := 0; index+1 < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		valueNode := node.Content[index+1]
		if !stringNode(keyNode) {
			return parsedEntry{}, diagnosticAt("binding.entry.key", name, keyNode,
				"binding keys must be strings")
		}
		if _, duplicate := values[keyNode.Value]; duplicate {
			return parsedEntry{}, diagnosticAt("binding.entry.duplicate_key", name, keyNode,
				"binding keys must be unique")
		}
		switch keyNode.Value {
		case "field", "source", "parent":
		default:
			return parsedEntry{}, diagnosticAt("binding.entry.key", name, keyNode,
				"binding keys must be field, source, or parent")
		}
		values[keyNode.Value] = valueNode
		keys[keyNode.Value] = keyNode
	}

	fieldNode, hasField := values["field"]
	if !hasField {
		return parsedEntry{}, diagnosticAt("binding.entry.field_required", name, node,
			"binding requires a field coordinate")
	}
	if !stringNode(fieldNode) {
		return parsedEntry{}, diagnosticAt("binding.entry.field_string", name, fieldNode,
			"binding field must be a string")
	}
	field, err := datasource.ParseFieldRef(fieldNode.Value)
	if err != nil {
		return parsedEntry{}, diagnosticAt("binding.entry.field_coordinate", name, fieldNode,
			"binding field must be a canonical non-introspection Type.field coordinate")
	}

	sourceNode, hasSource := values["source"]
	parentNode, hasParent := values["parent"]
	if !hasSource && !hasParent {
		return parsedEntry{}, diagnosticAt("binding.entry.target_required", name, node,
			"binding requires exactly one of source or parent")
	}
	if hasSource && hasParent {
		return parsedEntry{}, diagnosticAt("binding.entry.target_exclusive", name, keys["parent"],
			"binding source and parent targets are mutually exclusive")
	}

	entry := parsedEntry{binding: Binding{Field: field}, fieldPosition: nodePosition(name, fieldNode)}
	if hasSource {
		if !stringNode(sourceNode) {
			return parsedEntry{}, diagnosticAt("binding.entry.source_string", name, sourceNode,
				"binding source must be a string")
		}
		if strings.TrimSpace(sourceNode.Value) == "" {
			return parsedEntry{}, diagnosticAt("binding.entry.source_nonempty", name, sourceNode,
				"binding source must be nonempty")
		}
		entry.binding.Kind = Source
		entry.binding.SourceName = sourceNode.Value
		entry.targetPosition = nodePosition(name, sourceNode)
		return entry, nil
	}

	if parentNode.Kind != yaml.SequenceNode {
		return parsedEntry{}, diagnosticAt("binding.entry.parent_sequence", name, parentNode,
			"binding parent must be a sequence of literal object keys")
	}
	path := make([]string, len(parentNode.Content))
	for index, segment := range parentNode.Content {
		if !stringNode(segment) {
			return parsedEntry{}, diagnosticAt("binding.entry.parent_segment_string", name, segment,
				"parent path segments must be strings")
		}
		if segment.Value == "" {
			return parsedEntry{}, diagnosticAt("binding.entry.parent_segment_nonempty", name, segment,
				"parent path segments must be nonempty literal object keys")
		}
		path[index] = segment.Value
	}
	entry.binding.Kind = Parent
	entry.binding.ParentPath = path
	entry.targetPosition = nodePosition(name, parentNode)
	return entry, nil
}

func stringNode(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str"
}

func nodePosition(name string, node *yaml.Node) graphqlast.SourcePosition {
	position := graphqlast.SourcePosition{File: name, Line: 1, Column: 1}
	if node != nil {
		if node.Line > 0 {
			position.Line = node.Line
		}
		if node.Column > 0 {
			position.Column = node.Column
		}
	}
	return position
}

func diagnosticAt(rule, name string, node *yaml.Node, message string) []graphqlast.Diagnostic {
	position := nodePosition(name, node)
	return one(rule, position.File, position.Line, position.Column, message)
}

func one(rule, file string, line, column int, message string) []graphqlast.Diagnostic {
	return []graphqlast.Diagnostic{{
		Rule: rule, File: file, Line: line, Column: column, Message: message,
	}}
}

func containsAlias(root *yaml.Node) bool {
	return firstAlias(root) != nil
}

func firstAlias(root *yaml.Node) *yaml.Node {
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
