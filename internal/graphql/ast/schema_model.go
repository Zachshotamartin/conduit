package ast

import (
	"fmt"
	"sort"
	"strings"

	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
)

// SchemaSource is one logical SDL input. Name is an operator-facing relative
// path, never a physical host path. BuiltIn inputs participate in compilation
// but are omitted from the public snapshot.
type SchemaSource struct {
	Name    string
	Input   []byte
	BuiltIn bool
}

// SchemaLimits bounds aggregate work performed before SDL compilation. A zero
// value selects a conservative default.
type SchemaLimits struct {
	MaxBytes        int
	MaxTokens       int
	MaxNestingDepth int
}

// Diagnostic is one deterministic SDL configuration failure.
type Diagnostic struct {
	Rule    string
	File    string
	Line    int
	Column  int
	Message string
}

// Diagnostics is an ordered collection of configuration failures. It keeps
// operator detail in Error while unwrapping to Conduit's bounded category.
type Diagnostics struct {
	items []Diagnostic
}

// NewDiagnostics returns a sorted defensive diagnostic collection.
func NewDiagnostics(items ...Diagnostic) *Diagnostics {
	copied := append([]Diagnostic(nil), items...)
	sort.SliceStable(copied, func(i, j int) bool {
		left, right := copied[i], copied[j]
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Column != right.Column {
			return left.Column < right.Column
		}
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		return left.Message < right.Message
	})
	return &Diagnostics{items: copied}
}

// Items returns an independent snapshot.
func (diagnostics *Diagnostics) Items() []Diagnostic {
	if diagnostics == nil {
		return nil
	}
	return append([]Diagnostic(nil), diagnostics.items...)
}

func (diagnostics *Diagnostics) Error() string {
	if diagnostics == nil || len(diagnostics.items) == 0 {
		return "invalid GraphQL schema"
	}
	var result strings.Builder
	for index, item := range diagnostics.items {
		if index > 0 {
			result.WriteByte('\n')
		}
		if item.File != "" {
			result.WriteString(item.File)
			if item.Line > 0 {
				fmt.Fprintf(&result, ":%d:%d", item.Line, item.Column)
			}
			result.WriteString(": ")
		}
		fmt.Fprintf(&result, "[%s] %s", item.Rule, item.Message)
	}
	return result.String()
}

// Unwrap classifies schema diagnostics without exposing their high-cardinality
// operator detail through Conduit's public error boundary.
func (diagnostics *Diagnostics) Unwrap() error {
	return conduiterrors.New(conduiterrors.InvalidConfiguration)
}

// SourcePosition is a parser-neutral source location.
type SourcePosition struct {
	File   string
	Line   int
	Column int
}

// TypeRef is a parser-neutral GraphQL type reference.
type TypeRef struct {
	Named   string
	Element *TypeRef
	NonNull bool
}

// ValueKind is the parser-neutral kind of an SDL constant.
type ValueKind string

const (
	ValueInt     ValueKind = "INT"
	ValueFloat   ValueKind = "FLOAT"
	ValueString  ValueKind = "STRING"
	ValueBoolean ValueKind = "BOOLEAN"
	ValueNull    ValueKind = "NULL"
	ValueEnum    ValueKind = "ENUM"
	ValueList    ValueKind = "LIST"
	ValueObject  ValueKind = "OBJECT"
)

// Value is a parser-neutral SDL constant.
type Value struct {
	Kind     ValueKind
	Raw      string
	Children []ValueChild
	Position SourcePosition
}

// ValueChild is a list element (empty Name) or object field.
type ValueChild struct {
	Name  string
	Value Value
}

// DirectiveUse is one authored directive occurrence.
type DirectiveUse struct {
	Name      string
	Arguments []DirectiveArgument
	Position  SourcePosition
}

// DirectiveArgument is one authored directive argument.
type DirectiveArgument struct {
	Name     string
	Value    Value
	Position SourcePosition
}

// ArgumentDefinition is a field or directive argument definition.
type ArgumentDefinition struct {
	Name         string
	Description  string
	Type         TypeRef
	DefaultValue *Value
	Directives   []DirectiveUse
	Position     SourcePosition
}

// FieldDefinition is a parser-neutral schema field definition.
type FieldDefinition struct {
	Name         string
	Description  string
	Type         TypeRef
	DefaultValue *Value
	Arguments    []ArgumentDefinition
	Directives   []DirectiveUse
	Position     SourcePosition
}

// EnumValueDefinition is a parser-neutral enum value definition.
type EnumValueDefinition struct {
	Name        string
	Description string
	Directives  []DirectiveUse
	Position    SourcePosition
}

// TypeDefinition is a parser-neutral, extension-normalized type definition.
type TypeDefinition struct {
	Kind        string
	Name        string
	Description string
	Interfaces  []string
	UnionTypes  []string
	Fields      []FieldDefinition
	EnumValues  []EnumValueDefinition
	Directives  []DirectiveUse
	Position    SourcePosition
}

// DirectiveDefinition is a parser-neutral directive definition.
type DirectiveDefinition struct {
	Name        string
	Description string
	Arguments   []ArgumentDefinition
	Locations   []string
	Repeatable  bool
	Position    SourcePosition
}

// SchemaSnapshot is an immutable-by-copy, vendor-free view of the compiled
// user schema. Built-in types and directives are deliberately absent.
type SchemaSnapshot struct {
	Query            string
	Mutation         string
	Subscription     string
	Description      string
	Directives       []DirectiveDefinition
	SchemaDirectives []DirectiveUse
	Types            []TypeDefinition
}

// Snapshot returns a deep copy so callers cannot mutate the serving schema.
func (schema *Schema) Snapshot() SchemaSnapshot {
	if schema == nil {
		return SchemaSnapshot{}
	}
	return cloneSnapshot(schema.snapshot)
}
