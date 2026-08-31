package binding_test

import (
	"encoding/json"
	stderrors "errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Zachshotamartin/conduit/internal/datasource"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
	"github.com/Zachshotamartin/conduit/internal/graphql/binding"
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

var sourceOptions = binding.Options{SourceNames: []string{"analytics", "users"}}

func TestUNIT005_OneInvalidRulePerBindingFixture(t *testing.T) {
	t.Parallel()
	manifest := readManifest(t)
	if len(manifest.Invalid) < 20 {
		t.Fatalf("invalid fixture count = %d, want comprehensive corpus", len(manifest.Invalid))
	}
	schema := loadSchema(t, "schema.graphql", `type Query { ok: Boolean @source(name: "users") }`)
	seen := make(map[string]struct{}, len(manifest.Invalid))
	for _, fixtureCase := range manifest.Invalid {
		fixtureCase := fixtureCase
		if _, duplicate := seen[fixtureCase.Rule]; duplicate {
			t.Fatalf("rule %q has more than one invalid fixture", fixtureCase.Rule)
		}
		seen[fixtureCase.Rule] = struct{}{}
		t.Run(strings.TrimSuffix(filepath.Base(fixtureCase.File), ".yaml"), func(t *testing.T) {
			t.Parallel()
			table, err := binding.Compile(binding.Document{
				Name: fixtureCase.File, Input: fixture(t, fixtureCase.File),
			}, schema, sourceOptions)
			if table != nil {
				t.Fatal("Compile(invalid fixture) returned a partial table")
			}
			items := diagnosticsFrom(t, err).Items()
			if len(items) != 1 || items[0].Rule != fixtureCase.Rule {
				t.Fatalf("diagnostics = %#v, want only %q: %v", items, fixtureCase.Rule, err)
			}
			if items[0].File != fixtureCase.File || items[0].Line < 1 || items[0].Column < 1 {
				t.Fatalf("diagnostic location = %#v, want logical fixture location", items[0])
			}
		})
	}
}

func TestUNIT005_BindingInputIsBoundedStrictAndUnforgeable(t *testing.T) {
	t.Parallel()
	schema := loadSchema(t, "schema.graphql", `type Query { ok: Boolean @source(name: "users") }`)
	invalidUTF8 := []byte{'b', 'i', 'n', 'd', 'i', 'n', 'g', 's', ':', ' ', 0xff}
	overBound := append([]byte("bindings: []\n#"), make([]byte, binding.MaxDocumentBytes)...)
	for _, tc := range []struct {
		name     string
		document binding.Document
		rule     string
	}{
		{name: "invalid UTF-8", document: binding.Document{Name: "invalid-utf8.yaml", Input: invalidUTF8}, rule: "binding.document.utf8"},
		{name: "byte bound", document: binding.Document{Name: "oversized.yaml", Input: overBound}, rule: "binding.document.bytes"},
		{name: "logical name", document: binding.Document{Name: "../private.yaml", Input: []byte("bindings: []\n")}, rule: "binding.document.logical_name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			table, err := binding.Compile(tc.document, schema, sourceOptions)
			if table != nil {
				t.Fatal("Compile() returned partial table")
			}
			items := diagnosticsFrom(t, err).Items()
			if len(items) != 1 || items[0].Rule != tc.rule {
				t.Fatalf("diagnostics = %#v, want %q", items, tc.rule)
			}
		})
	}

	for _, tc := range []struct {
		name    string
		schema  *graphqlschema.Schema
		options binding.Options
		rule    string
	}{
		{name: "nil schema", options: sourceOptions, rule: "binding.schema.unavailable"},
		{name: "empty source name", schema: schema, options: binding.Options{SourceNames: []string{""}}, rule: "binding.source.registry"},
		{name: "duplicate source name", schema: schema, options: binding.Options{SourceNames: []string{"users", "users"}}, rule: "binding.source.registry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			table, err := binding.Compile(binding.Document{
				Name: "valid/simple.yaml", Input: fixture(t, "valid/simple.yaml"),
			}, tc.schema, tc.options)
			if table != nil {
				t.Fatal("Compile() returned partial table")
			}
			items := diagnosticsFrom(t, err).Items()
			if len(items) != 1 || items[0].Rule != tc.rule {
				t.Fatalf("diagnostics = %#v, want %q", items, tc.rule)
			}
		})
	}
}

func TestUNIT005_ValidBindingClosureIsCompleteImmutableAndSchemaAnchored(t *testing.T) {
	t.Parallel()
	schema := loadSchema(t, "schema.graphql", string(fixture(t, "schema.graphql")))
	input := fixture(t, "valid/full.yaml")
	table, err := binding.Compile(binding.Document{Name: "valid/full.yaml", Input: input}, schema, sourceOptions)
	if err != nil {
		t.Fatalf("Compile(valid closure) error = %v", err)
	}
	if table == nil || table.Len() != 10 {
		t.Fatalf("table = %#v, len = %d, want 10", table, table.Len())
	}
	if table.SchemaAnchor() == (graphqlast.SchemaAnchor{}) || table.SchemaAnchor() != schema.Anchor() {
		t.Fatal("binding table does not retain the unforgeable serving-schema anchor")
	}
	if !strings.HasPrefix(table.Hash().String(), "sha256:") {
		t.Fatalf("binding hash = %q, want algorithm-qualified SHA-256", table.Hash().String())
	}

	viewer := mustField(t, "Query.viewer")
	viewerBinding, ok := table.Lookup(viewer)
	if !ok || viewerBinding.Kind != binding.Source || viewerBinding.SourceName != "users" || viewerBinding.ParentPath != nil {
		t.Fatalf("Query.viewer binding = %#v, %v", viewerBinding, ok)
	}
	profile := mustField(t, "User.profile")
	profileBinding, ok := table.Lookup(profile)
	if !ok || profileBinding.Kind != binding.Parent || !reflect.DeepEqual(profileBinding.ParentPath, []string{"profile"}) {
		t.Fatalf("User.profile binding = %#v, %v", profileBinding, ok)
	}
	profileBinding.ParentPath[0] = "mutated"
	again, _ := table.Lookup(profile)
	if !reflect.DeepEqual(again.ParentPath, []string{"profile"}) {
		t.Fatalf("Lookup() aliases table state: %v", again.ParentPath)
	}
	subscription := mustField(t, "Subscription.userChanged")
	subscriptionBinding, ok := table.Lookup(subscription)
	if !ok || subscriptionBinding.Kind != binding.Parent || subscriptionBinding.ParentPath == nil || len(subscriptionBinding.ParentPath) != 0 {
		t.Fatalf("identity parent projection = %#v, %v", subscriptionBinding, ok)
	}
	input[0] = 'X'
	if table.Len() != 10 {
		t.Fatal("caller input mutation changed binding table")
	}
}

func TestUNIT005_BindingHashIsSemanticStableAndIncludesSchema(t *testing.T) {
	t.Parallel()
	schema := loadSchema(t, "schema.graphql", string(fixture(t, "schema.graphql")))
	compile := func(name string, input []byte) *binding.Table {
		t.Helper()
		table, err := binding.Compile(binding.Document{Name: name, Input: input}, schema, sourceOptions)
		if err != nil {
			t.Fatalf("Compile(%s) error = %v", name, err)
		}
		return table
	}
	canonical := compile("valid/full.yaml", fixture(t, "valid/full.yaml"))
	reordered := compile("valid/full-reordered.yaml", fixture(t, "valid/full-reordered.yaml"))
	if canonical.Hash() != reordered.Hash() {
		t.Fatalf("equivalent documents have different hashes: %s != %s", canonical.Hash(), reordered.Hash())
	}
	changedInput := []byte(strings.Replace(
		string(fixture(t, "valid/full.yaml")), "parent: [display_name]", "parent: [name]", 1,
	))
	changed := compile("valid/changed.yaml", changedInput)
	if canonical.Hash() == changed.Hash() {
		t.Fatalf("parent projection change did not change hash %s", canonical.Hash())
	}

	changedSchemaSDL := strings.Replace(string(fixture(t, "schema.graphql")), "displayName: String!", "displayName: String", 1)
	changedSchema := loadSchema(t, "schema.graphql", changedSchemaSDL)
	changedSchemaTable, err := binding.Compile(
		binding.Document{Name: "valid/full.yaml", Input: fixture(t, "valid/full.yaml")},
		changedSchema,
		sourceOptions,
	)
	if err != nil {
		t.Fatalf("Compile(changed schema) error = %v", err)
	}
	if canonical.Hash() == changedSchemaTable.Hash() {
		t.Fatalf("schema change did not change binding hash %s", canonical.Hash())
	}
}

func TestUNIT005_MissingAndOrphanBindingsNameBothLocations(t *testing.T) {
	t.Parallel()
	schema := loadSchema(t, "schema.graphql", string(fixture(t, "schema.graphql")))
	for _, tc := range []struct {
		name        string
		file        string
		rule        string
		primaryFile string
		relatedFile string
	}{
		{name: "missing", file: "invalid/missing.yaml", rule: "binding.schema.missing", primaryFile: "schema.graphql", relatedFile: "invalid/missing.yaml"},
		{name: "orphan", file: "invalid/orphan.yaml", rule: "binding.schema.orphan", primaryFile: "invalid/orphan.yaml", relatedFile: "schema.graphql"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			table, err := binding.Compile(binding.Document{Name: tc.file, Input: fixture(t, tc.file)}, schema, sourceOptions)
			if table != nil {
				t.Fatal("Compile() returned partial table")
			}
			items := diagnosticsFrom(t, err).Items()
			if len(items) != 1 || items[0].Rule != tc.rule {
				t.Fatalf("diagnostics = %#v, want only %q: %v", items, tc.rule, err)
			}
			item := items[0]
			if item.File != tc.primaryFile || item.Line < 1 || item.Column < 1 || len(item.Related) != 1 {
				t.Fatalf("primary/related locations = %#v", item)
			}
			if item.Related[0].File != tc.relatedFile || item.Related[0].Line < 1 || item.Related[0].Column < 1 {
				t.Fatalf("related location = %#v", item.Related[0])
			}
			if !strings.Contains(err.Error(), tc.primaryFile) || !strings.Contains(err.Error(), tc.relatedFile) {
				t.Fatalf("operator error does not name both locations: %v", err)
			}
		})
	}
}

func TestUNIT005_CrossValidationFailsClosedWithStableRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		sdl     string
		config  string
		options binding.Options
		rule    string
	}{
		{
			name: "unknown configured source",
			sdl:  `type Query { ok: Boolean @source(name: "users") }`,
			config: `bindings:
  - field: Query.ok
    source: missing
`,
			options: sourceOptions,
			rule:    "binding.source.unknown",
		},
		{
			name: "source differs from SDL",
			sdl:  `type Query { ok: Boolean @source(name: "users") }`,
			config: `bindings:
  - field: Query.ok
    source: analytics
`,
			options: sourceOptions,
			rule:    "binding.source.mismatch",
		},
		{
			name: "nested source lacks SDL annotation",
			sdl:  `type Query { item: Item @source(name: "users") } type Item { value: String }`,
			config: `bindings:
  - field: Query.item
    source: users
  - field: Item.value
    source: users
`,
			options: sourceOptions,
			rule:    "binding.source.annotation_missing",
		},
		{
			name: "parent conflicts with SDL source",
			sdl:  `type Query { item: Item @source(name: "users") } type Item { value: String @source(name: "analytics") }`,
			config: `bindings:
  - field: Query.item
    source: users
  - field: Item.value
    parent: [value]
`,
			options: sourceOptions,
			rule:    "binding.parent.annotation_conflict",
		},
		{
			name: "query root must dispatch",
			sdl:  `type Query { ok: Boolean }`,
			config: `bindings:
  - field: Query.ok
    parent: []
`,
			options: sourceOptions,
			rule:    "binding.root.source_required",
		},
		{
			name: "subscription closure must project",
			sdl:  `type Query { ok: Boolean @source(name: "users") } type Subscription { changed: Boolean }`,
			config: `bindings:
  - field: Query.ok
    source: users
  - field: Subscription.changed
    source: users
`,
			options: sourceOptions,
			rule:    "binding.subscription.parent_required",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			schema := loadSchemaWithSources(t, "schema.graphql", tc.sdl, tc.options.SourceNames)
			table, err := binding.Compile(
				binding.Document{Name: "bindings.yaml", Input: []byte(tc.config)}, schema, tc.options,
			)
			if table != nil {
				t.Fatal("Compile(invalid cross reference) returned partial table")
			}
			items := diagnosticsFrom(t, err).Items()
			if len(items) != 1 || items[0].Rule != tc.rule {
				t.Fatalf("diagnostics = %#v, want only %q: %v", items, tc.rule, err)
			}
		})
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

func loadSchema(t testing.TB, name, sdl string) *graphqlschema.Schema {
	t.Helper()
	return loadSchemaWithSources(t, name, sdl, sourceOptions.SourceNames)
}

func loadSchemaWithSources(t testing.TB, name, sdl string, sourceNames []string) *graphqlschema.Schema {
	t.Helper()
	loaded, err := graphqlschema.LoadSources([]graphqlast.SchemaSource{{Name: name, Input: []byte(sdl)}}, graphqlschema.Options{
		SourceNames: sourceNames,
	})
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	return loaded
}

func mustField(t *testing.T, coordinate string) datasource.FieldRef {
	t.Helper()
	field, err := datasource.ParseFieldRef(coordinate)
	if err != nil {
		t.Fatalf("ParseFieldRef(%q) error = %v", coordinate, err)
	}
	return field
}

func readManifest(t *testing.T) fixtureManifest {
	t.Helper()
	var manifest fixtureManifest
	if err := json.Unmarshal(fixture(t, "manifest.json"), &manifest); err != nil {
		t.Fatalf("decode binding fixture manifest: %v", err)
	}
	return manifest
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "bindings", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return contents
}
