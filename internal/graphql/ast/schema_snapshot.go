package ast

import (
	"sort"

	gqlast "github.com/vektah/gqlparser/v2/ast"
)

func snapshotSchema(schema *gqlast.Schema) SchemaSnapshot {
	result := SchemaSnapshot{Description: schema.Description}
	if schema.Query != nil {
		result.Query = schema.Query.Name
	}
	if schema.Mutation != nil {
		result.Mutation = schema.Mutation.Name
	}
	if schema.Subscription != nil {
		result.Subscription = schema.Subscription.Name
	}
	result.SchemaDirectives = snapshotDirectives(schema.SchemaDirectives)
	for _, definition := range schema.Types {
		if definition == nil ||
			(definition.BuiltIn && definition.Name != "ConduitBackpressurePolicy") ||
			definition.Name == "" || definition.Name[:min(2, len(definition.Name))] == "__" {
			continue
		}
		result.Types = append(result.Types, snapshotTypeDefinition(definition))
	}
	for _, definition := range schema.Directives {
		if definition == nil ||
			(isBuiltInPosition(definition.Position) && !isConduitDirective(definition.Name)) {
			continue
		}
		result.Directives = append(result.Directives, snapshotDirectiveDefinition(definition))
	}
	sort.Slice(result.Types, func(i, j int) bool { return result.Types[i].Name < result.Types[j].Name })
	sort.Slice(result.Directives, func(i, j int) bool { return result.Directives[i].Name < result.Directives[j].Name })
	return result
}

func snapshotTypeDefinition(definition *gqlast.Definition) TypeDefinition {
	result := TypeDefinition{
		Kind: string(definition.Kind), Name: definition.Name, Description: definition.Description,
		Interfaces: append([]string(nil), definition.Interfaces...),
		UnionTypes: append([]string(nil), definition.Types...),
		Directives: snapshotDirectives(definition.Directives), Position: positionOf(definition.Position),
	}
	for _, field := range definition.Fields {
		if field.Position == nil && (field.Name == "__schema" || field.Name == "__type") {
			continue
		}
		result.Fields = append(result.Fields, snapshotField(field))
	}
	for _, value := range definition.EnumValues {
		result.EnumValues = append(result.EnumValues, EnumValueDefinition{
			Name: value.Name, Description: value.Description,
			Directives: snapshotDirectives(value.Directives), Position: positionOf(value.Position),
		})
	}
	return result
}

func snapshotField(field *gqlast.FieldDefinition) FieldDefinition {
	result := FieldDefinition{
		Name: field.Name, Description: field.Description, Type: snapshotTypeRef(field.Type),
		Directives: snapshotDirectives(field.Directives), Position: positionOf(field.Position),
	}
	if field.DefaultValue != nil {
		value := snapshotValue(field.DefaultValue)
		result.DefaultValue = &value
	}
	for _, argument := range field.Arguments {
		result.Arguments = append(result.Arguments, snapshotArgumentDefinition(argument))
	}
	return result
}

func snapshotArgumentDefinition(argument *gqlast.ArgumentDefinition) ArgumentDefinition {
	result := ArgumentDefinition{
		Name: argument.Name, Description: argument.Description, Type: snapshotTypeRef(argument.Type),
		Directives: snapshotDirectives(argument.Directives), Position: positionOf(argument.Position),
	}
	if argument.DefaultValue != nil {
		value := snapshotValue(argument.DefaultValue)
		result.DefaultValue = &value
	}
	return result
}

func snapshotDirectiveDefinition(definition *gqlast.DirectiveDefinition) DirectiveDefinition {
	result := DirectiveDefinition{
		Name: definition.Name, Description: definition.Description,
		Repeatable: definition.IsRepeatable, Position: positionOf(definition.Position),
	}
	for _, argument := range definition.Arguments {
		result.Arguments = append(result.Arguments, snapshotArgumentDefinition(argument))
	}
	for _, location := range definition.Locations {
		result.Locations = append(result.Locations, string(location))
	}
	return result
}

func snapshotDirectives(directives gqlast.DirectiveList) []DirectiveUse {
	result := make([]DirectiveUse, 0, len(directives))
	for _, directive := range directives {
		use := DirectiveUse{Name: directive.Name, Position: positionOf(directive.Position)}
		for _, argument := range directive.Arguments {
			use.Arguments = append(use.Arguments, DirectiveArgument{
				Name: argument.Name, Value: snapshotValue(argument.Value), Position: positionOf(argument.Position),
			})
		}
		result = append(result, use)
	}
	return result
}

func snapshotTypeRef(reference *gqlast.Type) TypeRef {
	if reference == nil {
		return TypeRef{}
	}
	result := TypeRef{Named: reference.NamedType, NonNull: reference.NonNull}
	if reference.Elem != nil {
		element := snapshotTypeRef(reference.Elem)
		result.Element = &element
	}
	return result
}

func snapshotValue(value *gqlast.Value) Value {
	if value == nil {
		return Value{}
	}
	result := Value{Kind: snapshotValueKind(value.Kind), Raw: value.Raw, Position: positionOf(value.Position)}
	for _, child := range value.Children {
		result.Children = append(result.Children, ValueChild{Name: child.Name, Value: snapshotValue(child.Value)})
	}
	return result
}

func snapshotValueKind(kind gqlast.ValueKind) ValueKind {
	switch kind {
	case gqlast.Variable:
		return ValueVariable
	case gqlast.IntValue:
		return ValueInt
	case gqlast.FloatValue:
		return ValueFloat
	case gqlast.StringValue, gqlast.BlockValue:
		return ValueString
	case gqlast.BooleanValue:
		return ValueBoolean
	case gqlast.NullValue:
		return ValueNull
	case gqlast.EnumValue:
		return ValueEnum
	case gqlast.ListValue:
		return ValueList
	case gqlast.ObjectValue:
		return ValueObject
	default:
		return ValueKind("UNKNOWN")
	}
}

func isBuiltInPosition(position *gqlast.Position) bool {
	return position != nil && position.Src != nil && position.Src.BuiltIn
}

func cloneSnapshot(source SchemaSnapshot) SchemaSnapshot {
	result := source
	result.SchemaDirectives = cloneDirectives(source.SchemaDirectives)
	result.Types = make([]TypeDefinition, len(source.Types))
	for index := range source.Types {
		result.Types[index] = cloneTypeDefinition(source.Types[index])
	}
	result.Directives = make([]DirectiveDefinition, len(source.Directives))
	for index := range source.Directives {
		result.Directives[index] = cloneDirectiveDefinition(source.Directives[index])
	}
	return result
}

func isConduitDirective(name string) bool {
	_, ok := conduitDirectiveNames[name]
	return ok
}

func cloneTypeDefinition(source TypeDefinition) TypeDefinition {
	result := source
	result.Interfaces = append([]string(nil), source.Interfaces...)
	result.UnionTypes = append([]string(nil), source.UnionTypes...)
	result.Directives = cloneDirectives(source.Directives)
	result.Fields = make([]FieldDefinition, len(source.Fields))
	for index := range source.Fields {
		result.Fields[index] = cloneField(source.Fields[index])
	}
	result.EnumValues = make([]EnumValueDefinition, len(source.EnumValues))
	for index := range source.EnumValues {
		result.EnumValues[index] = source.EnumValues[index]
		result.EnumValues[index].Directives = cloneDirectives(source.EnumValues[index].Directives)
	}
	return result
}

func cloneField(source FieldDefinition) FieldDefinition {
	result := source
	result.Type = cloneTypeRef(source.Type)
	result.DefaultValue = cloneValuePointer(source.DefaultValue)
	result.Directives = cloneDirectives(source.Directives)
	result.Arguments = make([]ArgumentDefinition, len(source.Arguments))
	for index := range source.Arguments {
		result.Arguments[index] = cloneArgument(source.Arguments[index])
	}
	return result
}

func cloneArgument(source ArgumentDefinition) ArgumentDefinition {
	result := source
	result.Type = cloneTypeRef(source.Type)
	result.DefaultValue = cloneValuePointer(source.DefaultValue)
	result.Directives = cloneDirectives(source.Directives)
	return result
}

func cloneDirectiveDefinition(source DirectiveDefinition) DirectiveDefinition {
	result := source
	result.Locations = append([]string(nil), source.Locations...)
	result.Arguments = make([]ArgumentDefinition, len(source.Arguments))
	for index := range source.Arguments {
		result.Arguments[index] = cloneArgument(source.Arguments[index])
	}
	return result
}

func cloneDirectives(source []DirectiveUse) []DirectiveUse {
	result := make([]DirectiveUse, len(source))
	for index := range source {
		result[index] = source[index]
		result[index].Arguments = make([]DirectiveArgument, len(source[index].Arguments))
		for argumentIndex := range source[index].Arguments {
			result[index].Arguments[argumentIndex] = source[index].Arguments[argumentIndex]
			result[index].Arguments[argumentIndex].Value = cloneValue(source[index].Arguments[argumentIndex].Value)
		}
	}
	return result
}

func cloneTypeRef(source TypeRef) TypeRef {
	result := source
	if source.Element != nil {
		element := cloneTypeRef(*source.Element)
		result.Element = &element
	}
	return result
}

func cloneValuePointer(source *Value) *Value {
	if source == nil {
		return nil
	}
	result := cloneValue(*source)
	return &result
}

func cloneValue(source Value) Value {
	result := source
	result.Children = make([]ValueChild, len(source.Children))
	for index := range source.Children {
		result.Children[index] = ValueChild{Name: source.Children[index].Name, Value: cloneValue(source.Children[index].Value)}
	}
	return result
}
