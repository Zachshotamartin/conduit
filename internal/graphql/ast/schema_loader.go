package ast

import (
	stderrors "errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	gqlast "github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/vektah/gqlparser/v2/lexer"
	"github.com/vektah/gqlparser/v2/parser"
	"github.com/vektah/gqlparser/v2/validator"
)

const (
	defaultSchemaMaxBytes  = 4 << 20
	defaultSchemaMaxTokens = 80_000
	defaultSchemaMaxDepth  = 60
)

// LoadSchema validates and compiles an immutable schema from logical SDL
// sources. Every returned failure has a nil Schema; syntax failures are
// collected across all files and semantic failures are accumulated by rule.
func LoadSchema(sources []SchemaSource, limits SchemaLimits) (*Schema, error) {
	limits = limits.withDefaults()
	normalized, diagnostics := normalizeSchemaSources(sources, limits.MaxBytes)
	if len(diagnostics) > 0 {
		return nil, NewDiagnostics(diagnostics...)
	}

	document := &gqlast.SchemaDocument{}
	prelude, err := october2021Prelude()
	if err != nil {
		return nil, NewDiagnostics(Diagnostic{
			Rule: "graphql.sdl.compiler_guard", Message: "failed to load the frozen GraphQL prelude",
		})
	}
	document.Merge(prelude)

	tokenCount := 0
	for _, source := range normalized {
		vendorSource := &gqlast.Source{
			Name: source.Name, Input: string(source.Input), BuiltIn: source.BuiltIn,
		}
		if !utf8.Valid(source.Input) {
			diagnostics = append(diagnostics, diagnosticAt(
				"graphql.sdl.syntax", vendorSource.Name, 1, 1, "SDL must be valid UTF-8",
			))
			continue
		}
		already := tokenCount
		if source.BuiltIn {
			already = 0
		}
		count, depthDiagnostic := inspectSchemaTokens(vendorSource, limits, already)
		if !source.BuiltIn {
			tokenCount += count
		}
		if depthDiagnostic != nil {
			diagnostics = append(diagnostics, *depthDiagnostic)
			continue
		}
		if !source.BuiltIn && tokenCount > limits.MaxTokens {
			diagnostics = append(diagnostics, diagnosticAt(
				"sdl.limit.tokens", source.Name, 1, 1,
				fmt.Sprintf("aggregate SDL token limit %d exceeded", limits.MaxTokens),
			))
			continue
		}

		parsed, parseErr := parser.ParseSchemaWithLimit(vendorSource, limits.MaxTokens)
		if parseErr != nil {
			diagnostics = append(diagnostics, diagnosticFromParser(
				"graphql.sdl.syntax", source.Name, parseErr,
			))
			continue
		}
		document.Merge(parsed)
	}
	if len(diagnostics) > 0 {
		return nil, NewDiagnostics(diagnostics...)
	}

	diagnostics = lintSchemaDocument(document)
	if len(diagnostics) > 0 {
		return nil, NewDiagnostics(diagnostics...)
	}

	restoreConduitDefinitions := relaxConduitDefinitions(document)
	compiled, compileErr := validator.ValidateSchemaDocument(document)
	restoreConduitDefinitions()
	if compileErr != nil {
		diagnostic := diagnosticFromParser("graphql.sdl.compiler_guard", "", compileErr)
		if diagnostic.File != "" && diagnostic.File != validator.Prelude.Name && diagnostic.File != conduitPreludeLogicalName {
			diagnostic.Rule = "graphql.sdl.validation"
		}
		return nil, NewDiagnostics(diagnostic)
	}
	removePostOctober2021Surface(compiled)
	snapshot := snapshotSchema(compiled)
	return &Schema{
		document: compiled,
		snapshot: snapshot,
		anchor:   SchemaAnchor{identity: &schemaIdentity{}},
	}, nil
}

func (limits SchemaLimits) withDefaults() SchemaLimits {
	if limits.MaxBytes == 0 {
		limits.MaxBytes = defaultSchemaMaxBytes
	}
	if limits.MaxTokens == 0 {
		limits.MaxTokens = defaultSchemaMaxTokens
	}
	if limits.MaxNestingDepth == 0 {
		limits.MaxNestingDepth = defaultSchemaMaxDepth
	}
	return limits
}

func normalizeSchemaSources(sources []SchemaSource, maxBytes int) ([]SchemaSource, []Diagnostic) {
	result := make([]SchemaSource, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	total := 0
	var diagnostics []Diagnostic
	for _, source := range sources {
		name, ok := NormalizeSchemaSourceName(source.Name)
		if !ok {
			diagnostics = append(diagnostics, Diagnostic{
				Rule: "sdl.file.logical_name", Message: "SDL logical name must be a unique relative path",
			})
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			diagnostics = append(diagnostics, Diagnostic{
				Rule: "sdl.file.logical_name", File: name,
				Message: "SDL logical name is duplicated after normalization",
			})
			continue
		}
		seen[name] = struct{}{}
		if !source.BuiltIn {
			total += len(source.Input)
			if total > maxBytes {
				diagnostics = append(diagnostics, Diagnostic{
					Rule: "sdl.limit.bytes", File: name, Line: 1, Column: 1,
					Message: fmt.Sprintf("aggregate SDL byte limit %d exceeded", maxBytes),
				})
				continue
			}
		}
		result = append(result, SchemaSource{
			Name: name, Input: append([]byte(nil), source.Input...), BuiltIn: source.BuiltIn,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, diagnostics
}

// NormalizeSchemaSourceName canonicalizes the only source-name form allowed
// to reach diagnostics. It rejects absolute paths, traversal, control bytes,
// and malformed UTF-8 without ever echoing the unsafe input.
func NormalizeSchemaSourceName(name string) (string, bool) {
	if !utf8.ValidString(name) {
		return "", false
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", false
		}
	}
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") ||
		(len(name) >= 2 && name[1] == ':') {
		return "", false
	}
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func inspectSchemaTokens(source *gqlast.Source, limits SchemaLimits, already int) (int, *Diagnostic) {
	scanner := lexer.New(source)
	count, depth := 0, 0
	for {
		token, err := scanner.ReadToken()
		if err != nil {
			diagnostic := diagnosticFromParser("graphql.sdl.syntax", source.Name, err)
			return count, &diagnostic
		}
		if token.Kind == lexer.EOF {
			return count, nil
		}
		if token.Kind == lexer.Comment {
			continue
		}
		count++
		if already+count > limits.MaxTokens {
			return count, nil
		}
		switch token.Kind {
		case lexer.BraceL, lexer.BracketL, lexer.ParenL:
			depth++
			if depth > limits.MaxNestingDepth {
				diagnostic := diagnosticAt(
					"sdl.limit.depth", source.Name, token.Pos.Line, token.Pos.Column,
					fmt.Sprintf("SDL nesting depth limit %d exceeded", limits.MaxNestingDepth),
				)
				return count, &diagnostic
			}
		case lexer.BraceR, lexer.BracketR, lexer.ParenR:
			if depth > 0 {
				depth--
			}
		}
	}
}

func october2021Prelude() (*gqlast.SchemaDocument, error) {
	parsed, err := parser.ParseSchema(validator.Prelude)
	if err != nil {
		return nil, err
	}
	filtered := parsed.Directives[:0]
	for _, directive := range parsed.Directives {
		if directive.Name != "defer" && directive.Name != "oneOf" {
			filtered = append(filtered, directive)
		}
	}
	parsed.Directives = filtered
	if deprecated := parsed.Directives.ForName("deprecated"); deprecated != nil {
		deprecated.Locations = []gqlast.DirectiveLocation{
			gqlast.LocationFieldDefinition,
			gqlast.LocationEnumValue,
		}
	}
	if typeDefinition := parsed.Definitions.ForName("__Type"); typeDefinition != nil {
		fields := typeDefinition.Fields[:0]
		for _, field := range typeDefinition.Fields {
			if field.Name != "isOneOf" {
				fields = append(fields, field)
			}
		}
		typeDefinition.Fields = fields
	}
	removeFieldArgument(parsed, "__Type", "inputFields", "includeDeprecated")
	removeFieldArgument(parsed, "__Field", "args", "includeDeprecated")
	removeFieldArgument(parsed, "__Directive", "args", "includeDeprecated")
	removeFields(parsed, "__InputValue", "isDeprecated", "deprecationReason")
	return parsed, nil
}

func removePostOctober2021Surface(schema *gqlast.Schema) {
	if definition := schema.Types["__Type"]; definition != nil {
		fields := definition.Fields[:0]
		for _, field := range definition.Fields {
			if field.Name != "isOneOf" {
				fields = append(fields, field)
			}
		}
		definition.Fields = fields
	}
	removeCompiledFieldArgument(schema, "__Type", "inputFields", "includeDeprecated")
	removeCompiledFieldArgument(schema, "__Field", "args", "includeDeprecated")
	removeCompiledFieldArgument(schema, "__Directive", "args", "includeDeprecated")
	if definition := schema.Types["__InputValue"]; definition != nil {
		fields := definition.Fields[:0]
		for _, field := range definition.Fields {
			if field.Name != "isDeprecated" && field.Name != "deprecationReason" {
				fields = append(fields, field)
			}
		}
		definition.Fields = fields
	}
}

func removeFieldArgument(document *gqlast.SchemaDocument, typeName, fieldName, argumentName string) {
	definition := document.Definitions.ForName(typeName)
	if definition == nil {
		return
	}
	field := definition.Fields.ForName(fieldName)
	if field == nil {
		return
	}
	arguments := field.Arguments[:0]
	for _, argument := range field.Arguments {
		if argument.Name != argumentName {
			arguments = append(arguments, argument)
		}
	}
	field.Arguments = arguments
}

func removeFields(document *gqlast.SchemaDocument, typeName string, names ...string) {
	definition := document.Definitions.ForName(typeName)
	if definition == nil {
		return
	}
	fields := definition.Fields[:0]
	for _, field := range definition.Fields {
		remove := false
		for _, name := range names {
			if field.Name == name {
				remove = true
				break
			}
		}
		if !remove {
			fields = append(fields, field)
		}
	}
	definition.Fields = fields
}

func removeCompiledFieldArgument(schema *gqlast.Schema, typeName, fieldName, argumentName string) {
	definition := schema.Types[typeName]
	if definition == nil {
		return
	}
	field := definition.Fields.ForName(fieldName)
	if field == nil {
		return
	}
	arguments := field.Arguments[:0]
	for _, argument := range field.Arguments {
		if argument.Name != argumentName {
			arguments = append(arguments, argument)
		}
	}
	field.Arguments = arguments
}

const conduitPreludeLogicalName = "__conduit/directives.graphql"

type conduitDirectiveRestore struct {
	definition *gqlast.DirectiveDefinition
	locations  []gqlast.DirectiveLocation
	repeatable bool
	nonNull    []bool
}

func relaxConduitDefinitions(document *gqlast.SchemaDocument) func() {
	var restores []conduitDirectiveRestore
	allLocations := []gqlast.DirectiveLocation{
		gqlast.LocationSchema, gqlast.LocationScalar, gqlast.LocationObject,
		gqlast.LocationFieldDefinition, gqlast.LocationArgumentDefinition,
		gqlast.LocationInterface, gqlast.LocationUnion, gqlast.LocationEnum,
		gqlast.LocationEnumValue, gqlast.LocationInputObject,
		gqlast.LocationInputFieldDefinition,
	}
	for _, definition := range document.Directives {
		if !isConduitDirective(definition.Name) {
			continue
		}
		restore := conduitDirectiveRestore{
			definition: definition,
			locations:  append([]gqlast.DirectiveLocation(nil), definition.Locations...),
			repeatable: definition.IsRepeatable,
			nonNull:    make([]bool, len(definition.Arguments)),
		}
		for index, argument := range definition.Arguments {
			restore.nonNull[index] = argument.Type.NonNull
			argument.Type.NonNull = false
		}
		definition.Locations = append([]gqlast.DirectiveLocation(nil), allLocations...)
		definition.IsRepeatable = true
		restores = append(restores, restore)
	}
	return func() {
		for _, restore := range restores {
			restore.definition.Locations = restore.locations
			restore.definition.IsRepeatable = restore.repeatable
			for index, argument := range restore.definition.Arguments {
				argument.Type.NonNull = restore.nonNull[index]
			}
		}
	}
}

func diagnosticFromParser(rule, fallbackFile string, err error) Diagnostic {
	diagnostic := Diagnostic{Rule: rule, File: fallbackFile, Line: 1, Column: 1, Message: err.Error()}
	var gqlErr *gqlerror.Error
	if stderrors.As(err, &gqlErr) {
		diagnostic.Message = gqlErr.Message
		if len(gqlErr.Locations) > 0 {
			diagnostic.Line = gqlErr.Locations[0].Line
			diagnostic.Column = gqlErr.Locations[0].Column
		}
		if file, ok := gqlErr.Extensions["file"].(string); ok && file != "" {
			diagnostic.File = file
		}
	}
	if diagnostic.Line < 1 {
		diagnostic.Line = 1
	}
	if diagnostic.Column < 1 {
		diagnostic.Column = 1
	}
	return diagnostic
}

func diagnosticAt(rule, file string, line, column int, message string) Diagnostic {
	return Diagnostic{Rule: rule, File: file, Line: line, Column: column, Message: message}
}

func positionOf(position *gqlast.Position) SourcePosition {
	if position == nil {
		return SourcePosition{}
	}
	result := SourcePosition{Line: position.Line, Column: position.Column}
	if position.Src != nil {
		result.File = position.Src.Name
	}
	return result
}
