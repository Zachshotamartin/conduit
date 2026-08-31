package schema_test

import (
	"encoding/json"
	stderrors "errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
	graphqlschema "github.com/Zachshotamartin/conduit/internal/graphql/schema"
)

type fixtureManifest struct {
	Valid   []string             `json:"valid"`
	Invalid []invalidFixtureCase `json:"invalid"`
}

type invalidFixtureCase struct {
	File string `json:"file"`
	Rule string `json:"rule"`
}

var schemaOptions = graphqlschema.Options{
	SourceNames: []string{"db", "http"},
	AuthRules:   []string{"allow", "same-tenant-only"},
}

func TestUNIT002_OneInvalidRulePerSDLFixture(t *testing.T) {
	manifest := readManifest(t)
	if len(manifest.Invalid) < 35 {
		t.Fatalf("invalid fixture count = %d, want comprehensive corpus", len(manifest.Invalid))
	}

	seenFiles := make(map[string]struct{}, len(manifest.Invalid))
	seenRules := make(map[string]struct{}, len(manifest.Invalid))
	for _, fixtureCase := range manifest.Invalid {
		if _, duplicate := seenFiles[fixtureCase.File]; duplicate {
			t.Fatalf("fixture %q appears twice", fixtureCase.File)
		}
		seenFiles[fixtureCase.File] = struct{}{}
		seenRules[fixtureCase.Rule] = struct{}{}
	}

	for _, fixtureCase := range manifest.Invalid {
		fixtureCase := fixtureCase
		t.Run(strings.TrimSuffix(filepath.Base(fixtureCase.File), ".graphql"), func(t *testing.T) {
			t.Parallel()
			input := fixture(t, fixtureCase.File)
			loaded, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{{
				Name:  fixtureCase.File,
				Input: input,
			}}, schemaOptions)
			if loaded != nil {
				t.Fatal("LoadSources(invalid fixture) returned partial schema")
			}
			diagnostics := diagnosticsFrom(t, err).Items()
			if len(diagnostics) != 1 {
				t.Fatalf("diagnostic count = %d, want exactly 1: %v", len(diagnostics), err)
			}
			got := diagnostics[0]
			if got.Rule != fixtureCase.Rule {
				t.Fatalf("diagnostic rule = %q, want %q", got.Rule, fixtureCase.Rule)
			}
			if got.File != fixtureCase.File {
				t.Fatalf("diagnostic file = %q, want %q", got.File, fixtureCase.File)
			}
			if got.Line < 1 || got.Column < 1 {
				t.Fatalf("diagnostic location = %d:%d, want positive", got.Line, got.Column)
			}
			if !strings.Contains(err.Error(), fixtureCase.Rule) {
				t.Fatalf("operator error %q does not name rule %q", err, fixtureCase.Rule)
			}
		})
	}

	requiredFamilies := []string{
		"graphql.sdl.",
		"graphql.directive.",
		"conduit.source.",
		"conduit.auth.",
		"conduit.filterable.",
		"conduit.backpressure.",
		"conduit.complexity.",
		"conduit.spec_version.",
	}
	for _, family := range requiredFamilies {
		found := false
		for rule := range seenRules {
			if strings.HasPrefix(rule, family) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("fixture corpus has no rule in family %q", family)
		}
	}
}

func TestUNIT002_ValidSDLFixturesLoad(t *testing.T) {
	manifest := readManifest(t)
	for _, name := range manifest.Valid {
		name := name
		t.Run(strings.TrimSuffix(filepath.Base(name), ".graphql"), func(t *testing.T) {
			t.Parallel()
			loaded, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{{
				Name:  name,
				Input: fixture(t, name),
			}}, schemaOptions)
			if err != nil {
				t.Fatalf("LoadSources(valid fixture) error = %v", err)
			}
			if loaded == nil || loaded.Executable() == nil {
				t.Fatal("LoadSources(valid fixture) returned nil schema")
			}
			if loaded.Hash().String() == "" {
				t.Fatal("valid schema hash is empty")
			}
		})
	}
}

func TestUNIT002_CollectsAllIndependentSemanticErrors(t *testing.T) {
	t.Parallel()

	names := []string{
		"aggregate/base.graphql",
		"aggregate/auth.graphql",
		"aggregate/backpressure.graphql",
		"aggregate/complexity.graphql",
		"aggregate/filterable.graphql",
		"aggregate/source.graphql",
	}
	sources := make([]graphqlast.SchemaSource, 0, len(names))
	for _, name := range names {
		sources = append(sources, graphqlast.SchemaSource{Name: name, Input: fixture(t, name)})
	}
	loaded, err := graphqlschema.LoadSources(sources, schemaOptions)
	if loaded != nil {
		t.Fatal("LoadSources(aggregate invalid set) returned partial schema")
	}
	items := diagnosticsFrom(t, err).Items()
	if len(items) != 5 {
		t.Fatalf("diagnostic count = %d, want 5: %v", len(items), err)
	}
	want := []string{
		"conduit.auth.rule_undefined",
		"conduit.backpressure.queue_positive",
		"conduit.complexity.cost_nonnegative",
		"conduit.filterable.supported_scalar",
		"conduit.source.name_unknown",
	}
	got := make([]string, len(items))
	for index, item := range items {
		got[index] = item.Rule
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostic rules = %v, want %v", got, want)
	}
}

func TestUNIT002_HashIsSemanticAndStable(t *testing.T) {
	t.Parallel()

	first := []graphqlast.SchemaSource{
		{Name: "types.graphql", Input: fixture(t, "hash/a-types.graphql")},
		{Name: "schema.graphql", Input: fixture(t, "hash/a-schema.graphql")},
	}
	second := []graphqlast.SchemaSource{
		{Name: "schema-b.graphql", Input: fixture(t, "hash/b-schema.graphql")},
		{Name: "types-b.graphql", Input: fixture(t, "hash/b-types.graphql")},
	}
	a, err := graphqlschema.LoadSources(first, schemaOptions)
	if err != nil {
		t.Fatalf("LoadSources(hash A) error = %v", err)
	}
	b, err := graphqlschema.LoadSources(second, schemaOptions)
	if err != nil {
		t.Fatalf("LoadSources(hash B) error = %v", err)
	}
	if a.Hash() != b.Hash() {
		t.Fatalf("equivalent schema hashes differ: %s != %s", a.Hash(), b.Hash())
	}

	reversed := append([]graphqlast.SchemaSource(nil), first...)
	sort.Slice(reversed, func(i, j int) bool { return reversed[i].Name > reversed[j].Name })
	c, err := graphqlschema.LoadSources(reversed, schemaOptions)
	if err != nil {
		t.Fatalf("LoadSources(reversed hash input) error = %v", err)
	}
	if a.Hash() != c.Hash() {
		t.Fatalf("file order changed hash: %s != %s", a.Hash(), c.Hash())
	}

	changedInput := append([]byte(nil), fixture(t, "hash/a-schema.graphql")...)
	changedInput = []byte(strings.Replace(string(changedInput), "cost: 2", "cost: 3", 1))
	changed, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{
		{Name: "types.graphql", Input: fixture(t, "hash/a-types.graphql")},
		{Name: "schema.graphql", Input: changedInput},
	}, schemaOptions)
	if err != nil {
		t.Fatalf("LoadSources(changed hash input) error = %v", err)
	}
	if a.Hash() == changed.Hash() {
		t.Fatalf("semantic directive change did not change hash %s", a.Hash())
	}
}

func TestUNIT002_ServingMetadataIsImmutableAndComplete(t *testing.T) {
	t.Parallel()

	input := fixture(t, "valid/all-directives.graphql")
	loaded, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{{
		Name: "valid/all-directives.graphql", Input: input,
	}}, schemaOptions)
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	originalHash := loaded.Hash()

	field, ok := loaded.Field("Query", "order")
	if !ok {
		t.Fatal("Field(Query.order) not found")
	}
	if !field.HasSource || field.SourceName != "db" {
		t.Fatalf("Query.order source = (%v, %q), want (true, db)", field.HasSource, field.SourceName)
	}
	if !field.HasAuth || field.AuthRule != "allow" {
		t.Fatalf("Query.order auth = (%v, %q), want (true, allow)", field.HasAuth, field.AuthRule)
	}
	if field.Complexity.Cost != 2 || !reflect.DeepEqual(field.Complexity.Multipliers, []string{"id"}) {
		t.Fatalf("Query.order complexity = %#v", field.Complexity)
	}

	field.Complexity.Multipliers[0] = "mutated"
	again, _ := loaded.Field("Query", "order")
	if got := again.Complexity.Multipliers[0]; got != "id" {
		t.Fatalf("metadata mutation escaped into schema: %q", got)
	}
	input[0] = 'X'
	if loaded.Hash() != originalHash {
		t.Fatal("caller input mutation changed schema hash")
	}

	argument, ok := loaded.Argument("Subscription", "orderUpdated", "region")
	if !ok || !argument.Filterable || argument.Scalar != graphqlschema.ScalarString {
		t.Fatalf("filterable argument = %#v, %v", argument, ok)
	}
	subscription, ok := loaded.Field("Subscription", "orderUpdated")
	if !ok {
		t.Fatal("Field(Subscription.orderUpdated) not found")
	}
	if !subscription.Backpressure.Present ||
		subscription.Backpressure.Policy != graphqlschema.BackpressureCoalesceByKey ||
		subscription.Backpressure.Queue != 4 ||
		subscription.Backpressure.CoalesceKey != "id" {
		t.Fatalf("subscription backpressure = %#v", subscription.Backpressure)
	}
}

func TestUNIT002_LoadFilesUsesLogicalNameAndNeverLeaksPhysicalPath(t *testing.T) {
	t.Parallel()

	physical := filepath.Join(t.TempDir(), "private", "schema.graphql")
	if err := os.MkdirAll(filepath.Dir(physical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(physical, []byte("type Query { broken: Missing }"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := graphqlschema.LoadFiles([]graphqlschema.File{{
		Path: physical,
		Name: "operator/schema.graphql",
	}}, schemaOptions)
	if loaded != nil {
		t.Fatal("LoadFiles(invalid schema) returned partial schema")
	}
	diagnostics := diagnosticsFrom(t, err).Items()
	if len(diagnostics) != 1 || diagnostics[0].File != "operator/schema.graphql" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if strings.Contains(err.Error(), filepath.Dir(physical)) {
		t.Fatalf("operator error leaks physical path: %v", err)
	}
}

func diagnosticsFrom(t *testing.T, err error) *graphqlast.Diagnostics {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want diagnostics")
	}
	var diagnostics *graphqlast.Diagnostics
	if !stderrors.As(err, &diagnostics) {
		t.Fatalf("error type = %T, want *ast.Diagnostics", err)
	}
	return diagnostics
}

func readManifest(t *testing.T) fixtureManifest {
	t.Helper()
	contents := fixture(t, "manifest.json")
	var manifest fixtureManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		t.Fatalf("decode SDL fixture manifest: %v", err)
	}
	return manifest
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "sdl", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return contents
}
