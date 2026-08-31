package binding

import (
	"fmt"
	"unicode/utf8"

	"github.com/Zachshotamartin/conduit/internal/config"
	"github.com/Zachshotamartin/conduit/internal/datasource"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
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

	decoded, err := config.DecodeBindingDocument(name, document.Input)
	if err != nil {
		failure, ok := err.(*config.BindingDocumentError)
		if !ok {
			return parsedDocument{}, one("binding.document.yaml", name, 1, 1,
				"binding document must be valid YAML")
		}
		return parsedDocument{}, one(
			failure.Rule,
			failure.Location.File,
			failure.Location.Line,
			failure.Location.Column,
			failure.Message,
		)
	}

	result := parsedDocument{
		name:         decoded.Name,
		rootPosition: graphQLPosition(decoded.RootLocation),
	}
	firstByField := make(map[datasource.FieldRef]graphqlast.SourcePosition, len(decoded.Entries))
	for _, raw := range decoded.Entries {
		field, fieldErr := datasource.ParseFieldRef(raw.Field)
		if fieldErr != nil {
			position := graphQLPosition(raw.FieldLocation)
			return parsedDocument{}, one(
				"binding.entry.field_coordinate", position.File, position.Line, position.Column,
				"binding field must be a canonical non-introspection Type.field coordinate",
			)
		}
		entry := parsedEntry{
			binding: Binding{
				Field: field,
			},
			fieldPosition:  graphQLPosition(raw.FieldLocation),
			targetPosition: graphQLPosition(raw.TargetLocation),
		}
		if raw.HasSource {
			entry.binding.Kind = Source
			entry.binding.SourceName = raw.Source
		} else {
			entry.binding.Kind = Parent
			entry.binding.ParentPath = cloneParentPath(raw.Parent)
		}
		if first, duplicate := firstByField[field]; duplicate {
			diagnostic := graphqlast.Diagnostic{
				Rule: "binding.entry.duplicate_field",
				File: entry.fieldPosition.File, Line: entry.fieldPosition.Line, Column: entry.fieldPosition.Column,
				Message: "each field coordinate may appear only once",
				Related: []graphqlast.RelatedLocation{{
					File: first.File, Line: first.Line, Column: first.Column, Message: "first binding declared here",
				}},
			}
			return parsedDocument{}, []graphqlast.Diagnostic{diagnostic}
		}
		firstByField[field] = entry.fieldPosition
		result.entries = append(result.entries, entry)
	}
	return result, nil
}

func graphQLPosition(location config.SourceLocation) graphqlast.SourcePosition {
	return graphqlast.SourcePosition{
		File: location.File, Line: location.Line, Column: location.Column,
	}
}

func one(rule, file string, line, column int, message string) []graphqlast.Diagnostic {
	return []graphqlast.Diagnostic{{
		Rule: rule, File: file, Line: line, Column: column, Message: message,
	}}
}
