package ast

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	gqlast "github.com/vektah/gqlparser/v2/ast"
)

var conduitDirectiveNames = map[string]struct{}{
	"source": {}, "auth": {}, "filterable": {}, "backpressure": {}, "complexity": {},
}

// lintSchemaDocument owns deterministic, accumulating validation for rules
// where gqlparser's fail-fast compiler is incomplete or newer than Conduit's
// frozen October 2021 contract. The compiler still runs last as a guard.
func lintSchemaDocument(document *gqlast.SchemaDocument) []Diagnostic {
	types, directives, diagnostics := collectSchemaSymbols(document)
	diagnostics = append(diagnostics, lintDirectiveDefinitions(document, types, directives)...)
	diagnostics = append(diagnostics, lintOperationTypes(document, types)...)

	for _, definition := range document.Definitions {
		diagnostics = append(diagnostics, lintDefinition(definition, types, directives)...)
	}
	for _, extension := range document.Extensions {
		base := types[extension.Name]
		if base == nil {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.known_extension_type", extension.Position,
				fmt.Sprintf("cannot extend undefined type %q", extension.Name),
			))
			continue
		}
		if base.Kind != extension.Kind {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.extension_kind", extension.Position,
				fmt.Sprintf("extension kind for %q does not match its definition", extension.Name),
			))
			continue
		}
		if base.Kind == gqlast.Scalar && isBuiltInPosition(base.Position) {
			for _, directive := range extension.Directives {
				if directive.Name == "specifiedBy" {
					diagnostics = append(diagnostics, diagnosticForPosition(
						"graphql.sdl.specified_by_builtin", directive.Position,
						fmt.Sprintf("built-in scalar %s may not be assigned a specification URL", base.Name),
					))
				}
			}
		}
		diagnostics = append(diagnostics, lintDefinition(extension, types, directives)...)
	}

	diagnostics = append(diagnostics, lintCrossDefinitionMembers(document, types)...)
	diagnostics = append(diagnostics, lintCrossDefinitionDirectives(document, directives)...)
	diagnostics = append(diagnostics, lintNonEmptyTypes(document, types)...)
	diagnostics = append(diagnostics, lintInterfaceImplementation(document, types)...)
	diagnostics = append(diagnostics, lintRequiredInputCycles(document, types)...)
	diagnostics = append(diagnostics, lintSchemaDirectives(document, types, directives)...)
	return diagnostics
}

func collectSchemaSymbols(
	document *gqlast.SchemaDocument,
) (map[string]*gqlast.Definition, map[string]*gqlast.DirectiveDefinition, []Diagnostic) {
	types := make(map[string]*gqlast.Definition, len(document.Definitions))
	var diagnostics []Diagnostic
	for _, definition := range document.Definitions {
		if previous := types[definition.Name]; previous != nil {
			position := duplicatePosition(previous.Position, definition.Position)
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.unique_type", position,
				fmt.Sprintf("type %q is defined more than once", definition.Name),
			))
			continue
		}
		types[definition.Name] = definition
	}

	directives := make(map[string]*gqlast.DirectiveDefinition, len(document.Directives))
	for _, definition := range document.Directives {
		if previous := directives[definition.Name]; previous != nil {
			position := duplicatePosition(previous.Position, definition.Position)
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.directive.unique_definition", position,
				fmt.Sprintf("directive @%s is defined more than once", definition.Name),
			))
			continue
		}
		directives[definition.Name] = definition
	}
	return types, directives, diagnostics
}

func duplicatePosition(first, second *gqlast.Position) *gqlast.Position {
	if isBuiltInPosition(second) && !isBuiltInPosition(first) {
		return first
	}
	return second
}

func lintDirectiveDefinitions(
	document *gqlast.SchemaDocument,
	types map[string]*gqlast.Definition,
	directives map[string]*gqlast.DirectiveDefinition,
) []Diagnostic {
	var diagnostics []Diagnostic
	for _, definition := range document.Directives {
		if isBuiltInPosition(definition.Position) {
			continue
		}
		if strings.HasPrefix(definition.Name, "__") {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.reserved_name", definition.Position,
				fmt.Sprintf("directive name @%s is reserved", definition.Name),
			))
		}
		seen := make(map[string]bool, len(definition.Arguments))
		for _, argument := range definition.Arguments {
			if seen[argument.Name] {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.directive.unique_argument_definition", argument.Position,
					fmt.Sprintf("directive @%s defines argument %q more than once", definition.Name, argument.Name),
				))
				continue
			}
			seen[argument.Name] = true
			if strings.HasPrefix(argument.Name, "__") {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.sdl.reserved_name", argument.Position,
					fmt.Sprintf("directive @%s argument name %q is reserved", definition.Name, argument.Name),
				))
			}
			typeDefinition := typeForReference(argument.Type, types)
			if typeDefinition == nil {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.sdl.known_type", argument.Type.Position,
					fmt.Sprintf("directive @%s argument %q uses an undefined type", definition.Name, argument.Name),
				))
			} else if !isInputKind(typeDefinition.Kind) {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.sdl.input_type", argument.Type.Position,
					fmt.Sprintf("directive @%s argument %q must use an input type", definition.Name, argument.Name),
				))
			}
			if argument.DefaultValue != nil && !constantMatches(argument.DefaultValue, argument.Type, types) {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.directive.default_value", argument.DefaultValue.Position,
					fmt.Sprintf("directive @%s argument %q has an invalid default", definition.Name, argument.Name),
				))
			}
			diagnostics = append(diagnostics, lintDirectiveList(
				argument.Directives, gqlast.LocationArgumentDefinition, directives, types,
			)...)
		}
	}
	return diagnostics
}

func lintOperationTypes(
	document *gqlast.SchemaDocument,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	var diagnostics []Diagnostic
	if len(document.Schema) > 1 {
		for _, definition := range document.Schema[1:] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.unique_schema_definition", definition.Position,
				"schema may have only one definition",
			))
		}
	}
	if len(document.Schema) == 0 && len(document.SchemaExtension) > 0 {
		for _, extension := range document.SchemaExtension {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.schema_extension_base", extension.Position,
				"schema extension requires a schema definition",
			))
		}
	}

	seenOperations := make(map[gqlast.Operation]bool)
	rootOwners := make(map[string]gqlast.Operation)
	definitions := make(gqlast.SchemaDefinitionList, 0, 1+len(document.SchemaExtension))
	if len(document.Schema) > 0 {
		definitions = append(definitions, document.Schema[0])
		definitions = append(definitions, document.SchemaExtension...)
	}
	for _, definition := range definitions {
		for _, root := range definition.OperationTypes {
			if seenOperations[root.Operation] {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.sdl.unique_operation_type", root.Position,
					fmt.Sprintf("schema operation %s is declared more than once", root.Operation),
				))
				continue
			}
			seenOperations[root.Operation] = true
			rootType := types[root.Type]
			if rootType == nil {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.sdl.root_known_type", root.Position,
					fmt.Sprintf("schema root %s refers to undefined type %q", root.Operation, root.Type),
				))
				continue
			}
			if rootType.Kind != gqlast.Object {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.sdl.root_object", root.Position,
					fmt.Sprintf("schema root %s must refer to an object", root.Operation),
				))
				continue
			}
			if owner, duplicate := rootOwners[root.Type]; duplicate && owner != root.Operation {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.sdl.root_distinct", root.Position,
					"schema operation roots must be distinct object types",
				))
			} else {
				rootOwners[root.Type] = root.Operation
			}
		}
	}

	hasExplicitSchema := len(document.Schema) > 0
	hasQuery := seenOperations[gqlast.Query]
	if !hasExplicitSchema {
		query := types["Query"]
		hasQuery = query != nil && query.Kind == gqlast.Object
	}
	if !hasQuery {
		diagnostics = append(diagnostics, diagnosticForPosition(
			"graphql.sdl.query_root_required", firstUserPosition(document),
			"schema must define an object query root",
		))
	}
	return diagnostics
}

func firstUserPosition(document *gqlast.SchemaDocument) *gqlast.Position {
	for _, definition := range document.Schema {
		if !isBuiltInPosition(definition.Position) {
			return definition.Position
		}
	}
	for _, definition := range document.Definitions {
		if !isBuiltInPosition(definition.Position) {
			return definition.Position
		}
	}
	return nil
}

func lintDefinition(
	definition *gqlast.Definition,
	types map[string]*gqlast.Definition,
	directives map[string]*gqlast.DirectiveDefinition,
) []Diagnostic {
	var diagnostics []Diagnostic
	if !isBuiltInPosition(definition.Position) && strings.HasPrefix(definition.Name, "__") {
		diagnostics = append(diagnostics, diagnosticForPosition(
			"graphql.sdl.reserved_name", definition.Position,
			fmt.Sprintf("type name %q is reserved", definition.Name),
		))
	}
	diagnostics = append(diagnostics, lintImplementedInterfaces(definition, types)...)
	diagnostics = append(diagnostics, lintDirectiveList(
		definition.Directives, directiveLocationForDefinition(definition.Kind), directives, types,
	)...)
	diagnostics = append(diagnostics, lintFields(definition, types, directives)...)
	diagnostics = append(diagnostics, lintEnumValues(definition, directives, types)...)
	diagnostics = append(diagnostics, lintUnionMembers(definition, types)...)
	return diagnostics
}

func lintImplementedInterfaces(
	definition *gqlast.Definition,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	seen := make(map[string]bool, len(definition.Interfaces))
	var diagnostics []Diagnostic
	for _, name := range definition.Interfaces {
		if seen[name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.unique_interface", definition.Position,
				fmt.Sprintf("type %s implements %s more than once", definition.Name, name),
			))
			continue
		}
		seen[name] = true
		implemented := types[name]
		if implemented == nil {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.known_interface", definition.Position,
				fmt.Sprintf("type %s implements undefined interface %s", definition.Name, name),
			))
		} else if implemented.Kind != gqlast.Interface {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.interface_type", definition.Position,
				fmt.Sprintf("type %s may implement only interfaces", definition.Name),
			))
		}
	}
	return diagnostics
}

func lintFields(
	definition *gqlast.Definition,
	types map[string]*gqlast.Definition,
	directives map[string]*gqlast.DirectiveDefinition,
) []Diagnostic {
	seen := make(map[string]bool, len(definition.Fields))
	var diagnostics []Diagnostic
	for _, field := range definition.Fields {
		if seen[field.Name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.unique_field", field.Position,
				fmt.Sprintf("field %s.%s is defined more than once", definition.Name, field.Name),
			))
			continue
		}
		seen[field.Name] = true
		if !isBuiltInPosition(field.Position) && strings.HasPrefix(field.Name, "__") {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.reserved_name", field.Position,
				fmt.Sprintf("field name %s.%s is reserved", definition.Name, field.Name),
			))
		}
		diagnostics = append(diagnostics, lintFieldType(definition, field, types)...)
		fieldLocation := gqlast.LocationFieldDefinition
		if definition.Kind == gqlast.InputObject {
			fieldLocation = gqlast.LocationInputFieldDefinition
		}
		diagnostics = append(diagnostics, lintDirectiveList(field.Directives, fieldLocation, directives, types)...)
		diagnostics = append(diagnostics, lintArguments(definition, field, types, directives)...)
	}
	return diagnostics
}

func lintFieldType(
	definition *gqlast.Definition,
	field *gqlast.FieldDefinition,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	typeDefinition := typeForReference(field.Type, types)
	if typeDefinition == nil {
		return []Diagnostic{diagnosticForPosition(
			"graphql.sdl.known_type", field.Type.Position,
			fmt.Sprintf("field %s.%s refers to undefined type %q", definition.Name, field.Name, field.Type.Name()),
		)}
	}
	if definition.Kind == gqlast.InputObject {
		if !isInputKind(typeDefinition.Kind) {
			return []Diagnostic{diagnosticForPosition(
				"graphql.sdl.input_type", field.Type.Position,
				fmt.Sprintf("input field %s.%s must use an input type", definition.Name, field.Name),
			)}
		}
		if field.DefaultValue != nil && !constantMatches(field.DefaultValue, field.Type, types) {
			return []Diagnostic{diagnosticForPosition(
				"graphql.sdl.input_field_default", field.DefaultValue.Position,
				fmt.Sprintf("input field %s.%s has an invalid default", definition.Name, field.Name),
			)}
		}
		return nil
	}
	if (definition.Kind == gqlast.Object || definition.Kind == gqlast.Interface) &&
		!isOutputKind(typeDefinition.Kind) {
		return []Diagnostic{diagnosticForPosition(
			"graphql.sdl.output_type", field.Type.Position,
			fmt.Sprintf("field %s.%s must use an output type", definition.Name, field.Name),
		)}
	}
	return nil
}

func lintArguments(
	definition *gqlast.Definition,
	field *gqlast.FieldDefinition,
	types map[string]*gqlast.Definition,
	directives map[string]*gqlast.DirectiveDefinition,
) []Diagnostic {
	seen := make(map[string]bool, len(field.Arguments))
	var diagnostics []Diagnostic
	for _, argument := range field.Arguments {
		if seen[argument.Name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.unique_argument", argument.Position,
				fmt.Sprintf("argument %s.%s(%s:) is defined more than once", definition.Name, field.Name, argument.Name),
			))
			continue
		}
		seen[argument.Name] = true
		if !isBuiltInPosition(argument.Position) && strings.HasPrefix(argument.Name, "__") {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.reserved_name", argument.Position,
				fmt.Sprintf("argument name %s.%s(%s:) is reserved", definition.Name, field.Name, argument.Name),
			))
		}
		typeDefinition := typeForReference(argument.Type, types)
		if typeDefinition == nil {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.known_type", argument.Type.Position,
				fmt.Sprintf("argument %s.%s(%s:) uses an undefined type", definition.Name, field.Name, argument.Name),
			))
		} else if !isInputKind(typeDefinition.Kind) {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.input_type", argument.Type.Position,
				fmt.Sprintf("argument %s.%s(%s:) must use an input type", definition.Name, field.Name, argument.Name),
			))
		}
		if argument.DefaultValue != nil && !constantMatches(argument.DefaultValue, argument.Type, types) {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.argument_default", argument.DefaultValue.Position,
				fmt.Sprintf("argument %s.%s(%s:) has an invalid default", definition.Name, field.Name, argument.Name),
			))
		}
		diagnostics = append(diagnostics, lintDirectiveList(
			argument.Directives, gqlast.LocationArgumentDefinition, directives, types,
		)...)
	}
	return diagnostics
}

func lintEnumValues(
	definition *gqlast.Definition,
	directives map[string]*gqlast.DirectiveDefinition,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	seen := make(map[string]bool, len(definition.EnumValues))
	var diagnostics []Diagnostic
	for _, value := range definition.EnumValues {
		if seen[value.Name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.unique_enum_value", value.Position,
				fmt.Sprintf("enum value %s.%s is defined more than once", definition.Name, value.Name),
			))
			continue
		}
		seen[value.Name] = true
		if value.Name == "true" || value.Name == "false" || value.Name == "null" {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.enum_value_name", value.Position,
				fmt.Sprintf("enum value %s.%s may not use a reserved literal name", definition.Name, value.Name),
			))
		}
		if !isBuiltInPosition(value.Position) && strings.HasPrefix(value.Name, "__") {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.reserved_name", value.Position,
				fmt.Sprintf("enum value %s.%s uses a reserved name", definition.Name, value.Name),
			))
		}
		diagnostics = append(diagnostics, lintDirectiveList(
			value.Directives, gqlast.LocationEnumValue, directives, types,
		)...)
	}
	return diagnostics
}

func lintUnionMembers(
	definition *gqlast.Definition,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	if definition.Kind != gqlast.Union {
		return nil
	}
	seen := make(map[string]bool, len(definition.Types))
	var diagnostics []Diagnostic
	for index, name := range definition.Types {
		position := definition.Position
		if index < len(definition.TypePositions) {
			position = definition.TypePositions[index]
		}
		if seen[name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.unique_union_member", position,
				fmt.Sprintf("union %s includes %s more than once", definition.Name, name),
			))
			continue
		}
		seen[name] = true
		member := types[name]
		if member == nil {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.known_type", position,
				fmt.Sprintf("union %s refers to undefined type %s", definition.Name, name),
			))
		} else if member.Kind != gqlast.Object {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.union_member_object", position,
				fmt.Sprintf("union %s members must be object types", definition.Name),
			))
		}
	}
	return diagnostics
}

func lintCrossDefinitionMembers(
	document *gqlast.SchemaDocument,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	fieldNames := make(map[string]map[string]bool)
	enumNames := make(map[string]map[string]bool)
	interfaceNames := make(map[string]map[string]bool)
	unionNames := make(map[string]map[string]bool)
	var diagnostics []Diagnostic
	definitions := append(gqlast.DefinitionList(nil), document.Definitions...)
	definitions = append(definitions, document.Extensions...)
	for _, definition := range definitions {
		if types[definition.Name] == nil || isBuiltInPosition(definition.Position) {
			continue
		}
		diagnostics = append(diagnostics, crossMemberDuplicates(
			definition, fieldNames, enumNames, interfaceNames, unionNames,
		)...)
	}
	return diagnostics
}

func crossMemberDuplicates(
	definition *gqlast.Definition,
	fieldNames, enumNames, interfaceNames, unionNames map[string]map[string]bool,
) []Diagnostic {
	if fieldNames[definition.Name] == nil {
		fieldNames[definition.Name] = make(map[string]bool)
		enumNames[definition.Name] = make(map[string]bool)
		interfaceNames[definition.Name] = make(map[string]bool)
		unionNames[definition.Name] = make(map[string]bool)
	}
	var diagnostics []Diagnostic
	diagnostics = append(diagnostics, crossFieldDuplicates(definition, fieldNames[definition.Name])...)
	diagnostics = append(diagnostics, crossEnumDuplicates(definition, enumNames[definition.Name])...)
	diagnostics = append(diagnostics, crossUnionDuplicates(definition, unionNames[definition.Name])...)
	localInterfaces := make(map[string]bool)
	for _, name := range definition.Interfaces {
		if interfaceNames[definition.Name][name] && !localInterfaces[name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.unique_interface", definition.Position,
				fmt.Sprintf("type %s implements %s in more than one section", definition.Name, name),
			))
		}
		localInterfaces[name] = true
	}
	for name := range localInterfaces {
		interfaceNames[definition.Name][name] = true
	}
	return diagnostics
}

func crossUnionDuplicates(definition *gqlast.Definition, seen map[string]bool) []Diagnostic {
	if definition.Kind != gqlast.Union {
		return nil
	}
	local := make(map[string]bool, len(definition.Types))
	var diagnostics []Diagnostic
	for index, name := range definition.Types {
		position := definition.Position
		if index < len(definition.TypePositions) {
			position = definition.TypePositions[index]
		}
		if seen[name] && !local[name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.unique_union_member", position,
				fmt.Sprintf("union %s includes %s in more than one type section", definition.Name, name),
			))
		}
		local[name] = true
	}
	for name := range local {
		seen[name] = true
	}
	return diagnostics
}

func crossFieldDuplicates(definition *gqlast.Definition, seen map[string]bool) []Diagnostic {
	local := make(map[string]bool, len(definition.Fields))
	var diagnostics []Diagnostic
	for _, field := range definition.Fields {
		if seen[field.Name] && !local[field.Name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.unique_field", field.Position,
				fmt.Sprintf("field %s.%s is defined by more than one type section", definition.Name, field.Name),
			))
		}
		local[field.Name] = true
	}
	for name := range local {
		seen[name] = true
	}
	return diagnostics
}

func crossEnumDuplicates(definition *gqlast.Definition, seen map[string]bool) []Diagnostic {
	local := make(map[string]bool, len(definition.EnumValues))
	var diagnostics []Diagnostic
	for _, value := range definition.EnumValues {
		if seen[value.Name] && !local[value.Name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.unique_enum_value", value.Position,
				fmt.Sprintf("enum value %s.%s is defined by more than one type section", definition.Name, value.Name),
			))
		}
		local[value.Name] = true
	}
	for name := range local {
		seen[name] = true
	}
	return diagnostics
}

func lintCrossDefinitionDirectives(
	document *gqlast.SchemaDocument,
	directives map[string]*gqlast.DirectiveDefinition,
) []Diagnostic {
	seen := make(map[string]map[string]bool)
	var diagnostics []Diagnostic
	definitions := append(gqlast.DefinitionList(nil), document.Definitions...)
	definitions = append(definitions, document.Extensions...)
	for _, definition := range definitions {
		if isBuiltInPosition(definition.Position) {
			continue
		}
		if seen[definition.Name] == nil {
			seen[definition.Name] = make(map[string]bool)
		}
		local := make(map[string]bool)
		for _, use := range definition.Directives {
			directiveDefinition := directives[use.Name]
			if directiveDefinition == nil || directiveDefinition.IsRepeatable {
				continue
			}
			if seen[definition.Name][use.Name] && !local[use.Name] {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.directive.unique_per_location", use.Position,
					fmt.Sprintf("directive @%s repeats across type sections", use.Name),
				))
			}
			local[use.Name] = true
		}
		for name := range local {
			seen[definition.Name][name] = true
		}
	}
	return diagnostics
}

type mergedTypeView struct {
	name               string
	kind               gqlast.DefinitionKind
	position           *gqlast.Position
	fields             map[string]*gqlast.FieldDefinition
	fieldOrder         []*gqlast.FieldDefinition
	interfaces         []string
	interfacePositions map[string]*gqlast.Position
	unionMembers       map[string]bool
	enumValues         map[string]bool
}

func buildMergedTypeViews(
	document *gqlast.SchemaDocument,
	types map[string]*gqlast.Definition,
) map[string]*mergedTypeView {
	views := make(map[string]*mergedTypeView, len(types))
	for name, definition := range types {
		views[name] = &mergedTypeView{
			name:               name,
			kind:               definition.Kind,
			position:           definition.Position,
			fields:             make(map[string]*gqlast.FieldDefinition),
			interfacePositions: make(map[string]*gqlast.Position),
			unionMembers:       make(map[string]bool),
			enumValues:         make(map[string]bool),
		}
	}
	sections := append(gqlast.DefinitionList(nil), document.Definitions...)
	sections = append(sections, document.Extensions...)
	for _, section := range sections {
		view := views[section.Name]
		if view == nil || view.kind != section.Kind {
			continue
		}
		for _, field := range section.Fields {
			if _, exists := view.fields[field.Name]; exists {
				continue
			}
			view.fields[field.Name] = field
			view.fieldOrder = append(view.fieldOrder, field)
		}
		for _, name := range section.Interfaces {
			if _, exists := view.interfacePositions[name]; exists {
				continue
			}
			view.interfaces = append(view.interfaces, name)
			view.interfacePositions[name] = section.Position
		}
		for _, name := range section.Types {
			view.unionMembers[name] = true
		}
		for _, value := range section.EnumValues {
			view.enumValues[value.Name] = true
		}
	}
	return views
}

func lintNonEmptyTypes(
	document *gqlast.SchemaDocument,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	views := buildMergedTypeViews(document, types)
	var diagnostics []Diagnostic
	for _, definition := range document.Definitions {
		if isBuiltInPosition(definition.Position) || types[definition.Name] != definition {
			continue
		}
		view := views[definition.Name]
		if view == nil {
			continue
		}
		rule := ""
		kind := ""
		empty := false
		switch definition.Kind {
		case gqlast.Object:
			rule, kind, empty = "graphql.sdl.nonempty_object", "object", len(view.fields) == 0
		case gqlast.Interface:
			rule, kind, empty = "graphql.sdl.nonempty_interface", "interface", len(view.fields) == 0
		case gqlast.InputObject:
			rule, kind, empty = "graphql.sdl.nonempty_input_object", "input object", len(view.fields) == 0
		case gqlast.Enum:
			rule, kind, empty = "graphql.sdl.nonempty_enum", "enum", len(view.enumValues) == 0
		case gqlast.Union:
			rule, kind, empty = "graphql.sdl.nonempty_union", "union", len(view.unionMembers) == 0
		}
		if empty {
			diagnostics = append(diagnostics, diagnosticForPosition(
				rule, definition.Position,
				fmt.Sprintf("%s %s must define at least one member", kind, definition.Name),
			))
		}
	}
	return diagnostics
}

func lintInterfaceImplementation(
	document *gqlast.SchemaDocument,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	views := buildMergedTypeViews(document, types)
	diagnostics := lintInterfaceCycles(document, types, views)
	for _, definition := range document.Definitions {
		if isBuiltInPosition(definition.Position) || types[definition.Name] != definition {
			continue
		}
		implementor := views[definition.Name]
		if implementor == nil || (implementor.kind != gqlast.Object && implementor.kind != gqlast.Interface) {
			continue
		}
		declared := make(map[string]bool, len(implementor.interfaces))
		for _, name := range implementor.interfaces {
			declared[name] = true
		}
		for _, interfaceName := range implementor.interfaces {
			contract := views[interfaceName]
			if contract == nil || contract.kind != gqlast.Interface || contract == implementor {
				continue
			}
			for _, ancestor := range interfaceAncestors(contract, views) {
				if ancestor != implementor.name && !declared[ancestor] {
					diagnostics = append(diagnostics, diagnosticForPosition(
						"graphql.sdl.interface_transitive", implementor.interfacePositions[interfaceName],
						fmt.Sprintf("type %s must explicitly implement transitive interface %s", implementor.name, ancestor),
					))
				}
			}
			diagnostics = append(diagnostics, lintOneInterfaceImplementation(implementor, contract, views)...)
		}
	}
	return diagnostics
}

func lintInterfaceCycles(
	document *gqlast.SchemaDocument,
	types map[string]*gqlast.Definition,
	views map[string]*mergedTypeView,
) []Diagnostic {
	state := make(map[string]uint8)
	var diagnostics []Diagnostic
	var visit func(*mergedTypeView)
	visit = func(current *mergedTypeView) {
		state[current.name] = 1
		for _, name := range current.interfaces {
			next := views[name]
			if next == nil || next.kind != gqlast.Interface {
				continue
			}
			switch state[next.name] {
			case 0:
				visit(next)
			case 1:
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.sdl.interface_cycle", current.interfacePositions[name],
					fmt.Sprintf("interface implementation cycle reaches %s", next.name),
				))
			}
		}
		state[current.name] = 2
	}
	for _, definition := range document.Definitions {
		if isBuiltInPosition(definition.Position) || types[definition.Name] != definition ||
			definition.Kind != gqlast.Interface || state[definition.Name] != 0 {
			continue
		}
		visit(views[definition.Name])
	}
	return diagnostics
}

func interfaceAncestors(contract *mergedTypeView, views map[string]*mergedTypeView) []string {
	seen := make(map[string]bool)
	queue := append([]string(nil), contract.interfaces...)
	var result []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] || name == contract.name {
			continue
		}
		seen[name] = true
		ancestor := views[name]
		if ancestor == nil || ancestor.kind != gqlast.Interface {
			continue
		}
		result = append(result, name)
		queue = append(queue, ancestor.interfaces...)
	}
	return result
}

func lintOneInterfaceImplementation(
	implementor, contract *mergedTypeView,
	views map[string]*mergedTypeView,
) []Diagnostic {
	var diagnostics []Diagnostic
	for _, contractField := range contract.fieldOrder {
		field := implementor.fields[contractField.Name]
		if field == nil {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.interface_field", implementor.position,
				fmt.Sprintf("type %s does not provide interface field %s.%s", implementor.name, contract.name, contractField.Name),
			))
			continue
		}
		if !isOutputSubtype(field.Type, contractField.Type, views) {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.interface_field_type", field.Type.Position,
				fmt.Sprintf("field %s.%s is not a valid subtype of %s.%s", implementor.name, field.Name, contract.name, contractField.Name),
			))
		}
		contractArguments := make(map[string]*gqlast.ArgumentDefinition, len(contractField.Arguments))
		for _, contractArgument := range contractField.Arguments {
			contractArguments[contractArgument.Name] = contractArgument
			argument := field.Arguments.ForName(contractArgument.Name)
			if argument == nil {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.sdl.interface_argument", field.Position,
					fmt.Sprintf("field %s.%s omits interface argument %s", implementor.name, field.Name, contractArgument.Name),
				))
				continue
			}
			if argument.Type == nil || contractArgument.Type == nil || argument.Type.String() != contractArgument.Type.String() {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.sdl.interface_argument_type", argument.Position,
					fmt.Sprintf("field %s.%s argument %s must match the interface type", implementor.name, field.Name, argument.Name),
				))
			}
		}
		for _, argument := range field.Arguments {
			if contractArguments[argument.Name] == nil && argument.Type != nil &&
				argument.Type.NonNull && argument.DefaultValue == nil {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.sdl.interface_extra_required_argument", argument.Position,
					fmt.Sprintf("field %s.%s adds required argument %s outside the interface", implementor.name, field.Name, argument.Name),
				))
			}
		}
	}
	return diagnostics
}

func isOutputSubtype(
	implementation, contract *gqlast.Type,
	views map[string]*mergedTypeView,
) bool {
	if implementation == nil || contract == nil {
		return false
	}
	if contract.NonNull && !implementation.NonNull {
		return false
	}
	if (implementation.Elem == nil) != (contract.Elem == nil) {
		return false
	}
	if implementation.Elem != nil {
		return isOutputSubtype(implementation.Elem, contract.Elem, views)
	}
	if implementation.NamedType == contract.NamedType {
		return true
	}
	implemented := views[implementation.NamedType]
	wanted := views[contract.NamedType]
	if implemented == nil || wanted == nil {
		return false
	}
	if wanted.kind == gqlast.Interface &&
		(implemented.kind == gqlast.Object || implemented.kind == gqlast.Interface) {
		return typeImplementsInterface(implemented, wanted.name, views, make(map[string]bool))
	}
	return wanted.kind == gqlast.Union && implemented.kind == gqlast.Object &&
		wanted.unionMembers[implemented.name]
}

func typeImplementsInterface(
	definition *mergedTypeView,
	wanted string,
	views map[string]*mergedTypeView,
	seen map[string]bool,
) bool {
	if definition == nil || seen[definition.name] {
		return false
	}
	seen[definition.name] = true
	for _, name := range definition.interfaces {
		if name == wanted {
			return true
		}
		if typeImplementsInterface(views[name], wanted, views, seen) {
			return true
		}
	}
	return false
}

func lintRequiredInputCycles(
	document *gqlast.SchemaDocument,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	state := make(map[string]uint8)
	var diagnostics []Diagnostic
	for _, definition := range document.Definitions {
		if definition.Kind != gqlast.InputObject || isBuiltInPosition(definition.Position) || state[definition.Name] != 0 {
			continue
		}
		if position := findRequiredInputCycle(definition.Name, types, state); position != nil {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.sdl.input_cycle", position,
				"required singular input fields must not form a cycle",
			))
		}
	}
	return diagnostics
}

func findRequiredInputCycle(
	name string,
	types map[string]*gqlast.Definition,
	state map[string]uint8,
) *gqlast.Position {
	if state[name] == 1 {
		return types[name].Position
	}
	if state[name] == 2 {
		return nil
	}
	state[name] = 1
	definition := types[name]
	for _, field := range definition.Fields {
		if field.Type == nil || !field.Type.NonNull || field.Type.Elem != nil {
			continue
		}
		next := types[field.Type.NamedType]
		if next == nil || next.Kind != gqlast.InputObject {
			continue
		}
		if state[next.Name] == 1 {
			return field.Position
		}
		if position := findRequiredInputCycle(next.Name, types, state); position != nil {
			return position
		}
	}
	state[name] = 2
	return nil
}

func lintSchemaDirectives(
	document *gqlast.SchemaDocument,
	types map[string]*gqlast.Definition,
	directives map[string]*gqlast.DirectiveDefinition,
) []Diagnostic {
	var diagnostics []Diagnostic
	seen := make(map[string]bool)
	definitions := append(gqlast.SchemaDefinitionList(nil), document.Schema...)
	definitions = append(definitions, document.SchemaExtension...)
	for _, definition := range definitions {
		diagnostics = append(diagnostics, lintDirectiveList(
			definition.Directives, gqlast.LocationSchema, directives, types,
		)...)
		for _, use := range definition.Directives {
			directiveDefinition := directives[use.Name]
			if directiveDefinition == nil || directiveDefinition.IsRepeatable {
				continue
			}
			if seen[use.Name] {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.directive.unique_per_location", use.Position,
					fmt.Sprintf("directive @%s repeats across schema sections", use.Name),
				))
			}
			seen[use.Name] = true
		}
	}
	return diagnostics
}

func lintDirectiveList(
	uses gqlast.DirectiveList,
	location gqlast.DirectiveLocation,
	definitions map[string]*gqlast.DirectiveDefinition,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	seen := make(map[string]bool, len(uses))
	var diagnostics []Diagnostic
	for _, use := range uses {
		if isBuiltInPosition(use.Position) {
			continue
		}
		definition := definitions[use.Name]
		if isPostOctober2021Use(use.Name, location, definition) {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"conduit.spec_version.october_2021", use.Position,
				fmt.Sprintf("directive @%s is outside GraphQL October 2021", use.Name),
			))
			continue
		}
		if definition == nil {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.directive.known", use.Position,
				fmt.Sprintf("directive @%s is not defined", use.Name),
			))
			continue
		}
		if seen[use.Name] && !definition.IsRepeatable {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.directive.unique_per_location", use.Position,
				fmt.Sprintf("directive @%s is not repeatable at one location", use.Name),
			))
			continue
		}
		seen[use.Name] = true
		if isConduitDirective(use.Name) {
			diagnostics = append(diagnostics, lintDirectiveArguments(use, definition, types, false, false)...)
			continue
		}
		if !slices.Contains(definition.Locations, location) {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.directive.location", use.Position,
				fmt.Sprintf("directive @%s is not valid on %s", use.Name, location),
			))
			continue
		}
		diagnostics = append(diagnostics, lintDirectiveArguments(use, definition, types, true, true)...)
	}
	return diagnostics
}

func isPostOctober2021Use(
	name string,
	location gqlast.DirectiveLocation,
	definition *gqlast.DirectiveDefinition,
) bool {
	if name == "deprecated" &&
		(location == gqlast.LocationArgumentDefinition || location == gqlast.LocationInputFieldDefinition) {
		return true
	}
	if name != "defer" && name != "stream" && name != "oneOf" {
		return false
	}
	return definition == nil || isBuiltInPosition(definition.Position)
}

func lintDirectiveArguments(
	use *gqlast.Directive,
	definition *gqlast.DirectiveDefinition,
	types map[string]*gqlast.Definition,
	validateValues bool,
	validateRequired bool,
) []Diagnostic {
	seen := make(map[string]bool, len(use.Arguments))
	provided := make(map[string]bool, len(use.Arguments))
	var diagnostics []Diagnostic
	for _, argument := range use.Arguments {
		if seen[argument.Name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.directive.unique_argument", argument.Position,
				fmt.Sprintf("directive @%s argument %q is supplied more than once", use.Name, argument.Name),
			))
			continue
		}
		seen[argument.Name] = true
		argumentDefinition := definition.Arguments.ForName(argument.Name)
		if argumentDefinition == nil {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.directive.known_argument", argument.Position,
				fmt.Sprintf("directive @%s has no argument %q", use.Name, argument.Name),
			))
			continue
		}
		provided[argument.Name] = true
		if validateValues {
			diagnostics = append(diagnostics, directiveLiteralDiagnostics(
				use.Name, argument.Name, argument.Value, argumentDefinition.Type, types,
			)...)
		}
	}
	if validateRequired {
		for _, argument := range definition.Arguments {
			if argument.Type.NonNull && argument.DefaultValue == nil && !provided[argument.Name] {
				diagnostics = append(diagnostics, diagnosticForPosition(
					"graphql.directive.required_argument", use.Position,
					fmt.Sprintf("directive @%s requires argument %q", use.Name, argument.Name),
				))
			}
		}
	}
	return diagnostics
}

func directiveLiteralDiagnostics(
	directiveName, argumentName string,
	value *gqlast.Value,
	reference *gqlast.Type,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	return inputLiteralDiagnostics(
		directiveName, argumentName, value, reference, types, false,
	)
}

func inputLiteralDiagnostics(
	directiveName, argumentName string,
	value *gqlast.Value,
	reference *gqlast.Type,
	types map[string]*gqlast.Definition,
	nested bool,
) []Diagnostic {
	if value == nil || reference == nil {
		return literalTypeDiagnostic(directiveName, argumentName, value, nested)
	}
	if value.Kind == gqlast.Variable {
		return []Diagnostic{diagnosticForPosition(
			"graphql.directive.argument_const", value.Position,
			fmt.Sprintf("directive @%s argument %q must be constant", directiveName, argumentName),
		)}
	}
	if value.Kind == gqlast.NullValue {
		if reference.NonNull {
			return literalTypeDiagnostic(directiveName, argumentName, value, nested)
		}
		return nil
	}
	if reference.Elem != nil {
		if value.Kind != gqlast.ListValue {
			return inputLiteralDiagnostics(directiveName, argumentName, value, reference.Elem, types, nested)
		}
		var diagnostics []Diagnostic
		for _, child := range value.Children {
			diagnostics = append(diagnostics, inputLiteralDiagnostics(
				directiveName, argumentName, child.Value, reference.Elem, types, nested,
			)...)
		}
		return diagnostics
	}
	definition := types[reference.NamedType]
	if definition != nil && definition.Kind == gqlast.InputObject {
		return inputObjectLiteralDiagnostics(directiveName, argumentName, value, definition, types)
	}
	if scalarOrEnumLiteralMatches(value, reference.NamedType, definition) {
		return nil
	}
	return literalTypeDiagnostic(directiveName, argumentName, value, nested)
}

func inputObjectLiteralDiagnostics(
	directiveName, argumentName string,
	value *gqlast.Value,
	definition *gqlast.Definition,
	types map[string]*gqlast.Definition,
) []Diagnostic {
	if value.Kind != gqlast.ObjectValue {
		return literalTypeDiagnostic(directiveName, argumentName, value, false)
	}
	seen := make(map[string]bool, len(value.Children))
	provided := make(map[string]bool, len(value.Children))
	var diagnostics []Diagnostic
	for _, child := range value.Children {
		if seen[child.Name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.directive.input_field_unique", child.Position,
				fmt.Sprintf("input field %q is supplied more than once", child.Name),
			))
			continue
		}
		seen[child.Name] = true
		field := definition.Fields.ForName(child.Name)
		if field == nil {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.directive.input_field_known", child.Position,
				fmt.Sprintf("input object %s has no field %q", definition.Name, child.Name),
			))
			continue
		}
		provided[child.Name] = true
		diagnostics = append(diagnostics, inputLiteralDiagnostics(
			directiveName, argumentName, child.Value, field.Type, types, true,
		)...)
	}
	for _, field := range definition.Fields {
		if field.Type.NonNull && field.DefaultValue == nil && !provided[field.Name] {
			diagnostics = append(diagnostics, diagnosticForPosition(
				"graphql.directive.input_field_required", value.Position,
				fmt.Sprintf("input object %s requires field %q", definition.Name, field.Name),
			))
		}
	}
	return diagnostics
}

func literalTypeDiagnostic(
	directiveName, argumentName string,
	value *gqlast.Value,
	nested bool,
) []Diagnostic {
	rule := "graphql.directive.argument_type"
	message := fmt.Sprintf("directive @%s argument %q has an invalid value", directiveName, argumentName)
	if nested {
		rule = "graphql.directive.input_field_type"
		message = fmt.Sprintf("directive @%s argument %q has an invalid nested input field", directiveName, argumentName)
	}
	var position *gqlast.Position
	if value != nil {
		position = value.Position
	}
	return []Diagnostic{diagnosticForPosition(rule, position, message)}
}

func constantMatches(
	value *gqlast.Value,
	reference *gqlast.Type,
	types map[string]*gqlast.Definition,
) bool {
	return len(inputLiteralDiagnostics("default", "value", value, reference, types, false)) == 0
}

func scalarOrEnumLiteralMatches(
	value *gqlast.Value,
	typeName string,
	definition *gqlast.Definition,
) bool {
	switch typeName {
	case "String":
		return value.Kind == gqlast.StringValue || value.Kind == gqlast.BlockValue
	case "Boolean":
		return value.Kind == gqlast.BooleanValue
	case "Int":
		if value.Kind != gqlast.IntValue {
			return false
		}
		_, err := strconv.ParseInt(value.Raw, 10, 32)
		return err == nil
	case "Float":
		return value.Kind == gqlast.IntValue || value.Kind == gqlast.FloatValue
	case "ID":
		if value.Kind == gqlast.StringValue || value.Kind == gqlast.BlockValue {
			return true
		}
		if value.Kind != gqlast.IntValue {
			return false
		}
		_, err := strconv.ParseInt(value.Raw, 10, 32)
		return err == nil
	}
	if definition == nil {
		return false
	}
	switch definition.Kind {
	case gqlast.Enum:
		return value.Kind == gqlast.EnumValue && definition.EnumValues.ForName(value.Raw) != nil
	case gqlast.Scalar:
		return value.Kind != gqlast.ListValue && value.Kind != gqlast.ObjectValue
	default:
		return false
	}
}

func typeForReference(reference *gqlast.Type, types map[string]*gqlast.Definition) *gqlast.Definition {
	if reference == nil {
		return nil
	}
	return types[reference.Name()]
}

func isInputKind(kind gqlast.DefinitionKind) bool {
	return kind == gqlast.Scalar || kind == gqlast.Enum || kind == gqlast.InputObject
}

func isOutputKind(kind gqlast.DefinitionKind) bool {
	return kind == gqlast.Scalar || kind == gqlast.Object || kind == gqlast.Interface ||
		kind == gqlast.Union || kind == gqlast.Enum
}

func directiveLocationForDefinition(kind gqlast.DefinitionKind) gqlast.DirectiveLocation {
	switch kind {
	case gqlast.Scalar:
		return gqlast.LocationScalar
	case gqlast.Object:
		return gqlast.LocationObject
	case gqlast.Interface:
		return gqlast.LocationInterface
	case gqlast.Union:
		return gqlast.LocationUnion
	case gqlast.Enum:
		return gqlast.LocationEnum
	case gqlast.InputObject:
		return gqlast.LocationInputObject
	default:
		return gqlast.LocationObject
	}
}

func diagnosticForPosition(rule string, position *gqlast.Position, message string) Diagnostic {
	parsed := positionOf(position)
	return diagnosticAt(rule, parsed.File, parsed.Line, parsed.Column, message)
}
