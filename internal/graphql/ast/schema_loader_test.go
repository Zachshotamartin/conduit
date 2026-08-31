package ast_test

import (
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
	atBound := append(append([]byte(nil), base...), make([]byte, max-len(base))...)
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
