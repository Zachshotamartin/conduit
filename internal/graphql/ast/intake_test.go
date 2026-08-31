package ast_test

import (
	"bytes"
	stderrors "errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
)

const (
	defaultMaxBytes  = 1 << 20
	defaultMaxTokens = 20_000
	defaultMaxDepth  = 15
)

var (
	allocationOperationSink *graphqlast.Operation
	allocationErrorSink     error
)

func TestUNIT003_IntakeAcceptsAndRejectsExactDefaultBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		atBound    func() []byte
		pastBound  func() []byte
		allocation float64
	}{
		{
			name: "bytes",
			atBound: func() []byte {
				return paddedDocument(defaultMaxBytes)
			},
			pastBound: func() []byte {
				return paddedDocument(defaultMaxBytes + 1)
			},
			allocation: 1,
		},
		{
			name: "tokens",
			atBound: func() []byte {
				return fieldDocument(defaultMaxTokens - 2)
			},
			pastBound: func() []byte {
				return fieldDocument(defaultMaxTokens - 1)
			},
			allocation: 2,
		},
		{
			name: "parse depth",
			atBound: func() []byte {
				return nestedDocument(defaultMaxDepth)
			},
			pastBound: func() []byte {
				return nestedDocument(defaultMaxDepth + 1)
			},
			allocation: 2,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			atBound := tc.atBound()
			operation, err := graphqlast.Intake(atBound, graphqlast.IntakeLimits{}, nil)
			if err != nil {
				t.Fatalf("Intake(at %s bound) error = %v", tc.name, err)
			}
			if operation == nil {
				t.Fatalf("Intake(at %s bound) operation = nil", tc.name)
			}

			pastBound := tc.pastBound()
			assertTypedRejectionWithoutOperation(t, pastBound, graphqlast.IntakeLimits{})

			allocations := testing.AllocsPerRun(100, func() {
				allocationOperationSink, allocationErrorSink = graphqlast.Intake(
					pastBound,
					graphqlast.IntakeLimits{},
					nil,
				)
			})
			if allocations > tc.allocation {
				t.Fatalf(
					"Intake(over %s bound) allocations/run = %.1f, want <= %.1f so no parser AST can be allocated",
					tc.name,
					allocations,
					tc.allocation,
				)
			}
			if allocationOperationSink != nil || allocationErrorSink == nil {
				t.Fatalf(
					"Intake(over %s bound) after allocation run = (%p, %v), want (nil, typed error)",
					tc.name,
					allocationOperationSink,
					allocationErrorSink,
				)
			}
		})
	}
}

func TestUNIT003_IntakeHonorsEachConfiguredLimitIndependently(t *testing.T) {
	tests := []struct {
		name      string
		document  []byte
		limits    graphqlast.IntakeLimits
		wantError bool
	}{
		{
			name:     "byte limit at boundary",
			document: []byte("{a}"),
			limits: graphqlast.IntakeLimits{
				MaxBytes: 3,
			},
		},
		{
			name:      "byte limit one beyond",
			document:  []byte("{a}"),
			limits:    graphqlast.IntakeLimits{MaxBytes: 2},
			wantError: true,
		},
		{
			name:     "token limit at boundary",
			document: []byte("{a}"),
			limits: graphqlast.IntakeLimits{
				MaxTokens: 3,
			},
		},
		{
			name:      "token limit one beyond",
			document:  []byte("{a}"),
			limits:    graphqlast.IntakeLimits{MaxTokens: 2},
			wantError: true,
		},
		{
			name:     "depth limit at boundary",
			document: nestedDocument(2),
			limits: graphqlast.IntakeLimits{
				MaxDepth: 2,
			},
		},
		{
			name:      "depth limit one beyond",
			document:  nestedDocument(2),
			limits:    graphqlast.IntakeLimits{MaxDepth: 1},
			wantError: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			operation, err := graphqlast.Intake(tc.document, tc.limits, nil)
			if tc.wantError {
				assertTypedErrorAndNilOperation(t, operation, err)
				return
			}
			if err != nil {
				t.Fatalf("Intake() error = %v", err)
			}
			if operation == nil {
				t.Fatal("Intake() operation = nil, want parsed operation")
			}
		})
	}
}

func TestUNIT003_TokenCountingIgnoresGraphQLIgnoredText(t *testing.T) {
	t.Parallel()

	document := fixture(t, "ignored-text.graphql")
	operation, err := graphqlast.Intake(document, graphqlast.IntakeLimits{MaxTokens: 10, MaxDepth: 2}, nil)
	if err != nil {
		t.Fatalf("Intake(ignored text fixture) error = %v", err)
	}
	if operation == nil {
		t.Fatal("Intake(ignored text fixture) operation = nil")
	}
}

func TestUNIT003_AllFailuresReturnNoPartialOperationAndAreClassified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document []byte
		limits   graphqlast.IntakeLimits
	}{
		{name: "empty", document: nil},
		{name: "malformed syntax", document: fixture(t, "malformed.graphql")},
		{name: "invalid character", document: []byte("{a\x00}")},
		{name: "byte bound", document: []byte("{a}"), limits: graphqlast.IntakeLimits{MaxBytes: 2}},
		{name: "token bound", document: []byte("{a}"), limits: graphqlast.IntakeLimits{MaxTokens: 2}},
		{name: "depth bound", document: nestedDocument(2), limits: graphqlast.IntakeLimits{MaxDepth: 1}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			operation, err := graphqlast.Intake(tc.document, tc.limits, nil)
			assertTypedErrorAndNilOperation(t, operation, err)
		})
	}
}

func TestUNIT003_DepthPreflightCoversRecursiveValueAndTypeGrammar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document string
	}{
		{name: "list value", document: `query { field(arg: [[[1]]]) }`},
		{name: "object value", document: `query { field(arg: {a: {b: {c: 1}}}) }`},
		{name: "list type", document: `query Q($value: [[[Int]]]) { field(arg: $value) }`},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			operation, err := graphqlast.Intake(
				[]byte(tc.document),
				graphqlast.IntakeLimits{MaxDepth: 3},
				nil,
			)
			assertTypedErrorAndNilOperation(t, operation, err)
		})
	}
}

func TestUNIT003_DepthLimitAbortsBeforeTrailingLexicalFailure(t *testing.T) {
	t.Parallel()

	document := []byte{'{', 'a', '{', 'a', 0}
	operation, err := graphqlast.Intake(
		document,
		graphqlast.IntakeLimits{MaxDepth: 1},
		nil,
	)
	assertTypedErrorAndNilOperation(t, operation, err)

	cause := stderrors.Unwrap(err)
	if cause == nil {
		t.Fatal("Intake() typed rejection has no diagnostic cause")
	}
	const want = "GraphQL document parse-depth limit exceeded"
	if got := cause.Error(); got != want {
		t.Fatalf("Intake() cause = %q, want immediate depth rejection %q", got, want)
	}
}

func TestUNIT003_IntakeParsesDeterministicFixture(t *testing.T) {
	t.Parallel()

	operation, err := graphqlast.Intake(fixture(t, "valid.graphql"), graphqlast.IntakeLimits{}, nil)
	if err != nil {
		t.Fatalf("Intake(valid fixture) error = %v", err)
	}
	if operation == nil {
		t.Fatal("Intake(valid fixture) operation = nil")
	}
}

func TestUNIT003_OperationReportsParsedDocumentShape(t *testing.T) {
	t.Parallel()

	document := []byte(`
		query First { viewer { ...ViewerFields } }
		query Second { viewer { id } }
		fragment ViewerFields on Viewer { id name }
	`)
	operation, err := graphqlast.Intake(document, graphqlast.IntakeLimits{}, nil)
	if err != nil {
		t.Fatalf("Intake() error = %v", err)
	}
	if got := operation.OperationCount(); got != 2 {
		t.Fatalf("OperationCount() = %d, want 2", got)
	}
	if got := operation.FragmentCount(); got != 1 {
		t.Fatalf("FragmentCount() = %d, want 1", got)
	}
}

func TestUNIT006_OperationSnapshotIsResolvedVendorFreeAndDefensive(t *testing.T) {
	t.Parallel()
	schema, err := graphqlast.LoadSchema([]graphqlast.SchemaSource{{
		Name: "schema.graphql",
		Input: []byte(`
			input Filter { tag: String = "default" }
			interface Node { id: ID! }
			type User implements Node { id: ID!, name: String! }
			type Query { node(id: ID!, filter: Filter): Node }
		`),
	}}, graphqlast.SchemaLimits{})
	if err != nil {
		t.Fatalf("LoadSchema() error = %v", err)
	}
	operation, err := graphqlast.Intake([]byte(`
		query Lookup(
			$id: ID! = "u1",
			$show: Boolean! = true,
			$filter: Filter = {tag: "chosen"}
		) @skip(if: false) {
			alias: node(id: $id, filter: $filter) @include(if: $show) {
				__typename
				...NodeFields @skip(if: false)
				... on User { name }
			}
		}
		fragment NodeFields on Node { id }
	`), graphqlast.IntakeLimits{}, schema)
	if err != nil {
		t.Fatalf("Intake() error = %v", err)
	}

	snapshot := operation.Snapshot()
	if len(snapshot.Operations) != 1 || len(snapshot.Fragments) != 1 {
		t.Fatalf("Snapshot() shape = %d operations, %d fragments", len(snapshot.Operations), len(snapshot.Fragments))
	}
	definition := snapshot.Operations[0]
	if definition.Kind != graphqlast.OperationQuery || definition.Name != "Lookup" || len(definition.Variables) != 3 {
		t.Fatalf("operation definition = %#v", definition)
	}
	if definition.Variables[0].Name != "id" || definition.Variables[0].Type.Named != "ID" ||
		!definition.Variables[0].Type.NonNull || definition.Variables[0].DefaultValue == nil ||
		definition.Variables[0].DefaultValue.Kind != graphqlast.ValueString {
		t.Fatalf("id variable = %#v", definition.Variables[0])
	}
	if len(definition.Directives) != 1 || definition.Directives[0].Name != "skip" {
		t.Fatalf("operation directives = %#v", definition.Directives)
	}
	if len(definition.SelectionSet) != 1 || definition.SelectionSet[0].Kind != graphqlast.SelectionField {
		t.Fatalf("root selection = %#v", definition.SelectionSet)
	}
	field := definition.SelectionSet[0].Field
	if field.Name != "node" || field.Alias != "alias" || field.ParentType != "Query" ||
		field.Type.Named != "Node" || len(field.Arguments) != 2 || len(field.SelectionSet) != 3 {
		t.Fatalf("resolved field = %#v", field)
	}
	if field.Arguments[0].Value.Kind != graphqlast.ValueVariable || field.Arguments[0].Value.Raw != "id" {
		t.Fatalf("field argument = %#v", field.Arguments[0])
	}
	if field.SelectionSet[1].Kind != graphqlast.SelectionFragmentSpread ||
		field.SelectionSet[2].Kind != graphqlast.SelectionInlineFragment {
		t.Fatalf("nested selections = %#v", field.SelectionSet)
	}
	if snapshot.Fragments[0].Name != "NodeFields" || snapshot.Fragments[0].TypeCondition != "Node" {
		t.Fatalf("fragment = %#v", snapshot.Fragments[0])
	}
	if strings.Contains(reflect.TypeOf(snapshot).String(), "gqlparser") {
		t.Fatalf("operation snapshot leaks parser type: %T", snapshot)
	}

	snapshot.Operations[0].Name = "mutated"
	snapshot.Operations[0].SelectionSet[0].Field.Arguments[0].Value.Raw = "mutated"
	snapshot.Fragments[0].SelectionSet[0].Field.Name = "mutated"
	again := operation.Snapshot()
	if again.Operations[0].Name != "Lookup" || again.Operations[0].SelectionSet[0].Field.Arguments[0].Value.Raw != "id" ||
		again.Fragments[0].SelectionSet[0].Field.Name != "id" {
		t.Fatalf("Snapshot() exposed operation state: %#v", again)
	}
}

func FuzzIntake(f *testing.F) {
	f.Fuzz(func(t *testing.T, document []byte) {
		operation, err := graphqlast.Intake(
			document,
			graphqlast.IntakeLimits{MaxBytes: 4 << 10, MaxTokens: 256, MaxDepth: 12},
			nil,
		)
		if err == nil {
			if operation == nil {
				t.Fatal("Intake() returned (nil, nil)")
			}
			return
		}
		assertTypedErrorAndNilOperation(t, operation, err)
	})
}

func assertTypedRejectionWithoutOperation(t *testing.T, document []byte, limits graphqlast.IntakeLimits) {
	t.Helper()

	operation, err := graphqlast.Intake(document, limits, nil)
	assertTypedErrorAndNilOperation(t, operation, err)
}

func assertTypedErrorAndNilOperation(t *testing.T, operation *graphqlast.Operation, err error) {
	t.Helper()

	if operation != nil {
		t.Fatalf("Intake() operation = %p on failure, want nil", operation)
	}
	if err == nil {
		t.Fatal("Intake() error = nil, want typed rejection")
	}
	var typed *conduiterrors.Error
	if !stderrors.As(err, &typed) {
		t.Fatalf("Intake() error type = %T, want *errors.Error", err)
	}
	if got := typed.Category(); got != conduiterrors.InvalidRequest {
		t.Fatalf("Intake() error category = %q, want %q", got, conduiterrors.InvalidRequest)
	}
}

func paddedDocument(size int) []byte {
	base := []byte("{a}")
	if size < len(base) {
		panic("paddedDocument size is smaller than its valid GraphQL prefix")
	}
	return append(base, bytes.Repeat([]byte(" "), size-len(base))...)
}

func fieldDocument(fields int) []byte {
	var document strings.Builder
	document.Grow(2*fields + 2)
	document.WriteByte('{')
	for index := 0; index < fields; index++ {
		document.WriteString(" a")
	}
	document.WriteString(" }")
	return []byte(document.String())
}

func nestedDocument(depth int) []byte {
	if depth < 1 {
		panic("nestedDocument depth must be positive")
	}
	var document strings.Builder
	document.Grow(3 * depth)
	for level := 0; level < depth; level++ {
		document.WriteString("{a")
	}
	for level := 0; level < depth; level++ {
		document.WriteByte('}')
	}
	return []byte(document.String())
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()

	contents, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return contents
}
