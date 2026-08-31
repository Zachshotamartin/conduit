package ast

import gqlast "github.com/vektah/gqlparser/v2/ast"

// Snapshot returns a fully defensive parser-neutral operation document.
func (operation *Operation) Snapshot() OperationSnapshot {
	if operation == nil || operation.document == nil {
		return OperationSnapshot{}
	}
	return snapshotOperationDocument(operation.document)
}

func snapshotOperationDocument(document *gqlast.QueryDocument) OperationSnapshot {
	result := OperationSnapshot{}
	for _, definition := range document.Operations {
		if definition == nil {
			continue
		}
		result.Operations = append(result.Operations, ExecutableOperation{
			Kind:         operationKind(definition.Operation),
			Name:         definition.Name,
			Variables:    snapshotVariables(definition.VariableDefinitions),
			Directives:   snapshotDirectives(definition.Directives),
			SelectionSet: snapshotExecutableSelections(definition.SelectionSet),
			Position:     positionOf(definition.Position),
		})
	}
	for _, definition := range document.Fragments {
		if definition == nil {
			continue
		}
		result.Fragments = append(result.Fragments, ExecutableFragment{
			Name:          definition.Name,
			TypeCondition: definition.TypeCondition,
			Variables:     snapshotVariables(definition.VariableDefinition),
			Directives:    snapshotDirectives(definition.Directives),
			SelectionSet:  snapshotExecutableSelections(definition.SelectionSet),
			Position:      positionOf(definition.Position),
		})
	}
	return result
}

func snapshotVariables(definitions gqlast.VariableDefinitionList) []VariableDefinition {
	result := make([]VariableDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if definition == nil {
			continue
		}
		variable := VariableDefinition{
			Name:       definition.Variable,
			Type:       snapshotTypeRef(definition.Type),
			Directives: snapshotDirectives(definition.Directives),
			Position:   positionOf(definition.Position),
		}
		if definition.DefaultValue != nil {
			value := snapshotValue(definition.DefaultValue)
			variable.DefaultValue = &value
		}
		result = append(result, variable)
	}
	return result
}

func snapshotExecutableSelections(selections gqlast.SelectionSet) []ExecutableSelection {
	result := make([]ExecutableSelection, 0, len(selections))
	for _, selection := range selections {
		switch typed := selection.(type) {
		case *gqlast.Field:
			if typed == nil {
				continue
			}
			field := ExecutableField{
				Alias: typed.Alias, Name: typed.Name,
				Directives:   snapshotDirectives(typed.Directives),
				SelectionSet: snapshotExecutableSelections(typed.SelectionSet),
				Position:     positionOf(typed.Position),
			}
			if typed.ObjectDefinition != nil {
				field.ParentType = typed.ObjectDefinition.Name
			}
			if typed.Definition != nil {
				field.Type = snapshotTypeRef(typed.Definition.Type)
			}
			for _, argument := range typed.Arguments {
				if argument == nil || argument.Value == nil {
					continue
				}
				field.Arguments = append(field.Arguments, ExecutableArgument{
					Name: argument.Name, Value: snapshotValue(argument.Value),
					Position: positionOf(argument.Position),
				})
			}
			result = append(result, ExecutableSelection{
				Kind: SelectionField, Field: field, Position: field.Position,
			})
		case *gqlast.FragmentSpread:
			if typed == nil {
				continue
			}
			result = append(result, ExecutableSelection{
				Kind: SelectionFragmentSpread, FragmentName: typed.Name,
				Directives: snapshotDirectives(typed.Directives), Position: positionOf(typed.Position),
			})
		case *gqlast.InlineFragment:
			if typed == nil {
				continue
			}
			result = append(result, ExecutableSelection{
				Kind: SelectionInlineFragment, TypeCondition: typed.TypeCondition,
				Directives:   snapshotDirectives(typed.Directives),
				SelectionSet: snapshotExecutableSelections(typed.SelectionSet),
				Position:     positionOf(typed.Position),
			})
		}
	}
	return result
}

func operationKind(operation gqlast.Operation) OperationKind {
	switch operation {
	case gqlast.Query:
		return OperationQuery
	case gqlast.Mutation:
		return OperationMutation
	case gqlast.Subscription:
		return OperationSubscription
	default:
		return OperationKind(operation)
	}
}
