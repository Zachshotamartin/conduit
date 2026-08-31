package schema

import (
	"encoding/hex"

	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
)

// Options supplies the registries against which validated-but-inert schema
// annotations are checked. Runtime binding belongs to R1.03 and later gates.
type Options struct {
	SourceNames []string
	AuthRules   []string
}

// File pairs a physical read target with the normalized logical name allowed
// to appear in diagnostics.
type File struct {
	Path string
	Name string
}

// Scalar identifies the built-in scalar admitted by @filterable.
type Scalar string

const (
	ScalarString  Scalar = "String"
	ScalarID      Scalar = "ID"
	ScalarInt     Scalar = "Int"
	ScalarFloat   Scalar = "Float"
	ScalarBoolean Scalar = "Boolean"
)

// BackpressurePolicy is a validated policy name. @backpressure remains inert
// until the subscription runtime gate owns its behavior.
type BackpressurePolicy string

const (
	BackpressureDropOldest    BackpressurePolicy = "DROP_OLDEST"
	BackpressureCoalesceByKey BackpressurePolicy = "COALESCE_BY_KEY"
	BackpressureDisconnect    BackpressurePolicy = "DISCONNECT"
)

// ComplexityMetadata is validated schema metadata. R1.05 owns all request-time
// arithmetic, including defaults, nulls, negatives, and overflow behavior.
type ComplexityMetadata struct {
	Cost        int
	Multipliers []string
}

// BackpressureMetadata is validated schema metadata. R1.02 deliberately does
// not compile or interpret CoalesceKey as a response path; R6 owns that work.
type BackpressureMetadata struct {
	Present     bool
	Policy      BackpressurePolicy
	Queue       int
	CoalesceKey string
}

// FieldMetadata is the immutable-by-copy metadata for one field.
type FieldMetadata struct {
	HasSource    bool
	SourceName   string
	HasAuth      bool
	AuthRule     string
	Complexity   ComplexityMetadata
	Backpressure BackpressureMetadata
}

// ArgumentMetadata is the immutable metadata for one field argument.
type ArgumentMetadata struct {
	Filterable bool
	Scalar     Scalar
}

// Hash is the stable SHA-256 digest of a canonical semantic schema.
type Hash [32]byte

// String renders the algorithm-qualified digest used by logs and admin APIs.
func (hash Hash) String() string {
	return "sha256:" + hex.EncodeToString(hash[:])
}

type fieldKey struct {
	parent string
	field  string
}

type argumentKey struct {
	parent   string
	field    string
	argument string
}

// Schema is a fully validated, immutable serving schema and its Conduit-owned
// metadata indexes.
type Schema struct {
	executable *graphqlast.Schema
	hash       Hash
	fields     map[fieldKey]FieldMetadata
	arguments  map[argumentKey]ArgumentMetadata
}

// Executable returns the opaque compiled schema used by later GraphQL gates.
func (schema *Schema) Executable() *graphqlast.Schema {
	if schema == nil {
		return nil
	}
	return schema.executable
}

// Hash returns the stable semantic schema digest.
func (schema *Schema) Hash() Hash {
	if schema == nil {
		return Hash{}
	}
	return schema.hash
}

// Field returns a defensive copy of field metadata.
func (schema *Schema) Field(parent, field string) (FieldMetadata, bool) {
	if schema == nil {
		return FieldMetadata{}, false
	}
	metadata, ok := schema.fields[fieldKey{parent: parent, field: field}]
	metadata.Complexity.Multipliers = append([]string(nil), metadata.Complexity.Multipliers...)
	return metadata, ok
}

// Argument returns argument metadata.
func (schema *Schema) Argument(parent, field, argument string) (ArgumentMetadata, bool) {
	if schema == nil {
		return ArgumentMetadata{}, false
	}
	metadata, ok := schema.arguments[argumentKey{parent: parent, field: field, argument: argument}]
	return metadata, ok
}
