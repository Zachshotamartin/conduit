package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"

	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
)

type orderedField struct {
	name  string
	value any
}

type orderedObject struct {
	fields []orderedField
}

func (object orderedObject) MarshalJSON() ([]byte, error) {
	output := []byte{'{'}
	for index, field := range object.fields {
		if index > 0 {
			output = append(output, ',')
		}
		name, err := json.Marshal(field.name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(field.value)
		if err != nil {
			return nil, err
		}
		output = append(output, name...)
		output = append(output, ':')
		output = append(output, value...)
	}
	output = append(output, '}')
	return output, nil
}

func marshalOutput(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return encoded
}

func decodeSourceJSON(input []byte) (any, error) {
	if len(input) == 0 || !utf8.Valid(input) {
		return nil, fmt.Errorf("source JSON must be nonempty valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	value, err := decodeStrictSourceValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if token, trailingErr := decoder.Token(); trailingErr != io.EOF {
		if trailingErr == nil {
			return nil, fmt.Errorf("source JSON contains trailing token %v", token)
		}
		return nil, trailingErr
	}
	return value, nil
}

func decodeStrictSourceValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 128 {
		return nil, fmt.Errorf("source JSON nesting exceeds 128")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch typed := token.(type) {
	case nil, bool, string, json.Number:
		return typed, nil
	case json.Delim:
		switch typed {
		case '[':
			values := make([]any, 0)
			for decoder.More() {
				value, valueErr := decodeStrictSourceValue(decoder, depth+1)
				if valueErr != nil {
					return nil, valueErr
				}
				values = append(values, value)
			}
			closeToken, closeErr := decoder.Token()
			if closeErr != nil || closeToken != json.Delim(']') {
				return nil, fmt.Errorf("source JSON array is unterminated")
			}
			return values, nil
		case '{':
			object := make(map[string]any)
			for decoder.More() {
				nameToken, nameErr := decoder.Token()
				if nameErr != nil {
					return nil, nameErr
				}
				name, ok := nameToken.(string)
				if !ok {
					return nil, fmt.Errorf("source JSON object key must be a string")
				}
				if _, duplicate := object[name]; duplicate {
					return nil, fmt.Errorf("source JSON object key %q is duplicated", name)
				}
				value, valueErr := decodeStrictSourceValue(decoder, depth+1)
				if valueErr != nil {
					return nil, valueErr
				}
				object[name] = value
			}
			closeToken, closeErr := decoder.Token()
			if closeErr != nil || closeToken != json.Delim('}') {
				return nil, fmt.Errorf("source JSON object is unterminated")
			}
			return object, nil
		}
	}
	return nil, fmt.Errorf("source JSON contains unsupported token %v", token)
}

func canonicalParent(value any) ([]byte, error) {
	return json.Marshal(value)
}

func appendPath(path []any, segment any) []any {
	result := make([]any, len(path)+1)
	copy(result, path)
	result[len(path)] = segment
	return result
}

func newExecutionError(category conduiterrors.Category, path []any) Error {
	classified := conduiterrors.New(category)
	return Error{
		Message: classified.SafeMessage(),
		Path:    append([]any(nil), path...),
		Code:    classified.Category(),
	}
}
