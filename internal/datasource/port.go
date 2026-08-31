package datasource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxTenantIDBytes = 64
	maxJSONDepth     = 128
)

// DataSource is the only resolver-facing adapter contract. Implementations
// must not retain requests or caller-owned slices after Resolve returns.
type DataSource interface {
	Name() string
	Resolve(ctx context.Context, request *SourceRequest) (*SourceResponse, error)
	HealthCheck(ctx context.Context) error
	Close(ctx context.Context) error
}

// FieldRef is a canonical, comparable GraphQL field coordinate.
type FieldRef struct {
	parentType string
	field      string
}

// NewFieldRef validates and constructs a field coordinate.
func NewFieldRef(parentType, field string) (FieldRef, error) {
	if !validGraphQLName(parentType) || !validGraphQLName(field) ||
		strings.HasPrefix(parentType, "__") || strings.HasPrefix(field, "__") {
		return FieldRef{}, errors.New("field coordinate must contain two non-introspection GraphQL names")
	}
	return FieldRef{parentType: parentType, field: field}, nil
}

// ParseFieldRef parses the canonical Type.field coordinate form.
func ParseFieldRef(coordinate string) (FieldRef, error) {
	if strings.Count(coordinate, ".") != 1 {
		return FieldRef{}, errors.New("field coordinate must contain exactly one dot")
	}
	parentType, field, _ := strings.Cut(coordinate, ".")
	return NewFieldRef(parentType, field)
}

// ParentType returns the GraphQL parent type name.
func (field FieldRef) ParentType() string { return field.parentType }

// Field returns the GraphQL field name.
func (field FieldRef) Field() string { return field.field }

// String returns the canonical Type.field form or an empty string for the
// invalid zero value.
func (field FieldRef) String() string {
	if !field.Valid() {
		return ""
	}
	return field.parentType + "." + field.field
}

// Valid reports whether the coordinate satisfies all construction rules.
func (field FieldRef) Valid() bool {
	return validGraphQLName(field.parentType) && validGraphQLName(field.field) &&
		!strings.HasPrefix(field.parentType, "__") && !strings.HasPrefix(field.field, "__")
}

func validGraphQLName(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if index == 0 {
			if character != '_' && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
				return false
			}
			continue
		}
		if character != '_' && (character < 'A' || character > 'Z') &&
			(character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// TenantID is an exact, bounded tenant identity. It deliberately performs no
// trimming, folding, or subject encoding.
type TenantID struct {
	value string
}

// NewTenantID validates a tenant identifier by encoded byte length.
func NewTenantID(value string) (TenantID, error) {
	if value == "" || len(value) > maxTenantIDBytes || !utf8.ValidString(value) {
		return TenantID{}, fmt.Errorf("tenant ID must be valid UTF-8 containing 1 to %d bytes", maxTenantIDBytes)
	}
	return TenantID{value: value}, nil
}

// DefaultTenantID returns the non-tenanted-mode identity.
func DefaultTenantID() TenantID { return TenantID{value: "default"} }

// String returns the exact tenant identity.
func (tenant TenantID) String() string { return tenant.value }

// Valid reports whether this value could have been constructed.
func (tenant TenantID) Valid() bool {
	return tenant.value != "" && len(tenant.value) <= maxTenantIDBytes && utf8.ValidString(tenant.value)
}

// MarshalText implements encoding.TextMarshaler without permitting an invalid
// zero value to escape.
func (tenant TenantID) MarshalText() ([]byte, error) {
	if !tenant.Valid() {
		return nil, errors.New("cannot marshal invalid tenant ID")
	}
	return []byte(tenant.value), nil
}

type jsonSpan struct {
	start int
	end   int
}

// ArgumentValues is a strict, canonical JSON object. Its zero value is the
// empty object, and no returned byte slice aliases its internal representation.
type ArgumentValues struct {
	canonical string
	index     map[string]jsonSpan
}

// NewArgumentValues validates exactly one JSON object, rejects duplicate keys
// at every depth, and canonicalizes object-member order.
func NewArgumentValues(input json.RawMessage) (ArgumentValues, error) {
	if len(input) == 0 || !utf8.Valid(input) {
		return ArgumentValues{}, errors.New("argument values must be one valid UTF-8 JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	value, err := decodeJSONValue(decoder, 0)
	if err != nil {
		return ArgumentValues{}, fmt.Errorf("decode argument values: %w", err)
	}
	if value.kind != jsonObject {
		return ArgumentValues{}, errors.New("argument values root must be a JSON object")
	}
	if token, trailingErr := decoder.Token(); trailingErr != io.EOF {
		if trailingErr == nil {
			return ArgumentValues{}, fmt.Errorf("argument values contain trailing JSON token %v", token)
		}
		return ArgumentValues{}, fmt.Errorf("decode trailing argument JSON: %w", trailingErr)
	}

	var output strings.Builder
	index := make(map[string]jsonSpan, len(value.object))
	encodeJSONObject(&output, value.object, index)
	return ArgumentValues{canonical: output.String(), index: index}, nil
}

// EmptyArgumentValues returns the canonical empty object.
func EmptyArgumentValues() ArgumentValues { return ArgumentValues{} }

// Len returns the number of top-level argument names.
func (arguments ArgumentValues) Len() int { return len(arguments.index) }

// LookupJSON returns a defensive copy of one canonical JSON value. Its boolean
// distinguishes an absent name from an explicit JSON null.
func (arguments ArgumentValues) LookupJSON(name string) (json.RawMessage, bool) {
	span, ok := arguments.index[name]
	if !ok {
		return nil, false
	}
	canonical := arguments.canonicalString()
	return append(json.RawMessage(nil), canonical[span.start:span.end]...), true
}

// CanonicalJSON returns a defensive copy of the complete object.
func (arguments ArgumentValues) CanonicalJSON() json.RawMessage {
	return append(json.RawMessage(nil), arguments.canonicalString()...)
}

// Equal compares canonical semantic argument values.
func (arguments ArgumentValues) Equal(other ArgumentValues) bool {
	return arguments.canonicalString() == other.canonicalString()
}

// MarshalJSON implements json.Marshaler.
func (arguments ArgumentValues) MarshalJSON() ([]byte, error) {
	return arguments.CanonicalJSON(), nil
}

func (arguments ArgumentValues) canonicalString() string {
	if arguments.canonical == "" {
		return "{}"
	}
	return arguments.canonical
}

type jsonKind uint8

const (
	jsonNull jsonKind = iota + 1
	jsonBoolean
	jsonNumber
	jsonString
	jsonArray
	jsonObject
)

type strictJSONValue struct {
	kind    jsonKind
	raw     string
	boolean bool
	array   []strictJSONValue
	object  []strictJSONMember
}

type strictJSONMember struct {
	name  string
	value strictJSONValue
}

func decodeJSONValue(decoder *json.Decoder, depth int) (strictJSONValue, error) {
	if depth > maxJSONDepth {
		return strictJSONValue{}, fmt.Errorf("JSON nesting exceeds %d", maxJSONDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return strictJSONValue{}, err
	}
	switch typed := token.(type) {
	case nil:
		return strictJSONValue{kind: jsonNull}, nil
	case bool:
		return strictJSONValue{kind: jsonBoolean, boolean: typed}, nil
	case json.Number:
		return strictJSONValue{kind: jsonNumber, raw: typed.String()}, nil
	case string:
		return strictJSONValue{kind: jsonString, raw: typed}, nil
	case json.Delim:
		switch typed {
		case '[':
			var values []strictJSONValue
			for decoder.More() {
				value, valueErr := decodeJSONValue(decoder, depth+1)
				if valueErr != nil {
					return strictJSONValue{}, valueErr
				}
				values = append(values, value)
			}
			if closeToken, closeErr := decoder.Token(); closeErr != nil || closeToken != json.Delim(']') {
				return strictJSONValue{}, errors.New("unterminated JSON array")
			}
			return strictJSONValue{kind: jsonArray, array: values}, nil
		case '{':
			seen := make(map[string]struct{})
			var members []strictJSONMember
			for decoder.More() {
				nameToken, nameErr := decoder.Token()
				if nameErr != nil {
					return strictJSONValue{}, nameErr
				}
				name, ok := nameToken.(string)
				if !ok {
					return strictJSONValue{}, errors.New("JSON object member name must be a string")
				}
				if _, duplicate := seen[name]; duplicate {
					return strictJSONValue{}, fmt.Errorf("duplicate JSON object member %q", name)
				}
				seen[name] = struct{}{}
				value, valueErr := decodeJSONValue(decoder, depth+1)
				if valueErr != nil {
					return strictJSONValue{}, valueErr
				}
				members = append(members, strictJSONMember{name: name, value: value})
			}
			if closeToken, closeErr := decoder.Token(); closeErr != nil || closeToken != json.Delim('}') {
				return strictJSONValue{}, errors.New("unterminated JSON object")
			}
			return strictJSONValue{kind: jsonObject, object: members}, nil
		}
	}
	return strictJSONValue{}, fmt.Errorf("unsupported JSON token %v", token)
}

func encodeJSONObject(output *strings.Builder, members []strictJSONMember, index map[string]jsonSpan) {
	sort.Slice(members, func(i, j int) bool { return members[i].name < members[j].name })
	output.WriteByte('{')
	for memberIndex, member := range members {
		if memberIndex > 0 {
			output.WriteByte(',')
		}
		writeJSONString(output, member.name)
		output.WriteByte(':')
		start := output.Len()
		encodeJSONValue(output, member.value)
		index[member.name] = jsonSpan{start: start, end: output.Len()}
	}
	output.WriteByte('}')
}

func encodeJSONValue(output *strings.Builder, value strictJSONValue) {
	switch value.kind {
	case jsonNull:
		output.WriteString("null")
	case jsonBoolean:
		if value.boolean {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case jsonNumber:
		output.WriteString(value.raw)
	case jsonString:
		writeJSONString(output, value.raw)
	case jsonArray:
		output.WriteByte('[')
		for index, element := range value.array {
			if index > 0 {
				output.WriteByte(',')
			}
			encodeJSONValue(output, element)
		}
		output.WriteByte(']')
	case jsonObject:
		encodeJSONObject(output, value.object, make(map[string]jsonSpan))
	}
}

func writeJSONString(output *strings.Builder, value string) {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic("datasource: JSON string encoding failed: " + err.Error())
	}
	output.WriteString(string(encoded))
}

// PrincipalView is the least-privilege identity forwarded to a source.
type PrincipalView struct {
	subject string
	tenant  TenantID
	scopes  []string
}

// NewPrincipalView validates, copies, sorts, and deduplicates its inputs.
func NewPrincipalView(subject string, tenant TenantID, scopes []string) (PrincipalView, error) {
	if subject == "" || !utf8.ValidString(subject) {
		return PrincipalView{}, errors.New("principal subject must be nonempty valid UTF-8")
	}
	if !tenant.Valid() {
		return PrincipalView{}, errors.New("principal tenant must be valid")
	}
	canonicalScopes := append([]string(nil), scopes...)
	for _, scope := range canonicalScopes {
		if scope == "" || !utf8.ValidString(scope) {
			return PrincipalView{}, errors.New("principal scopes must be nonempty valid UTF-8")
		}
	}
	sort.Strings(canonicalScopes)
	canonicalScopes = compactStrings(canonicalScopes)
	return PrincipalView{subject: subject, tenant: tenant, scopes: canonicalScopes}, nil
}

// Subject returns the principal subject.
func (principal PrincipalView) Subject() string { return principal.subject }

// Tenant returns the principal tenant.
func (principal PrincipalView) Tenant() TenantID { return principal.tenant }

// Scopes returns a defensive sorted scope snapshot.
func (principal PrincipalView) Scopes() []string {
	return append([]string(nil), principal.scopes...)
}

// MarshalJSON emits only the deliberately narrowed principal surface.
func (principal PrincipalView) MarshalJSON() ([]byte, error) {
	if principal.subject == "" || !principal.tenant.Valid() {
		return nil, errors.New("cannot marshal invalid principal view")
	}
	scopes := principal.scopes
	if scopes == nil {
		scopes = []string{}
	}
	return json.Marshal(struct {
		Subject string   `json:"subject"`
		Tenant  string   `json:"tenant"`
		Scopes  []string `json:"scopes"`
	}{Subject: principal.subject, Tenant: principal.tenant.String(), Scopes: scopes})
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

// SourceRequest is a single field-resolution request. Parent nil identifies a
// query or mutation root; non-nil Parent is canonical immediate-parent JSON.
type SourceRequest struct {
	Field     FieldRef
	Tenant    TenantID
	Args      ArgumentValues
	Parent    []byte
	Principal PrincipalView
	Deadline  time.Time
}

// SourceResponse is untrusted source JSON plus tracing-only source duration.
type SourceResponse struct {
	Data       []byte
	SourceTime time.Duration
}
