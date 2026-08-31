package ast_test

import (
	"bytes"
	stderrors "errors"
	"reflect"
	"strings"
	"testing"

	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
)

func TestUNIT002_SchemaLoaderEnforcesAggregatePreparseBound(t *testing.T) {
	t.Parallel()

	const max = 4 << 20
	base := []byte("type Query { ok: Boolean! }\n")
	atBound := append(append([]byte(nil), base...), bytes.Repeat([]byte(" "), max-len(base))...)
	loaded, err := graphqlast.LoadSchema(
		[]graphqlast.SchemaSource{{Name: "schema.graphql", Input: atBound}},
		graphqlast.SchemaLimits{},
	)
	if err != nil {
		t.Fatalf("LoadSchema(at aggregate byte bound) error = %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadSchema(at aggregate byte bound) schema = nil")
	}

	over := append(append([]byte(nil), atBound...), ' ')
	loaded, err = graphqlast.LoadSchema(
		[]graphqlast.SchemaSource{{Name: "schema.graphql", Input: over}},
		graphqlast.SchemaLimits{},
	)
	assertSchemaDiagnostics(t, loaded, err, "sdl.limit.bytes", "schema.graphql")
}

func TestUNIT002_SchemaLoaderCollectsEveryMalformedFile(t *testing.T) {
	t.Parallel()

	loaded, err := graphqlast.LoadSchema([]graphqlast.SchemaSource{
		{Name: "./zeta//broken.graphql", Input: []byte("type Query {")},
		{Name: "alpha\\broken.graphql", Input: []byte("type Query { field: }")},
	}, graphqlast.SchemaLimits{})
	if loaded != nil {
		t.Fatal("LoadSchema(malformed set) returned a partial schema")
	}
	diagnostics := diagnosticsFrom(t, err)
	items := diagnostics.Items()
	if len(items) != 2 {
		t.Fatalf("diagnostic count = %d, want 2: %v", len(items), err)
	}
	if got, want := items[0].File, "alpha/broken.graphql"; got != want {
		t.Fatalf("first diagnostic file = %q, want %q", got, want)
	}
	if got, want := items[1].File, "zeta/broken.graphql"; got != want {
		t.Fatalf("second diagnostic file = %q, want %q", got, want)
	}
	for _, item := range items {
		if item.Rule != "graphql.sdl.syntax" {
			t.Errorf("diagnostic rule = %q, want graphql.sdl.syntax", item.Rule)
		}
		if item.Line < 1 || item.Column < 1 {
			t.Errorf("diagnostic location = %d:%d, want positive", item.Line, item.Column)
		}
	}
}

func TestUNIT002_SchemaLoaderRejectsUnsafeOrDuplicateLogicalNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sources []graphqlast.SchemaSource
	}{
		{
			name: "absolute",
			sources: []graphqlast.SchemaSource{
				{Name: "/private/tmp/schema.graphql", Input: []byte("type Query { ok: Boolean }")},
			},
		},
		{
			name: "parent traversal",
			sources: []graphqlast.SchemaSource{
				{Name: "../schema.graphql", Input: []byte("type Query { ok: Boolean }")},
			},
		},
		{
			name: "duplicate after normalization",
			sources: []graphqlast.SchemaSource{
				{Name: "schemas/./main.graphql", Input: []byte("type Query { ok: Boolean }")},
				{Name: "schemas/main.graphql", Input: []byte("type Other { ok: Boolean }")},
			},
		},
		{
			name: "control character",
			sources: []graphqlast.SchemaSource{
				{Name: "schema\nforged.graphql", Input: []byte("type Query { ok: Boolean }")},
			},
		},
		{
			name: "invalid UTF-8",
			sources: []graphqlast.SchemaSource{
				{Name: string([]byte{'s', 0xff}), Input: []byte("type Query { ok: Boolean }")},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loaded, err := graphqlast.LoadSchema(tc.sources, graphqlast.SchemaLimits{})
			assertSchemaDiagnostics(t, loaded, err, "sdl.file.logical_name", "")
		})
	}
}

func TestUNIT002_SchemaLoaderEnforcesExactTokenAndDepthBounds(t *testing.T) {
	t.Parallel()
	const flat = "type Query { ok: Boolean }"
	loaded, err := graphqlast.LoadSchema([]graphqlast.SchemaSource{{
		Name: "flat.graphql", Input: []byte(flat),
	}}, graphqlast.SchemaLimits{MaxTokens: 7, MaxNestingDepth: 1})
	if err != nil || loaded == nil {
		t.Fatalf("LoadSchema(at token/depth bounds) = (%v, %v), want schema", loaded, err)
	}
	loaded, err = graphqlast.LoadSchema([]graphqlast.SchemaSource{{
		Name: "flat.graphql", Input: []byte(flat),
	}}, graphqlast.SchemaLimits{MaxTokens: 6})
	assertSchemaDiagnostics(t, loaded, err, "sdl.limit.tokens", "flat.graphql")

	const nested = "type Query { value(input: Int): Int }"
	loaded, err = graphqlast.LoadSchema([]graphqlast.SchemaSource{{
		Name: "nested.graphql", Input: []byte(nested),
	}}, graphqlast.SchemaLimits{MaxNestingDepth: 2})
	if err != nil || loaded == nil {
		t.Fatalf("LoadSchema(at nesting bound) = (%v, %v), want schema", loaded, err)
	}
	loaded, err = graphqlast.LoadSchema([]graphqlast.SchemaSource{{
		Name: "nested.graphql", Input: []byte(nested),
	}}, graphqlast.SchemaLimits{MaxNestingDepth: 1})
	assertSchemaDiagnostics(t, loaded, err, "sdl.limit.depth", "nested.graphql")
}

func TestUNIT002_SchemaSnapshotIsVendorFreeAndDefensive(t *testing.T) {
	t.Parallel()

	loaded, err := graphqlast.LoadSchema([]graphqlast.SchemaSource{{
		Name: "schema.graphql",
		Input: []byte(`
			type Query { viewer(id: ID!): Viewer }
			type Viewer { id: ID! }
		`),
	}}, graphqlast.SchemaLimits{})
	if err != nil {
		t.Fatalf("LoadSchema() error = %v", err)
	}

	snapshot := loaded.Snapshot()
	if len(snapshot.Types) == 0 {
		t.Fatal("Snapshot().Types is empty")
	}
	if strings.Contains(reflect.TypeOf(snapshot).String(), "gqlparser") {
		t.Fatalf("Snapshot type leaks gqlparser: %T", snapshot)
	}
	original := snapshot.Types[0].Name
	snapshot.Types[0].Name = "mutated"
	if got := loaded.Snapshot().Types[0].Name; got != original {
		t.Fatalf("Snapshot mutation changed serving schema: got %q, want %q", got, original)
	}
}

func TestUNIT002_SchemaValidatesOperationsAndRetainsAnchor(t *testing.T) {
	t.Parallel()
	schema, err := graphqlast.LoadSchema([]graphqlast.SchemaSource{{
		Name: "schema.graphql", Input: []byte("type Query { ok: Boolean! }"),
	}}, graphqlast.SchemaLimits{})
	if err != nil {
		t.Fatalf("LoadSchema() error = %v", err)
	}
	if schema.Anchor() == (graphqlast.SchemaAnchor{}) {
		t.Fatal("Schema.Anchor() returned the forgeable zero anchor")
	}

	operation, err := graphqlast.Intake([]byte("{ ok }"), graphqlast.IntakeLimits{}, schema)
	if err != nil {
		t.Fatalf("Intake(known field) error = %v", err)
	}
	if operation.Anchor() != schema.Anchor() {
		t.Fatal("admitted operation does not retain its validating schema anchor")
	}

	operation, err = graphqlast.Intake([]byte("{ missing }"), graphqlast.IntakeLimits{}, schema)
	if operation != nil {
		t.Fatal("Intake(unknown field) returned a partial operation")
	}
	if err == nil {
		t.Fatal("Intake(unknown field) error = nil")
	}
	var classified *conduiterrors.Error
	if !stderrors.As(err, &classified) || classified.Category() != conduiterrors.InvalidRequest {
		t.Fatalf("Intake(unknown field) error = %v, want invalid_request", err)
	}
}

func TestUNIT002_IntrospectionSurfaceIsFrozenAtOctober2021(t *testing.T) {
	t.Parallel()
	schema, err := graphqlast.LoadSchema([]graphqlast.SchemaSource{{
		Name: "schema.graphql",
		Input: []byte(`
			type Query { ok: Boolean! }
			input Filter { value: String }
		`),
	}}, graphqlast.SchemaLimits{})
	if err != nil {
		t.Fatalf("LoadSchema() error = %v", err)
	}

	accepted := []byte(`{
		__schema { description }
		__type(name: "String") { specifiedByURL }
	}`)
	operation, err := graphqlast.Intake(accepted, graphqlast.IntakeLimits{}, schema)
	if err != nil || operation == nil {
		t.Fatalf("Intake(October 2021 introspection) = (%v, %v), want operation", operation, err)
	}

	rejected := map[string]string{
		"oneOf flag":                     `{ __type(name: "Filter") { isOneOf } }`,
		"input-field deprecation arg":    `{ __type(name: "Filter") { inputFields(includeDeprecated: true) { name } } }`,
		"input-value deprecated flag":    `{ __type(name: "Filter") { inputFields { isDeprecated } } }`,
		"field-argument deprecation":     `{ __type(name: "Query") { fields { args(includeDeprecated: true) { name } } } }`,
		"directive-argument deprecation": `{ __schema { directives { args(includeDeprecated: true) { name } } }`,
	}
	for name, document := range rejected {
		name, document := name, document
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			operation, err := graphqlast.Intake([]byte(document), graphqlast.IntakeLimits{}, schema)
			assertTypedErrorAndNilOperation(t, operation, err)
		})
	}
}

func assertSchemaDiagnostics(
	t *testing.T,
	schema *graphqlast.Schema,
	err error,
	wantRule string,
	wantFile string,
) {
	t.Helper()
	if schema != nil {
		t.Fatal("LoadSchema() returned partial schema on failure")
	}
	diagnostics := diagnosticsFrom(t, err)
	items := diagnostics.Items()
	if len(items) == 0 {
		t.Fatal("LoadSchema() returned no diagnostics")
	}
	if got := items[0].Rule; got != wantRule {
		t.Fatalf("first diagnostic rule = %q, want %q", got, wantRule)
	}
	if wantFile != "" && items[0].File != wantFile {
		t.Fatalf("first diagnostic file = %q, want %q", items[0].File, wantFile)
	}
}

func diagnosticsFrom(t *testing.T, err error) *graphqlast.Diagnostics {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want schema diagnostics")
	}
	var diagnostics *graphqlast.Diagnostics
	if !stderrors.As(err, &diagnostics) {
		t.Fatalf("error type = %T, want *ast.Diagnostics", err)
	}
	var classified *conduiterrors.Error
	if !stderrors.As(err, &classified) {
		t.Fatalf("error does not unwrap to classified Conduit error: %v", err)
	}
	if got := classified.Category(); got != conduiterrors.InvalidConfiguration {
		t.Fatalf("error category = %q, want %q", got, conduiterrors.InvalidConfiguration)
	}
	return diagnostics
}
