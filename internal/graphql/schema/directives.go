package schema

import (
	"strconv"
	"strings"

	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
)

type directiveContext struct {
	typeName     string
	fieldName    string
	argumentName string
	location     string
	field        *graphqlast.FieldDefinition
}

func validateConduitDirectives(
	snapshot graphqlast.SchemaSnapshot,
	sourceNames map[string]struct{},
	authRules map[string]struct{},
) (map[fieldKey]FieldMetadata, map[argumentKey]ArgumentMetadata, []graphqlast.Diagnostic) {
	fields := make(map[fieldKey]FieldMetadata)
	arguments := make(map[argumentKey]ArgumentMetadata)
	types := make(map[string]graphqlast.TypeDefinition, len(snapshot.Types))
	for _, definition := range snapshot.Types {
		types[definition.Name] = definition
	}
	subscriptionPayloads := subscriptionPayloadTypes(snapshot, types)
	var diagnostics []graphqlast.Diagnostic

	for typeIndex := range snapshot.Types {
		definition := &snapshot.Types[typeIndex]
		for _, directive := range definition.Directives {
			diagnostics = append(diagnostics, validateDirective(
				directive,
				directiveContext{typeName: definition.Name, location: "type"},
				snapshot, sourceNames, authRules, subscriptionPayloads,
				nil, nil,
			)...)
		}
		for fieldIndex := range definition.Fields {
			field := &definition.Fields[fieldIndex]
			key := fieldKey{parent: definition.Name, field: field.Name}
			metadata := FieldMetadata{Complexity: ComplexityMetadata{Cost: 1}}
			for _, directive := range field.Directives {
				diagnostics = append(diagnostics, validateDirective(
					directive,
					directiveContext{
						typeName: definition.Name, fieldName: field.Name,
						location: "field", field: field,
					},
					snapshot, sourceNames, authRules, subscriptionPayloads,
					&metadata, nil,
				)...)
			}
			fields[key] = metadata
			for argumentIndex := range field.Arguments {
				argument := &field.Arguments[argumentIndex]
				argumentMetadata := ArgumentMetadata{}
				for _, directive := range argument.Directives {
					diagnostics = append(diagnostics, validateDirective(
						directive,
						directiveContext{
							typeName: definition.Name, fieldName: field.Name,
							argumentName: argument.Name, location: "argument", field: field,
						},
						snapshot, sourceNames, authRules, subscriptionPayloads,
						nil, &argumentMetadata,
					)...)
				}
				arguments[argumentKey{
					parent: definition.Name, field: field.Name, argument: argument.Name,
				}] = argumentMetadata
			}
		}
		for _, enumValue := range definition.EnumValues {
			for _, directive := range enumValue.Directives {
				diagnostics = append(diagnostics, validateDirective(
					directive,
					directiveContext{typeName: definition.Name, location: "enum value"},
					snapshot, sourceNames, authRules, subscriptionPayloads,
					nil, nil,
				)...)
			}
		}
	}
	return fields, arguments, diagnostics
}

func validateDirective(
	directive graphqlast.DirectiveUse,
	context directiveContext,
	snapshot graphqlast.SchemaSnapshot,
	sourceNames map[string]struct{},
	authRules map[string]struct{},
	subscriptionPayloads map[string]struct{},
	fieldMetadata *FieldMetadata,
	argumentMetadata *ArgumentMetadata,
) []graphqlast.Diagnostic {
	switch directive.Name {
	case "source":
		return validateSource(directive, context, sourceNames, subscriptionPayloads, fieldMetadata)
	case "auth":
		return validateAuth(directive, context, authRules, fieldMetadata)
	case "filterable":
		return validateFilterable(directive, context, snapshot.Subscription, argumentMetadata)
	case "backpressure":
		return validateBackpressure(directive, context, snapshot.Subscription, fieldMetadata)
	case "complexity":
		return validateComplexity(directive, context, fieldMetadata)
	default:
		return nil
	}
}

func validateSource(
	directive graphqlast.DirectiveUse,
	context directiveContext,
	known map[string]struct{},
	subscriptionPayloads map[string]struct{},
	metadata *FieldMetadata,
) []graphqlast.Diagnostic {
	if context.location != "field" {
		return oneDiagnostic("conduit.source.location", directive.Position, "@source is valid only on fields")
	}
	argument, ok := directiveArgument(directive, "name")
	if !ok {
		return oneDiagnostic("conduit.source.name_required", directive.Position, "@source requires name")
	}
	if argument.Value.Kind != graphqlast.ValueString {
		return oneDiagnostic("conduit.source.name_type", argument.Value.Position, "@source name must be a string")
	}
	if strings.TrimSpace(argument.Value.Raw) == "" {
		return oneDiagnostic("conduit.source.name_nonempty", argument.Value.Position, "@source name must be nonempty")
	}
	if _, found := known[argument.Value.Raw]; !found {
		return oneDiagnostic("conduit.source.name_unknown", argument.Value.Position, "@source name is not registered")
	}
	if _, forbidden := subscriptionPayloads[context.typeName]; forbidden {
		return oneDiagnostic("conduit.source.subscription_io", directive.Position, "@source is forbidden on subscription output fields")
	}
	if metadata != nil {
		metadata.HasSource = true
		metadata.SourceName = argument.Value.Raw
	}
	return nil
}

func validateAuth(
	directive graphqlast.DirectiveUse,
	context directiveContext,
	known map[string]struct{},
	metadata *FieldMetadata,
) []graphqlast.Diagnostic {
	if context.location != "field" {
		return oneDiagnostic("conduit.auth.location", directive.Position, "@auth is valid only on fields")
	}
	argument, ok := directiveArgument(directive, "rule")
	if !ok {
		return oneDiagnostic("conduit.auth.rule_required", directive.Position, "@auth requires rule")
	}
	if argument.Value.Kind != graphqlast.ValueString {
		return oneDiagnostic("conduit.auth.rule_type", argument.Value.Position, "@auth rule must be a string")
	}
	if strings.TrimSpace(argument.Value.Raw) == "" {
		return oneDiagnostic("conduit.auth.rule_nonempty", argument.Value.Position, "@auth rule must be nonempty")
	}
	if _, found := known[argument.Value.Raw]; !found {
		return oneDiagnostic("conduit.auth.rule_undefined", argument.Value.Position, "@auth rule is not registered")
	}
	if metadata != nil {
		// Validation and metadata extraction are intentional; enforcement is
		// owned by the authorization gate, not R1.02.
		metadata.HasAuth = true
		metadata.AuthRule = argument.Value.Raw
	}
	return nil
}

func validateFilterable(
	directive graphqlast.DirectiveUse,
	context directiveContext,
	subscriptionRoot string,
	metadata *ArgumentMetadata,
) []graphqlast.Diagnostic {
	if context.location != "argument" {
		return oneDiagnostic("conduit.filterable.location", directive.Position, "@filterable is valid only on arguments")
	}
	if context.typeName != subscriptionRoot {
		return oneDiagnostic("conduit.filterable.subscription_argument", directive.Position, "@filterable requires a root subscription argument")
	}
	argument := fieldArgument(context.field, context.argumentName)
	if argument == nil || argument.Type.Element != nil {
		return oneDiagnostic("conduit.filterable.supported_scalar", directive.Position, "@filterable requires a built-in scalar")
	}
	scalar, supported := builtinScalar(argument.Type.Named)
	if !supported {
		return oneDiagnostic("conduit.filterable.supported_scalar", directive.Position, "@filterable requires a built-in scalar")
	}
	if metadata != nil {
		metadata.Filterable = true
		metadata.Scalar = scalar
	}
	return nil
}

func validateBackpressure(
	directive graphqlast.DirectiveUse,
	context directiveContext,
	subscriptionRoot string,
	metadata *FieldMetadata,
) []graphqlast.Diagnostic {
	if context.location != "field" || context.typeName != subscriptionRoot {
		return oneDiagnostic("conduit.backpressure.location", directive.Position, "@backpressure requires a root subscription field")
	}
	result := BackpressureMetadata{Present: true}
	var diagnostics []graphqlast.Diagnostic
	if argument, ok := directiveArgument(directive, "policy"); ok {
		if argument.Value.Kind != graphqlast.ValueEnum {
			diagnostics = append(diagnostics, oneDiagnostic(
				"conduit.backpressure.policy", argument.Value.Position, "@backpressure policy is invalid",
			)...)
		} else {
			result.Policy = BackpressurePolicy(argument.Value.Raw)
			switch result.Policy {
			case BackpressureDropOldest, BackpressureCoalesceByKey, BackpressureDisconnect:
			default:
				diagnostics = append(diagnostics, oneDiagnostic(
					"conduit.backpressure.policy", argument.Value.Position, "@backpressure policy is invalid",
				)...)
			}
		}
	}
	if argument, ok := directiveArgument(directive, "queue"); ok {
		if argument.Value.Kind != graphqlast.ValueInt {
			diagnostics = append(diagnostics, oneDiagnostic(
				"conduit.backpressure.queue_type", argument.Value.Position, "@backpressure queue must be an integer",
			)...)
		} else if queue, err := strconv.ParseInt(argument.Value.Raw, 10, 32); err != nil {
			diagnostics = append(diagnostics, oneDiagnostic(
				"conduit.backpressure.queue_type", argument.Value.Position, "@backpressure queue must be an integer",
			)...)
		} else if queue <= 0 {
			diagnostics = append(diagnostics, oneDiagnostic(
				"conduit.backpressure.queue_positive", argument.Value.Position, "@backpressure queue must be positive",
			)...)
		} else {
			result.Queue = int(queue)
		}
	}
	if argument, ok := directiveArgument(directive, "coalesceKey"); ok {
		if argument.Value.Kind != graphqlast.ValueString || strings.TrimSpace(argument.Value.Raw) == "" {
			diagnostics = append(diagnostics, oneDiagnostic(
				"conduit.backpressure.coalesce_key_nonempty", argument.Value.Position,
				"@backpressure coalesceKey must be a nonempty string",
			)...)
		} else {
			// R6, not schema loading, owns response-path compilation and semantics.
			result.CoalesceKey = argument.Value.Raw
		}
	}
	if len(diagnostics) == 0 && metadata != nil {
		metadata.Backpressure = result
	}
	return diagnostics
}

func validateComplexity(
	directive graphqlast.DirectiveUse,
	context directiveContext,
	metadata *FieldMetadata,
) []graphqlast.Diagnostic {
	if context.location != "field" {
		return oneDiagnostic("conduit.complexity.location", directive.Position, "@complexity is valid only on fields")
	}
	result := ComplexityMetadata{Cost: 1}
	var diagnostics []graphqlast.Diagnostic
	if argument, ok := directiveArgument(directive, "cost"); ok {
		if argument.Value.Kind != graphqlast.ValueInt {
			diagnostics = append(diagnostics, oneDiagnostic(
				"conduit.complexity.cost_type", argument.Value.Position, "@complexity cost must be an integer",
			)...)
		} else if cost, err := strconv.ParseInt(argument.Value.Raw, 10, 32); err != nil {
			diagnostics = append(diagnostics, oneDiagnostic(
				"conduit.complexity.cost_type", argument.Value.Position, "@complexity cost must be an integer",
			)...)
		} else if cost < 0 {
			diagnostics = append(diagnostics, oneDiagnostic(
				"conduit.complexity.cost_nonnegative", argument.Value.Position, "@complexity cost must be nonnegative",
			)...)
		} else {
			result.Cost = int(cost)
		}
	}
	if argument, ok := directiveArgument(directive, "multipliers"); ok {
		values, valid := stringList(argument.Value)
		if !valid {
			diagnostics = append(diagnostics, oneDiagnostic(
				"conduit.complexity.multiplier_nonempty", argument.Value.Position,
				"@complexity multipliers must be nonempty argument names",
			)...)
		} else {
			seen := make(map[string]bool, len(values))
			for _, multiplier := range values {
				switch {
				case strings.TrimSpace(multiplier) == "":
					diagnostics = append(diagnostics, oneDiagnostic(
						"conduit.complexity.multiplier_nonempty", argument.Value.Position,
						"@complexity multiplier must be nonempty",
					)...)
				case seen[multiplier]:
					diagnostics = append(diagnostics, oneDiagnostic(
						"conduit.complexity.multiplier_unique", argument.Value.Position,
						"@complexity multipliers must be unique",
					)...)
				default:
					seen[multiplier] = true
					definition := fieldArgument(context.field, multiplier)
					if definition == nil {
						diagnostics = append(diagnostics, oneDiagnostic(
							"conduit.complexity.multiplier_argument", argument.Value.Position,
							"@complexity multiplier must name an argument on the same field",
						)...)
					} else if definition.Type.Element != nil || definition.Type.Named != "Int" {
						diagnostics = append(diagnostics, oneDiagnostic(
							"conduit.complexity.multiplier_type", argument.Value.Position,
							"@complexity multiplier argument must have scalar Int type",
						)...)
					}
				}
			}
			result.Multipliers = values
		}
	}
	if len(diagnostics) == 0 && metadata != nil {
		metadata.Complexity = result
	}
	return diagnostics
}

func directiveArgument(directive graphqlast.DirectiveUse, name string) (graphqlast.DirectiveArgument, bool) {
	for _, argument := range directive.Arguments {
		if argument.Name == name {
			return argument, true
		}
	}
	return graphqlast.DirectiveArgument{}, false
}

func fieldArgument(field *graphqlast.FieldDefinition, name string) *graphqlast.ArgumentDefinition {
	if field == nil {
		return nil
	}
	for index := range field.Arguments {
		if field.Arguments[index].Name == name {
			return &field.Arguments[index]
		}
	}
	return nil
}

func stringList(value graphqlast.Value) ([]string, bool) {
	if value.Kind == graphqlast.ValueString {
		return []string{value.Raw}, true
	}
	if value.Kind != graphqlast.ValueList {
		return nil, false
	}
	result := make([]string, 0, len(value.Children))
	for _, child := range value.Children {
		if child.Value.Kind != graphqlast.ValueString {
			return nil, false
		}
		result = append(result, child.Value.Raw)
	}
	return result, true
}

func builtinScalar(name string) (Scalar, bool) {
	switch name {
	case "String":
		return ScalarString, true
	case "ID":
		return ScalarID, true
	case "Int":
		return ScalarInt, true
	case "Float":
		return ScalarFloat, true
	case "Boolean":
		return ScalarBoolean, true
	default:
		return "", false
	}
}

func subscriptionPayloadTypes(
	snapshot graphqlast.SchemaSnapshot,
	types map[string]graphqlast.TypeDefinition,
) map[string]struct{} {
	result := make(map[string]struct{})
	if snapshot.Subscription == "" {
		return result
	}
	result[snapshot.Subscription] = struct{}{}
	queue := []string{snapshot.Subscription}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		definition, ok := types[current]
		if !ok {
			continue
		}
		enqueue := func(name string) {
			if _, userType := types[name]; !userType {
				return
			}
			if _, seen := result[name]; seen {
				return
			}
			result[name] = struct{}{}
			queue = append(queue, name)
		}
		for _, field := range definition.Fields {
			enqueue(namedType(field.Type))
		}
		for _, member := range definition.UnionTypes {
			enqueue(member)
		}
		for _, implemented := range definition.Interfaces {
			enqueue(implemented)
		}
		if definition.Kind == "INTERFACE" {
			for _, candidate := range types {
				for _, implemented := range candidate.Interfaces {
					if implemented == definition.Name {
						enqueue(candidate.Name)
						break
					}
				}
			}
		}
	}
	return result
}

func namedType(reference graphqlast.TypeRef) string {
	for reference.Element != nil {
		reference = *reference.Element
	}
	return reference.Named
}

func oneDiagnostic(rule string, position graphqlast.SourcePosition, message string) []graphqlast.Diagnostic {
	return []graphqlast.Diagnostic{{
		Rule: rule, File: position.File, Line: position.Line, Column: position.Column, Message: message,
	}}
}
