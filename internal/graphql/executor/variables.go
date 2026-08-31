package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/Zachshotamartin/conduit/internal/datasource"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
)

func (executor *Executor) coerceVariables(
	raw json.RawMessage,
	definitions []graphqlast.VariableDefinition,
) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage("{}")
	}
	canonical, err := datasource.NewArgumentValues(raw)
	if err != nil {
		return nil, fmt.Errorf("variables must be one strict JSON object: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical.CanonicalJSON()))
	decoder.UseNumber()
	provided := make(map[string]any)
	if err := decoder.Decode(&provided); err != nil {
		return nil, fmt.Errorf("decode variables: %w", err)
	}

	definitionByName := make(map[string]graphqlast.VariableDefinition, len(definitions))
	for _, definition := range definitions {
		definitionByName[definition.Name] = definition
	}
	unknown := make([]string, 0)
	for name := range provided {
		if _, ok := definitionByName[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown variable $%s", unknown[0])
	}

	coerced := make(map[string]any, len(definitions))
	for _, definition := range definitions {
		value, present := provided[definition.Name]
		if !present && definition.DefaultValue != nil {
			value, present, err = evaluateValue(*definition.DefaultValue, nil)
			if err != nil {
				return nil, fmt.Errorf("variable $%s default: %w", definition.Name, err)
			}
		}
		value, present, err = executor.coerceInput(definition.Type, value, present, "$"+definition.Name)
		if err != nil {
			return nil, err
		}
		if present {
			coerced[definition.Name] = value
		}
	}
	return coerced, nil
}

func (executor *Executor) coerceArguments(
	field graphqlast.ExecutableField,
	variables map[string]any,
) (datasource.ArgumentValues, error) {
	definition, ok := executor.index.fields[fieldCoordinate(field.ParentType, field.Name)]
	if !ok {
		return datasource.ArgumentValues{}, fmt.Errorf("missing schema field %s.%s", field.ParentType, field.Name)
	}
	authored := make(map[string]graphqlast.ExecutableArgument, len(field.Arguments))
	for _, argument := range field.Arguments {
		authored[argument.Name] = argument
	}
	values := make(map[string]any, len(definition.Arguments))
	for _, argumentDefinition := range definition.Arguments {
		argument, supplied := authored[argumentDefinition.Name]
		var value any
		present := false
		var err error
		if supplied {
			value, present, err = evaluateValue(argument.Value, variables)
			if err != nil {
				return datasource.ArgumentValues{}, fmt.Errorf("argument %s: %w", argumentDefinition.Name, err)
			}
		}
		if !present && argumentDefinition.DefaultValue != nil {
			value, present, err = evaluateValue(*argumentDefinition.DefaultValue, nil)
			if err != nil {
				return datasource.ArgumentValues{}, fmt.Errorf("argument %s default: %w", argumentDefinition.Name, err)
			}
		}
		value, present, err = executor.coerceInput(
			argumentDefinition.Type,
			value,
			present,
			field.ParentType+"."+field.Name+"("+argumentDefinition.Name+")",
		)
		if err != nil {
			return datasource.ArgumentValues{}, err
		}
		if present {
			values[argumentDefinition.Name] = value
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return datasource.ArgumentValues{}, fmt.Errorf("encode coerced arguments: %w", err)
	}
	arguments, err := datasource.NewArgumentValues(encoded)
	if err != nil {
		return datasource.ArgumentValues{}, fmt.Errorf("canonicalize coerced arguments: %w", err)
	}
	return arguments, nil
}

func (executor *Executor) coerceInput(
	reference graphqlast.TypeRef,
	value any,
	present bool,
	path string,
) (any, bool, error) {
	if !present {
		if reference.NonNull {
			return nil, false, fmt.Errorf("%s must be provided", path)
		}
		return nil, false, nil
	}
	if value == nil {
		if reference.NonNull {
			return nil, false, fmt.Errorf("%s cannot be null", path)
		}
		return nil, true, nil
	}
	if reference.Element != nil {
		values, list := value.([]any)
		if !list {
			values = []any{value}
		}
		coerced := make([]any, len(values))
		for index, element := range values {
			item, _, err := executor.coerceInput(
				*reference.Element, element, true, fmt.Sprintf("%s[%d]", path, index),
			)
			if err != nil {
				return nil, false, err
			}
			coerced[index] = item
		}
		return coerced, true, nil
	}

	if isBuiltInScalar(reference.Named) {
		coerced, err := coerceScalarInput(reference.Named, value)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", path, err)
		}
		return coerced, true, nil
	}
	definition, exists := executor.index.types[reference.Named]
	if !exists {
		return nil, false, fmt.Errorf("%s references unknown input type %s", path, reference.Named)
	}
	switch definition.Kind {
	case "SCALAR":
		coerced, err := coerceScalarInput(reference.Named, value)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", path, err)
		}
		return coerced, true, nil
	case "ENUM":
		name, ok := value.(string)
		if !ok || !enumContains(definition, name) {
			return nil, false, fmt.Errorf("%s must be a %s enum value", path, reference.Named)
		}
		return name, true, nil
	case "INPUT_OBJECT":
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("%s must be an input object", path)
		}
		knownFields := make(map[string]graphqlast.FieldDefinition, len(definition.Fields))
		for _, field := range definition.Fields {
			knownFields[field.Name] = field
		}
		unknown := make([]string, 0)
		for name := range object {
			if _, known := knownFields[name]; !known {
				unknown = append(unknown, name)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			return nil, false, fmt.Errorf("%s.%s is unknown", path, unknown[0])
		}
		result := make(map[string]any, len(definition.Fields))
		for _, field := range definition.Fields {
			fieldValue, fieldPresent := object[field.Name]
			var err error
			if !fieldPresent && field.DefaultValue != nil {
				fieldValue, fieldPresent, err = evaluateValue(*field.DefaultValue, nil)
				if err != nil {
					return nil, false, fmt.Errorf("%s.%s default: %w", path, field.Name, err)
				}
			}
			fieldValue, fieldPresent, err = executor.coerceInput(
				field.Type, fieldValue, fieldPresent, path+"."+field.Name,
			)
			if err != nil {
				return nil, false, err
			}
			if fieldPresent {
				result[field.Name] = fieldValue
			}
		}
		return result, true, nil
	default:
		return nil, false, fmt.Errorf("%s must use an input type", path)
	}
}

func evaluateValue(value graphqlast.Value, variables map[string]any) (any, bool, error) {
	switch value.Kind {
	case graphqlast.ValueVariable:
		if variables == nil {
			return nil, false, nil
		}
		result, ok := variables[value.Raw]
		return result, ok, nil
	case graphqlast.ValueInt:
		integer, err := strconv.ParseInt(value.Raw, 10, 64)
		return integer, true, err
	case graphqlast.ValueFloat:
		floating, err := strconv.ParseFloat(value.Raw, 64)
		return floating, true, err
	case graphqlast.ValueString, graphqlast.ValueEnum:
		return value.Raw, true, nil
	case graphqlast.ValueBoolean:
		boolean, err := strconv.ParseBool(value.Raw)
		return boolean, true, err
	case graphqlast.ValueNull:
		return nil, true, nil
	case graphqlast.ValueList:
		result := make([]any, len(value.Children))
		for index, child := range value.Children {
			item, present, err := evaluateValue(child.Value, variables)
			if err != nil {
				return nil, false, err
			}
			if !present {
				return nil, false, nil
			}
			result[index] = item
		}
		return result, true, nil
	case graphqlast.ValueObject:
		result := make(map[string]any, len(value.Children))
		for _, child := range value.Children {
			item, present, err := evaluateValue(child.Value, variables)
			if err != nil {
				return nil, false, err
			}
			if present {
				result[child.Name] = item
			}
		}
		return result, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported value kind %s", value.Kind)
	}
}

func coerceScalarInput(name string, value any) (any, error) {
	switch name {
	case "Int":
		integer, ok := integerValue(value)
		if !ok || integer < math.MinInt32 || integer > math.MaxInt32 {
			return nil, fmt.Errorf("must be a signed 32-bit integer")
		}
		return integer, nil
	case "Float":
		switch typed := value.(type) {
		case json.Number:
			floating, err := typed.Float64()
			if err != nil || math.IsInf(floating, 0) || math.IsNaN(floating) {
				return nil, fmt.Errorf("must be a finite number")
			}
			return floating, nil
		case int64:
			return float64(typed), nil
		case float64:
			if math.IsInf(typed, 0) || math.IsNaN(typed) {
				return nil, fmt.Errorf("must be a finite number")
			}
			return typed, nil
		default:
			return nil, fmt.Errorf("must be a number")
		}
	case "String":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("must be a string")
		}
		return text, nil
	case "Boolean":
		boolean, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("must be a boolean")
		}
		return boolean, nil
	case "ID":
		switch typed := value.(type) {
		case string:
			return typed, nil
		case json.Number:
			integer, err := typed.Int64()
			if err != nil {
				return nil, fmt.Errorf("must be a string or integer ID")
			}
			return strconv.FormatInt(integer, 10), nil
		case int64:
			return strconv.FormatInt(typed, 10), nil
		default:
			return nil, fmt.Errorf("must be a string or integer ID")
		}
	default:
		return value, nil
	}
}

func integerValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		integer, err := typed.Int64()
		return integer, err == nil
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}

func enumContains(definition graphqlast.TypeDefinition, value string) bool {
	for _, candidate := range definition.EnumValues {
		if candidate.Name == value {
			return true
		}
	}
	return false
}

func isBuiltInScalar(name string) bool {
	switch name {
	case "Int", "Float", "String", "Boolean", "ID":
		return true
	default:
		return false
	}
}

func shouldInclude(directives []graphqlast.DirectiveUse, variables map[string]any) (bool, error) {
	for _, directive := range directives {
		if directive.Name != "skip" && directive.Name != "include" {
			continue
		}
		var conditionValue graphqlast.Value
		found := false
		for _, argument := range directive.Arguments {
			if argument.Name == "if" {
				conditionValue = argument.Value
				found = true
				break
			}
		}
		if !found {
			return false, fmt.Errorf("@%s requires if", directive.Name)
		}
		value, present, err := evaluateValue(conditionValue, variables)
		if err != nil || !present {
			return false, fmt.Errorf("@%s condition is unavailable", directive.Name)
		}
		condition, ok := value.(bool)
		if !ok {
			return false, fmt.Errorf("@%s condition must be boolean", directive.Name)
		}
		if directive.Name == "skip" && condition {
			return false, nil
		}
		if directive.Name == "include" && !condition {
			return false, nil
		}
	}
	return true, nil
}
