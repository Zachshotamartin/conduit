package schema_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
	graphqlschema "github.com/Zachshotamartin/conduit/internal/graphql/schema"
)

func TestUNIT002_AccumulatesTypePositionRulesBeforeCompilerGuard(t *testing.T) {
	t.Parallel()
	loaded, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{{
		Name:  "aggregate-spec/type-positions.graphql",
		Input: fixture(t, "aggregate-spec/type-positions.graphql"),
	}}, schemaOptions)
	if loaded != nil {
		t.Fatal("LoadSources() returned partial schema")
	}
	items := diagnosticsFrom(t, err).Items()
	want := []string{
		"graphql.sdl.union_member_object",
		"graphql.sdl.output_type",
		"graphql.sdl.input_type",
	}
	got := make([]string, len(items))
	for index, item := range items {
		got[index] = item.Rule
		if item.Rule == "graphql.sdl.compiler_guard" {
			t.Fatalf("authored invalid SDL escaped to compiler guard: %v", err)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rules = %v, want %v: %v", got, want, err)
	}
}

func TestUNIT002_CollectsIndependentConduitArgumentErrors(t *testing.T) {
	t.Parallel()
	loaded, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{{
		Name:  "aggregate/conduit-arguments.graphql",
		Input: fixture(t, "aggregate/conduit-arguments.graphql"),
	}}, schemaOptions)
	if loaded != nil {
		t.Fatal("LoadSources() returned partial schema")
	}
	items := diagnosticsFrom(t, err).Items()
	want := []string{
		"conduit.backpressure.policy",
		"conduit.backpressure.queue_type",
		"conduit.backpressure.coalesce_key_nonempty",
	}
	got := make([]string, len(items))
	for index := range items {
		got[index] = items[index].Rule
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rules = %v, want %v: %v", got, want, err)
	}
}

func TestUNIT002_CustomFutureNamedDirectivesRemainAuthoredSchema(t *testing.T) {
	t.Parallel()
	loaded, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{{
		Name:  "valid/custom-future-named-directives.graphql",
		Input: fixture(t, "valid/custom-future-named-directives.graphql"),
	}}, schemaOptions)
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	names := make([]string, 0, 3)
	for _, definition := range loaded.Executable().Snapshot().Directives {
		names = append(names, definition.Name)
	}
	want := []string{"defer", "oneOf", "stream"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("authored directives = %v, want %v", names, want)
	}
}

func TestUNIT002_HashCoversOrderedSchemaDirectivesAndNormalizesNumbers(t *testing.T) {
	t.Parallel()
	load := func(name, values string) *graphqlschema.Schema {
		t.Helper()
		input := []byte(`
directive @tag(value: Float!) repeatable on SCHEMA
schema ` + values + ` { query: Query }
type Query { ok: Boolean }
`)
		loaded, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{{Name: name, Input: input}}, schemaOptions)
		if err != nil {
			t.Fatalf("LoadSources(%s) error = %v", name, err)
		}
		return loaded
	}
	canonical := load("canonical.graphql", `@tag(value: 1.0) @tag(value: 2.0)`)
	equivalent := load("equivalent.graphql", `@tag(value: 1e0) @tag(value: 2.00)`)
	reversed := load("reversed.graphql", `@tag(value: 2.0) @tag(value: 1.0)`)
	changed := load("changed.graphql", `@tag(value: 1.0) @tag(value: 3.0)`)
	if canonical.Hash() != equivalent.Hash() {
		t.Fatalf("numeric spellings changed semantic hash: %s != %s", canonical.Hash(), equivalent.Hash())
	}
	if canonical.Hash() == reversed.Hash() {
		t.Fatalf("ordered repeated schema directives did not change hash: %s", canonical.Hash())
	}
	if canonical.Hash() == changed.Hash() {
		t.Fatalf("schema directive value did not change hash: %s", canonical.Hash())
	}
}

func TestUNIT002_LoadSourcesCannotTrustCallerBuiltInFlag(t *testing.T) {
	t.Parallel()
	loaded, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{{
		Name: "untrusted.graphql", BuiltIn: true,
		Input: []byte(`type Query { ok: Boolean @source(name: "missing") }`),
	}}, schemaOptions)
	if loaded != nil {
		t.Fatal("LoadSources() trusted caller BuiltIn flag and returned schema")
	}
	items := diagnosticsFrom(t, err).Items()
	if len(items) != 1 || items[0].Rule != "conduit.source.name_unknown" {
		t.Fatalf("diagnostics = %#v, want source registry rejection", items)
	}
}

func TestUNIT002_ConduitDirectiveSignaturesAreExactAndClosed(t *testing.T) {
	t.Parallel()
	loaded, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{{
		Name: "schema.graphql", Input: []byte("type Query { ok: Boolean }")},
	}, schemaOptions)
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	snapshot := loaded.Executable().Snapshot()

	definitions := make(map[string]graphqlast.DirectiveDefinition)
	for _, definition := range snapshot.Directives {
		definitions[definition.Name] = definition
	}
	expected := map[string]struct {
		location string
		args     map[string]string
	}{
		"source":       {location: "FIELD_DEFINITION", args: map[string]string{"name": "String!"}},
		"auth":         {location: "FIELD_DEFINITION", args: map[string]string{"rule": "String!"}},
		"filterable":   {location: "ARGUMENT_DEFINITION", args: map[string]string{}},
		"backpressure": {location: "FIELD_DEFINITION", args: map[string]string{"policy": "ConduitBackpressurePolicy", "queue": "Int", "coalesceKey": "String"}},
		"complexity":   {location: "FIELD_DEFINITION", args: map[string]string{"cost": "Int", "multipliers": "[String!]"}},
	}
	for name, want := range expected {
		definition, ok := definitions[name]
		if !ok {
			t.Errorf("compiled schema omits @%s", name)
			continue
		}
		if definition.Repeatable {
			t.Errorf("@%s is advertised as repeatable", name)
		}
		if !reflect.DeepEqual(definition.Locations, []string{want.location}) {
			t.Errorf("@%s locations = %v, want [%s]", name, definition.Locations, want.location)
		}
		gotArgs := make(map[string]string, len(definition.Arguments))
		for _, argument := range definition.Arguments {
			gotArgs[argument.Name] = typeRefString(argument.Type)
		}
		if !reflect.DeepEqual(gotArgs, want.args) {
			t.Errorf("@%s arguments = %v, want %v", name, gotArgs, want.args)
		}
	}

	var policy *graphqlast.TypeDefinition
	for index := range snapshot.Types {
		if snapshot.Types[index].Name == "ConduitBackpressurePolicy" {
			policy = &snapshot.Types[index]
			break
		}
	}
	if policy == nil {
		t.Fatal("compiled schema has dangling ConduitBackpressurePolicy reference")
	}
	values := make([]string, len(policy.EnumValues))
	for index := range policy.EnumValues {
		values[index] = policy.EnumValues[index].Name
	}
	wantValues := []string{"DROP_OLDEST", "COALESCE_BY_KEY", "DISCONNECT"}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("ConduitBackpressurePolicy values = %v, want %v", values, wantValues)
	}
}

func TestUNIT002_LoadFilesRejectsUnsafeLogicalNameBeforePhysicalRead(t *testing.T) {
	t.Parallel()
	physical := filepath.Join(t.TempDir(), "must-not-leak.graphql")
	loaded, err := graphqlschema.LoadFiles([]graphqlschema.File{{
		Path: physical, Name: "operator\nforged.graphql",
	}}, schemaOptions)
	if loaded != nil {
		t.Fatal("LoadFiles() returned partial schema")
	}
	items := diagnosticsFrom(t, err).Items()
	if len(items) != 1 || items[0].Rule != "sdl.file.logical_name" || items[0].File != "" {
		t.Fatalf("diagnostics = %#v", items)
	}
	if strings.Contains(err.Error(), "forged.graphql") || strings.Contains(err.Error(), physical) {
		t.Fatalf("unsafe logical or physical name leaked: %v", err)
	}
	if _, statErr := os.Stat(physical); !os.IsNotExist(statErr) {
		t.Fatalf("unexpected physical path state: %v", statErr)
	}
}

func typeRefString(reference graphqlast.TypeRef) string {
	suffix := ""
	if reference.NonNull {
		suffix = "!"
	}
	if reference.Element != nil {
		return "[" + typeRefString(*reference.Element) + "]" + suffix
	}
	return reference.Named + suffix
}
