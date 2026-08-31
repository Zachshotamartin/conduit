package ast

import (
	stderrors "errors"

	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
	gqlast "github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/lexer"
	"github.com/vektah/gqlparser/v2/parser"
	"github.com/vektah/gqlparser/v2/validator"
)

const (
	defaultMaxBytes  = 1 << 20
	defaultMaxTokens = 20_000
	defaultMaxDepth  = 15
)

var (
	errByteLimit  = stderrors.New("GraphQL document byte limit exceeded")
	errTokenLimit = stderrors.New("GraphQL document token limit exceeded")
	errDepthLimit = stderrors.New("GraphQL document parse-depth limit exceeded")
	errEmpty      = stderrors.New("GraphQL document contains no tokens")
)

// IntakeLimits bounds work performed before an operation document is
// admitted to the parser. A zero field selects that field's default.
type IntakeLimits struct {
	MaxBytes  int
	MaxTokens int
	MaxDepth  int
}

// Operation is Conduit's opaque representation of a parsed GraphQL operation
// document. The underlying parser representation remains confined here.
type Operation struct {
	document *gqlast.QueryDocument
	anchor   SchemaAnchor
}

type schemaIdentity struct{ _ byte }

// SchemaAnchor is an opaque, comparable identity for one immutable compiled
// schema. Its unexported identity cannot be forged by callers; the zero value
// means that no schema participated in admission.
type SchemaAnchor struct {
	identity *schemaIdentity
}

// OperationCount returns the number of operation definitions admitted from
// the document without exposing the parser-owned AST.
func (operation *Operation) OperationCount() int {
	if operation == nil || operation.document == nil {
		return 0
	}
	return len(operation.document.Operations)
}

// FragmentCount returns the number of named fragment definitions admitted
// from the document without exposing the parser-owned AST.
func (operation *Operation) FragmentCount() int {
	if operation == nil || operation.document == nil {
		return 0
	}
	return len(operation.document.Fragments)
}

// Schema is Conduit's opaque representation of a validated GraphQL schema.
// The parser-owned representation never crosses this package boundary.
type Schema struct {
	document *gqlast.Schema
	snapshot SchemaSnapshot
	anchor   SchemaAnchor
}

// Anchor returns the immutable schema identity used by operation admission
// and later binding tables.
func (schema *Schema) Anchor() SchemaAnchor {
	if schema == nil {
		return SchemaAnchor{}
	}
	return schema.anchor
}

// Anchor returns the schema identity under which this operation was admitted.
func (operation *Operation) Anchor() SchemaAnchor {
	if operation == nil {
		return SchemaAnchor{}
	}
	return operation.anchor
}

// Intake parses doc after enforcing byte, lexical-token, and syntactic-depth
// limits. It returns no partial Operation on rejection. Schema validation is
// added by R1.02; schema is retained in this gate's stable intake signature.
func Intake(doc []byte, limits IntakeLimits, schema *Schema) (*Operation, error) {
	limits = limits.withDefaults()
	if len(doc) > limits.MaxBytes {
		return nil, invalidRequest(errByteLimit)
	}

	input := string(doc)
	if err := preflight(input, limits); err != nil {
		return nil, err
	}

	document, err := parser.ParseQuery(&gqlast.Source{
		Name:  "operation.graphql",
		Input: input,
	})
	if err != nil {
		return nil, invalidRequest(err)
	}
	anchor := SchemaAnchor{}
	if schema != nil {
		if validationErrors := validator.ValidateWithRules(schema.document, document, nil); len(validationErrors) > 0 {
			return nil, invalidRequest(validationErrors)
		}
		anchor = schema.anchor
	}
	return &Operation{document: document, anchor: anchor}, nil
}

func (limits IntakeLimits) withDefaults() IntakeLimits {
	if limits.MaxBytes == 0 {
		limits.MaxBytes = defaultMaxBytes
	}
	if limits.MaxTokens == 0 {
		limits.MaxTokens = defaultMaxTokens
	}
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaultMaxDepth
	}
	return limits
}

func preflight(input string, limits IntakeLimits) error {
	source := gqlast.Source{Name: "operation.graphql", Input: input}
	scanner := lexer.New(&source)
	tokenCount := 0
	depth := 0

	for {
		token, err := scanner.ReadToken()
		if err != nil {
			return invalidRequest(err)
		}
		if token.Kind == lexer.EOF {
			if tokenCount == 0 {
				return invalidRequest(errEmpty)
			}
			return nil
		}
		if token.Kind == lexer.Comment {
			continue
		}

		tokenCount++
		if tokenCount > limits.MaxTokens {
			return invalidRequest(errTokenLimit)
		}

		switch token.Kind {
		case lexer.BraceL, lexer.BracketL, lexer.ParenL:
			depth++
			if depth > limits.MaxDepth {
				return invalidRequest(errDepthLimit)
			}
		case lexer.BraceR, lexer.BracketR, lexer.ParenR:
			if depth > 0 {
				depth--
			}
		}
	}
}

func invalidRequest(cause error) error {
	return conduiterrors.Wrap(conduiterrors.InvalidRequest, cause)
}
