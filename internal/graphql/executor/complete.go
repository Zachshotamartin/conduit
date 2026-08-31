package executor

import (
	"context"
	"encoding/json"
	"math"
	"strconv"

	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
)

func (executor *Executor) completeValue(
	ctx context.Context,
	state executionState,
	reference graphqlast.TypeRef,
	value any,
	selectionSet []graphqlast.ExecutableSelection,
	path []any,
	position graphqlast.SourcePosition,
) fieldResult {
	if value == nil {
		if reference.NonNull {
			return fieldResult{
				value:  nil,
				errors: []Error{newExecutionError(conduiterrors.SourceInvalidResponse, path, position)},
				bubble: true,
			}
		}
		return fieldResult{value: nil}
	}
	if reference.Element != nil {
		items, ok := value.([]any)
		if !ok {
			return invalidCompletedValue(reference, path, position)
		}
		completed := make([]any, len(items))
		var failures []Error
		for index, item := range items {
			result := executor.completeValue(
				ctx, state, *reference.Element, item, selectionSet, appendPath(path, index), position,
			)
			failures = append(failures, result.errors...)
			if result.bubble {
				return fieldResult{value: nil, errors: failures, bubble: reference.NonNull}
			}
			completed[index] = result.value
		}
		return fieldResult{value: completed, errors: failures}
	}

	if isBuiltInScalar(reference.Named) {
		completed, ok := completeScalar(reference.Named, value)
		if !ok {
			return invalidCompletedValue(reference, path, position)
		}
		return fieldResult{value: completed}
	}
	definition, exists := executor.index.types[reference.Named]
	if !exists {
		return fieldResult{
			value: nil, errors: []Error{newExecutionError(conduiterrors.InternalInvariant, path, position)},
			bubble: reference.NonNull,
		}
	}
	switch definition.Kind {
	case "SCALAR":
		completed, ok := completeScalar(reference.Named, value)
		if !ok {
			return invalidCompletedValue(reference, path, position)
		}
		return fieldResult{value: completed}
	case "ENUM":
		name, ok := value.(string)
		if !ok || !enumContains(definition, name) {
			return invalidCompletedValue(reference, path, position)
		}
		return fieldResult{value: name}
	case "OBJECT", "INTERFACE", "UNION":
		object, ok := value.(map[string]any)
		if !ok {
			return invalidCompletedValue(reference, path, position)
		}
		runtimeType, ok := executor.runtimeObjectType(definition, object)
		if !ok {
			return invalidCompletedValue(reference, path, position)
		}
		completed, failures, bubble := executor.executeSelectionSet(
			ctx, state, runtimeType, object, selectionSet, path, false,
		)
		if bubble {
			return fieldResult{value: nil, errors: failures, bubble: reference.NonNull}
		}
		return fieldResult{value: completed, errors: failures}
	default:
		return invalidCompletedValue(reference, path, position)
	}
}

func invalidCompletedValue(
	reference graphqlast.TypeRef,
	path []any,
	position graphqlast.SourcePosition,
) fieldResult {
	return fieldResult{
		value:  nil,
		errors: []Error{newExecutionError(conduiterrors.SourceInvalidResponse, path, position)},
		bubble: reference.NonNull,
	}
}

func completeScalar(name string, value any) (any, bool) {
	switch name {
	case "String":
		text, ok := value.(string)
		return text, ok
	case "Boolean":
		boolean, ok := value.(bool)
		return boolean, ok
	case "Int":
		integer, ok := integerValue(value)
		if !ok || integer < math.MinInt32 || integer > math.MaxInt32 {
			return nil, false
		}
		return integer, true
	case "Float":
		switch typed := value.(type) {
		case json.Number:
			floating, err := typed.Float64()
			if err != nil || math.IsNaN(floating) || math.IsInf(floating, 0) {
				return nil, false
			}
			return typed, true
		case float64:
			if math.IsNaN(typed) || math.IsInf(typed, 0) {
				return nil, false
			}
			return typed, true
		case int64:
			return typed, true
		default:
			return nil, false
		}
	case "ID":
		switch typed := value.(type) {
		case string:
			return typed, true
		case json.Number:
			integer, err := typed.Int64()
			if err != nil {
				return nil, false
			}
			return strconv.FormatInt(integer, 10), true
		case int64:
			return strconv.FormatInt(typed, 10), true
		default:
			return nil, false
		}
	default:
		switch value.(type) {
		case nil, map[string]any, []any:
			return nil, false
		default:
			return value, true
		}
	}
}

func (executor *Executor) runtimeObjectType(
	definition graphqlast.TypeDefinition,
	object map[string]any,
) (string, bool) {
	if definition.Kind == "OBJECT" {
		if authored, exists := object["__typename"]; exists {
			name, ok := authored.(string)
			if !ok || name != definition.Name {
				return "", false
			}
		}
		return definition.Name, true
	}
	typename, ok := object["__typename"].(string)
	if !ok || typename == "" {
		return "", false
	}
	concrete, exists := executor.index.types[typename]
	if !exists || concrete.Kind != "OBJECT" || !executor.typeConditionMatches(definition.Name, typename) {
		return "", false
	}
	return typename, true
}
