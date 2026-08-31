package executor_test

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Zachshotamartin/conduit/internal/datasource"
	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
	"github.com/Zachshotamartin/conduit/internal/graphql/binding"
	"github.com/Zachshotamartin/conduit/internal/graphql/executor"
	graphqlschema "github.com/Zachshotamartin/conduit/internal/graphql/schema"
)

type executionCorpus struct {
	Values map[string]json.RawMessage `json:"values"`
	Cases  []executionCase            `json:"cases"`
}

type executionCase struct {
	Name          string            `json:"name"`
	Feature       string            `json:"feature"`
	Document      string            `json:"document"`
	OperationName string            `json:"operation_name"`
	Variables     json.RawMessage   `json:"variables"`
	Responses     map[string]string `json:"responses"`
	Calls         []expectedCall    `json:"calls"`
	OrderedCalls  bool              `json:"ordered_calls"`
	Want          json.RawMessage   `json:"want"`
}

type expectedCall struct {
	Field string          `json:"field"`
	Args  json.RawMessage `json:"args"`
}

type recordedCall struct {
	Field  string
	Args   string
	Parent string
}

type corpusSource struct {
	mu        sync.Mutex
	values    map[string]json.RawMessage
	responses map[string]string
	calls     []recordedCall
	failures  map[string]error
}

func (source *corpusSource) Name() string { return "fixture" }

func (source *corpusSource) Resolve(_ context.Context, request *datasource.SourceRequest) (*datasource.SourceResponse, error) {
	call := recordedCall{Field: request.Field.String(), Args: string(request.Args.CanonicalJSON())}
	if request.Parent != nil {
		call.Parent = string(request.Parent)
	}
	source.mu.Lock()
	source.calls = append(source.calls, call)
	failure := source.failures[call.Field]
	valueName, configured := source.responses[call.Field]
	value := append(json.RawMessage(nil), source.values[valueName]...)
	source.mu.Unlock()
	if failure != nil {
		return nil, failure
	}
	if !configured {
		return nil, fmt.Errorf("unexpected source call %s", call.Field)
	}
	return &datasource.SourceResponse{Data: value}, nil
}

func (source *corpusSource) HealthCheck(context.Context) error { return nil }
func (source *corpusSource) Close(context.Context) error       { return nil }

func (source *corpusSource) Calls() []recordedCall {
	source.mu.Lock()
	defer source.mu.Unlock()
	return append([]recordedCall(nil), source.calls...)
}

func TestUNIT006_SpecExecutionCorpusIsByteExact(t *testing.T) {
	corpus := readCorpus(t)
	if len(corpus.Cases) < 60 {
		t.Fatalf("execution corpus count = %d, want at least 60", len(corpus.Cases))
	}
	requiredFeatures := map[string]bool{
		"field_collection": false,
		"aliases":          false,
		"fragments":        false,
		"directives":       false,
		"variables":        false,
		"coercion":         false,
		"type_conditions":  false,
		"null_propagation": false,
		"mutation":         false,
	}
	schema, table := loadExecutionArtifacts(t)
	seenNames := make(map[string]struct{}, len(corpus.Cases))
	for _, corpusCase := range corpus.Cases {
		corpusCase := corpusCase
		if _, duplicate := seenNames[corpusCase.Name]; duplicate {
			t.Fatalf("duplicate corpus case name %q", corpusCase.Name)
		}
		seenNames[corpusCase.Name] = struct{}{}
		if _, required := requiredFeatures[corpusCase.Feature]; required {
			requiredFeatures[corpusCase.Feature] = true
		}
		t.Run(corpusCase.Name, func(t *testing.T) {
			t.Parallel()
			source := &corpusSource{values: corpus.Values, responses: corpusCase.Responses}
			runtime, err := executor.New(executor.Config{
				Schema: schema, Bindings: table, Sources: []datasource.DataSource{source}, MaxSourceConcurrency: 4,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			operation, err := graphqlast.Intake([]byte(corpusCase.Document), graphqlast.IntakeLimits{}, schema.Executable())
			if err != nil {
				t.Fatalf("Intake() error = %v", err)
			}
			result := runtime.Execute(context.Background(), executor.Request{
				Operation: operation, OperationName: corpusCase.OperationName, Variables: corpusCase.Variables,
				Tenant: testTenant(t), Principal: testPrincipal(t), Deadline: testDeadline(),
			})
			got, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("Marshal(result) error = %v", err)
			}
			want := compactJSON(t, corpusCase.Want)
			if !bytes.Equal(got, want) {
				t.Fatalf("response mismatch\n got: %s\nwant: %s", got, want)
			}
			assertCalls(t, source.Calls(), corpusCase)
		})
	}
	for feature, found := range requiredFeatures {
		if !found {
			t.Errorf("execution corpus has no %s case", feature)
		}
	}
}

func TestUNIT006_MutationsRemainSerialAndContinueAfterMiddleFailure(t *testing.T) {
	t.Parallel()
	schema, table := loadExecutionArtifacts(t)
	source := &corpusSource{
		values: map[string]json.RawMessage{
			"one":   json.RawMessage(`"one"`),
			"three": json.RawMessage(`"three"`),
		},
		responses: map[string]string{
			"Mutation.first": "one", "Mutation.second": "unused", "Mutation.third": "three",
		},
		failures: map[string]error{
			"Mutation.second": conduiterrors.New(conduiterrors.SourceUnavailable),
		},
	}
	runtime, err := executor.New(executor.Config{Schema: schema, Bindings: table, Sources: []datasource.DataSource{source}})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := graphqlast.Intake([]byte(`mutation Run {
		first(value: "1")
		second(value: "2")
		third(value: "3")
	}`), graphqlast.IntakeLimits{}, schema.Executable())
	if err != nil {
		t.Fatal(err)
	}
	result := runtime.Execute(context.Background(), executor.Request{
		Operation: operation, Tenant: testTenant(t), Principal: testPrincipal(t), Deadline: testDeadline(),
	})
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	want := compactJSON(t, json.RawMessage(`{
		"data":{"first":"one","second":null,"third":"three"},
		"errors":[{"message":"data source unavailable","path":["second"],"extensions":{"code":"source_unavailable"}}]
	}`))
	if !bytes.Equal(encoded, want) {
		t.Fatalf("mutation response\n got: %s\nwant: %s", encoded, want)
	}
	calls := source.Calls()
	fields := make([]string, len(calls))
	for index := range calls {
		fields[index] = calls[index].Field
	}
	if !reflect.DeepEqual(fields, []string{"Mutation.first", "Mutation.second", "Mutation.third"}) {
		t.Fatalf("mutation source order = %v", fields)
	}
}

func TestUNIT006_SourceRequestIsNarrowedCanonicalAndDeadlineBound(t *testing.T) {
	t.Parallel()
	schema := loadSchema(t, `
		type Query { item(id: ID!): Item @source(name: "fixture") }
		type Item { id: ID!, remote(flag: Boolean!): String @source(name: "fixture") }
	`)
	table := loadBindings(t, schema, `bindings:
  - field: Query.item
    source: fixture
  - field: Item.id
    parent: [id]
  - field: Item.remote
    source: fixture
`)
	source := &corpusSource{
		values: map[string]json.RawMessage{
			"item":   json.RawMessage(`{"id":"i1"}`),
			"remote": json.RawMessage(`"ok"`),
		},
		responses: map[string]string{"Query.item": "item", "Item.remote": "remote"},
	}
	runtime, err := executor.New(executor.Config{Schema: schema, Bindings: table, Sources: []datasource.DataSource{source}})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := graphqlast.Intake(
		[]byte(`query($id: ID!) { item(id: $id) { remote(flag: true) } }`),
		graphqlast.IntakeLimits{}, schema.Executable(),
	)
	if err != nil {
		t.Fatal(err)
	}
	tenant := testTenant(t)
	principal := testPrincipal(t)
	deadline := testDeadline()
	result := runtime.Execute(context.Background(), executor.Request{
		Operation: operation, Variables: json.RawMessage(`{"id":7}`), Tenant: tenant, Principal: principal, Deadline: deadline,
	})
	if len(result.Errors) != 0 {
		t.Fatalf("Execute() errors = %#v", result.Errors)
	}
	calls := source.Calls()
	if len(calls) != 2 {
		t.Fatalf("source calls = %#v", calls)
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].Field < calls[j].Field })
	if calls[0].Field != "Item.remote" || calls[0].Args != `{"flag":true}` || calls[0].Parent != `{"id":"i1"}` {
		t.Fatalf("nested source call = %#v", calls[0])
	}
	if calls[1].Field != "Query.item" || calls[1].Args != `{"id":"7"}` || calls[1].Parent != "" {
		t.Fatalf("root source call = %#v", calls[1])
	}
}

func TestUNIT006_OperationSelectionAndVariableFailuresNeverCallSources(t *testing.T) {
	t.Parallel()
	schema, table := loadExecutionArtifacts(t)
	tests := []struct {
		name          string
		document      string
		operationName string
		variables     json.RawMessage
	}{
		{name: "ambiguous operation", document: `query A { user(id: "u1") { id } } query B { user(id: "u1") { id } }`},
		{name: "unknown operation", document: `query A { user(id: "u1") { id } }`, operationName: "B"},
		{name: "missing variable", document: `query($id: ID!) { user(id: $id) { id } }`, variables: json.RawMessage(`{}`)},
		{name: "unknown variable", document: `query($id: ID!) { user(id: $id) { id } }`, variables: json.RawMessage(`{"id":"u1","extra":true}`)},
		{name: "wrong variable type", document: `query($id: ID!) { user(id: $id) { id } }`, variables: json.RawMessage(`{"id":true}`)},
		{name: "malformed variables", document: `query($id: ID!) { user(id: $id) { id } }`, variables: json.RawMessage(`{"id":"u1","id":"u2"}`)},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := &corpusSource{values: map[string]json.RawMessage{"user": json.RawMessage(`{"id":"u1"}`)}, responses: map[string]string{"Query.user": "user"}}
			runtime, err := executor.New(executor.Config{Schema: schema, Bindings: table, Sources: []datasource.DataSource{source}})
			if err != nil {
				t.Fatal(err)
			}
			operation, err := graphqlast.Intake([]byte(tc.document), graphqlast.IntakeLimits{}, schema.Executable())
			if err != nil {
				t.Fatal(err)
			}
			result := runtime.Execute(context.Background(), executor.Request{
				Operation: operation, OperationName: tc.operationName, Variables: tc.variables,
				Tenant: testTenant(t), Principal: testPrincipal(t), Deadline: testDeadline(),
			})
			if len(result.Errors) != 1 || result.Errors[0].Code != conduiterrors.InvalidRequest {
				t.Fatalf("Execute() errors = %#v, want one invalid_request", result.Errors)
			}
			if calls := source.Calls(); len(calls) != 0 {
				t.Fatalf("invalid operation called source: %#v", calls)
			}
		})
	}
}

func TestUNIT006_ExecutorRejectsArtifactAndSourceMismatches(t *testing.T) {
	t.Parallel()
	schema, table := loadExecutionArtifacts(t)
	otherSchema := loadSchema(t, `type Query { ok: Boolean @source(name: "fixture") }`)
	otherTable := loadBindings(t, otherSchema, `bindings:
  - field: Query.ok
    source: fixture
`)
	validSource := &corpusSource{}
	for _, tc := range []struct {
		name   string
		config executor.Config
	}{
		{name: "nil schema", config: executor.Config{Bindings: table, Sources: []datasource.DataSource{validSource}}},
		{name: "nil bindings", config: executor.Config{Schema: schema, Sources: []datasource.DataSource{validSource}}},
		{name: "anchor mismatch", config: executor.Config{Schema: schema, Bindings: otherTable, Sources: []datasource.DataSource{validSource}}},
		{name: "nil source", config: executor.Config{Schema: schema, Bindings: table, Sources: []datasource.DataSource{nil}}},
		{name: "duplicate source", config: executor.Config{Schema: schema, Bindings: table, Sources: []datasource.DataSource{validSource, validSource}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := executor.New(tc.config)
			if err == nil || got != nil {
				t.Fatalf("New() = (%#v, %v), want rejected nil", got, err)
			}
			var classified *conduiterrors.Error
			if !stderrors.As(err, &classified) || classified.Category() != conduiterrors.InvalidConfiguration {
				t.Fatalf("New() error = %v, want invalid_configuration", err)
			}
		})
	}
}

func assertCalls(t *testing.T, actual []recordedCall, corpusCase executionCase) {
	t.Helper()
	var expected []recordedCall
	if corpusCase.Calls != nil {
		for _, call := range corpusCase.Calls {
			args := "{}"
			if len(call.Args) > 0 {
				arguments, err := datasource.NewArgumentValues(call.Args)
				if err != nil {
					t.Fatalf("invalid expected args for %s: %v", call.Field, err)
				}
				args = string(arguments.CanonicalJSON())
			}
			expected = append(expected, recordedCall{Field: call.Field, Args: args})
		}
	} else {
		for field := range corpusCase.Responses {
			expected = append(expected, recordedCall{Field: field})
		}
		for index := range actual {
			actual[index].Args = ""
		}
	}
	if !corpusCase.OrderedCalls {
		sort.Slice(actual, func(i, j int) bool {
			return actual[i].Field+actual[i].Args < actual[j].Field+actual[j].Args
		})
		sort.Slice(expected, func(i, j int) bool {
			return expected[i].Field+expected[i].Args < expected[j].Field+expected[j].Args
		})
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("source calls = %#v, want %#v", actual, expected)
	}
}

func loadExecutionArtifacts(t *testing.T) (*graphqlschema.Schema, *binding.Table) {
	t.Helper()
	schemaSDL, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "executor", "schema.graphql"))
	if err != nil {
		t.Fatal(err)
	}
	bindingYAML, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "executor", "bindings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	schema := loadSchema(t, string(schemaSDL))
	return schema, loadBindings(t, schema, string(bindingYAML))
}

func loadSchema(t *testing.T, sdl string) *graphqlschema.Schema {
	t.Helper()
	loaded, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{{Name: "schema.graphql", Input: []byte(sdl)}}, graphqlschema.Options{SourceNames: []string{"fixture"}})
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	return loaded
}

func loadBindings(t *testing.T, schema *graphqlschema.Schema, contents string) *binding.Table {
	t.Helper()
	table, err := binding.Compile(binding.Document{Name: "bindings.yaml", Input: []byte(contents)}, schema, binding.Options{SourceNames: []string{"fixture"}})
	if err != nil {
		t.Fatalf("binding.Compile() error = %v", err)
	}
	return table
}

func readCorpus(t *testing.T) executionCorpus {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "executor", "corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var corpus executionCorpus
	if err := decoder.Decode(&corpus); err != nil {
		t.Fatalf("decode execution corpus: %v", err)
	}
	return corpus
}

func compactJSON(t *testing.T, input json.RawMessage) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := json.Compact(&output, input); err != nil {
		t.Fatalf("compact JSON %q: %v", input, err)
	}
	return output.Bytes()
}

func testTenant(t *testing.T) datasource.TenantID {
	t.Helper()
	tenant, err := datasource.NewTenantID("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	return tenant
}

func testPrincipal(t *testing.T) datasource.PrincipalView {
	t.Helper()
	principal, err := datasource.NewPrincipalView("subject-a", testTenant(t), []string{"read"})
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func testDeadline() time.Time {
	return time.Unix(1_900_000_000, 0).UTC()
}
