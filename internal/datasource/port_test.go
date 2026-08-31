package datasource_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Zachshotamartin/conduit/internal/datasource"
)

type fixtureSource struct{}

func (fixtureSource) Name() string { return "fixture" }
func (fixtureSource) Resolve(context.Context, *datasource.SourceRequest) (*datasource.SourceResponse, error) {
	return &datasource.SourceResponse{Data: []byte(`{"ok":true}`)}, nil
}
func (fixtureSource) HealthCheck(context.Context) error { return nil }
func (fixtureSource) Close(context.Context) error       { return nil }

var _ datasource.DataSource = fixtureSource{}

func TestUNIT004_DataSourcePortHasTheFrozenRequestResponseShape(t *testing.T) {
	t.Parallel()
	field, err := datasource.ParseFieldRef("Query.viewer")
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := datasource.NewTenantID("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := datasource.NewArgumentValues(json.RawMessage(`{"z":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	principal, err := datasource.NewPrincipalView("subject-a", tenant, []string{"write", "read"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Unix(1_700_000_000, 0).UTC()
	request := &datasource.SourceRequest{
		Field: field, Tenant: tenant, Args: arguments, Parent: []byte(`{"id":"1"}`),
		Principal: principal, Deadline: deadline,
	}
	response, err := (fixtureSource{}).Resolve(context.Background(), request)
	if err != nil || string(response.Data) != `{"ok":true}` {
		t.Fatalf("Resolve() = (%s, %v)", response.Data, err)
	}
	if request.Field.String() != "Query.viewer" || request.Tenant.String() != "tenant-a" ||
		!request.Deadline.Equal(deadline) {
		t.Fatalf("request changed: %#v", request)
	}
}

func TestUNIT004_FieldRefIsCanonicalComparableAndRejectsMalformedCoordinates(t *testing.T) {
	t.Parallel()
	valid := []string{"Query.viewer", "_Type._field", "A0.b9"}
	for _, coordinate := range valid {
		field, err := datasource.ParseFieldRef(coordinate)
		if err != nil || !field.Valid() || field.String() != coordinate {
			t.Errorf("ParseFieldRef(%q) = (%q, %v, %v)", coordinate, field.String(), field.Valid(), err)
		}
		rebuilt, err := datasource.NewFieldRef(field.ParentType(), field.Field())
		if err != nil || rebuilt != field {
			t.Errorf("NewFieldRef(%q, %q) = (%v, %v), want comparable equality", field.ParentType(), field.Field(), rebuilt, err)
		}
	}

	invalid := []string{"", "Query", ".field", "Type.", "A.b.c", "9Type.field", "Type.9field", "__Type.field", "Type.__field", "Type.field\n"}
	for _, coordinate := range invalid {
		field, err := datasource.ParseFieldRef(coordinate)
		if err == nil || field.Valid() || field != (datasource.FieldRef{}) {
			t.Errorf("ParseFieldRef(%q) = (%v, %v), want zero and error", coordinate, field, err)
		}
	}
	if (datasource.FieldRef{}).Valid() || (datasource.FieldRef{}).String() != "" {
		t.Fatal("zero FieldRef is valid")
	}
}

func TestUNIT004_TenantIDUsesExactUTF8ByteBounds(t *testing.T) {
	t.Parallel()
	if got := datasource.DefaultTenantID(); !got.Valid() || got.String() != "default" {
		t.Fatalf("DefaultTenantID() = %q, valid=%v", got.String(), got.Valid())
	}
	for _, tc := range []struct {
		name  string
		value string
		valid bool
	}{
		{name: "empty", value: ""},
		{name: "one", value: "a", valid: true},
		{name: "sixty-four", value: string(makeBytes('a', 64)), valid: true},
		{name: "sixty-five", value: string(makeBytes('a', 65))},
		{name: "utf8 encoded bound", value: "é", valid: true},
		{name: "invalid utf8", value: string([]byte{0xff})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := datasource.NewTenantID(tc.value)
			if tc.valid {
				if err != nil || !got.Valid() || got.String() != tc.value {
					t.Fatalf("NewTenantID() = (%q, %v), valid=%v", got.String(), err, got.Valid())
				}
				encoded, err := got.MarshalText()
				if err != nil || string(encoded) != tc.value {
					t.Fatalf("MarshalText() = (%q, %v)", encoded, err)
				}
				return
			}
			if err == nil || got.Valid() || got != (datasource.TenantID{}) {
				t.Fatalf("NewTenantID() = (%q, %v), want zero error", got.String(), err)
			}
		})
	}
}

func TestUNIT004_ArgumentValuesAreStrictCanonicalAndDefensive(t *testing.T) {
	t.Parallel()
	input := json.RawMessage(` { "z": [3,{"b":2,"a":1}], "a": null } `)
	arguments, err := datasource.NewArgumentValues(input)
	if err != nil {
		t.Fatal(err)
	}
	input[3] = 'X'
	if arguments.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", arguments.Len())
	}
	if got := string(arguments.CanonicalJSON()); got != `{"a":null,"z":[3,{"a":1,"b":2}]}` {
		t.Fatalf("CanonicalJSON() = %s", got)
	}
	nullValue, found := arguments.LookupJSON("a")
	if !found || string(nullValue) != "null" {
		t.Fatalf("LookupJSON(a) = (%s, %v)", nullValue, found)
	}
	if value, found := arguments.LookupJSON("missing"); found || value != nil {
		t.Fatalf("LookupJSON(missing) = (%s, %v)", value, found)
	}
	returned := arguments.CanonicalJSON()
	returned[0] = '['
	if got := string(arguments.CanonicalJSON()); got[0] != '{' {
		t.Fatalf("returned canonical bytes alias internal state: %s", got)
	}
	marshaled, err := json.Marshal(arguments)
	if err != nil || string(marshaled) != string(arguments.CanonicalJSON()) {
		t.Fatalf("MarshalJSON() = (%s, %v)", marshaled, err)
	}
	equal, err := datasource.NewArgumentValues(json.RawMessage(`{"a":null,"z":[3,{"a":1,"b":2}]}`))
	if err != nil || !arguments.Equal(equal) {
		t.Fatalf("equivalent arguments = (%v, %v)", arguments.Equal(equal), err)
	}
	if empty := datasource.EmptyArgumentValues(); empty.Len() != 0 || string(empty.CanonicalJSON()) != "{}" ||
		!empty.Equal(datasource.ArgumentValues{}) {
		t.Fatalf("empty arguments = len %d, json %s", empty.Len(), empty.CanonicalJSON())
	}
}

func TestUNIT004_ArgumentValuesRejectEveryAmbiguousJSONShape(t *testing.T) {
	t.Parallel()
	invalid := [][]byte{
		nil,
		[]byte(`null`),
		[]byte(`[]`),
		[]byte(`"string"`),
		[]byte(`{"a":1} trailing`),
		[]byte(`{"a":1,"a":2}`),
		[]byte(`{"nested":{"x":1,"x":2}}`),
		[]byte(`{"list":[{"x":1,"x":2}]}`),
		{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'},
	}
	for _, input := range invalid {
		arguments, err := datasource.NewArgumentValues(input)
		if err == nil || arguments.CanonicalJSON() != nil || arguments.Len() != 0 {
			t.Errorf("NewArgumentValues(%q) = (%s, %v), want rejected zero", input, arguments.CanonicalJSON(), err)
		}
	}
}

func TestUNIT004_PrincipalViewCopiesAndCanonicalizesLeastPrivilegeData(t *testing.T) {
	t.Parallel()
	tenant, err := datasource.NewTenantID("tenant")
	if err != nil {
		t.Fatal(err)
	}
	scopes := []string{"write", "read", "write"}
	principal, err := datasource.NewPrincipalView("subject", tenant, scopes)
	if err != nil {
		t.Fatal(err)
	}
	scopes[0] = "mutated"
	if principal.Subject() != "subject" || principal.Tenant() != tenant ||
		!reflect.DeepEqual(principal.Scopes(), []string{"read", "write"}) {
		t.Fatalf("principal = subject %q tenant %q scopes %v", principal.Subject(), principal.Tenant().String(), principal.Scopes())
	}
	returned := principal.Scopes()
	returned[0] = "mutated"
	if !reflect.DeepEqual(principal.Scopes(), []string{"read", "write"}) {
		t.Fatalf("Scopes() aliases state: %v", principal.Scopes())
	}
	encoded, err := json.Marshal(principal)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if len(object) != 3 || object["subject"] != "subject" || object["tenant"] != "tenant" {
		t.Fatalf("MarshalJSON() = %s", encoded)
	}

	for _, tc := range []struct {
		name    string
		subject string
		tenant  datasource.TenantID
		scopes  []string
	}{
		{name: "empty subject", tenant: tenant},
		{name: "invalid subject utf8", subject: string([]byte{0xff}), tenant: tenant},
		{name: "invalid tenant", subject: "subject"},
		{name: "empty scope", subject: "subject", tenant: tenant, scopes: []string{""}},
		{name: "invalid scope utf8", subject: "subject", tenant: tenant, scopes: []string{string([]byte{0xff})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := datasource.NewPrincipalView(tc.subject, tc.tenant, tc.scopes)
			if err == nil || got.Subject() != "" || got.Scopes() != nil {
				t.Fatalf("NewPrincipalView() = (%#v, %v), want rejected zero", got, err)
			}
		})
	}
}

func makeBytes(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}
