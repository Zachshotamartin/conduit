package binding_test

import (
	"testing"

	"github.com/Zachshotamartin/conduit/internal/graphql/binding"
)

func FuzzBindingDocument(f *testing.F) {
	schema := loadSchema(f, "schema.graphql", `type Query { ok: Boolean @source(name: "users") }`)
	f.Add([]byte("bindings:\n  - field: Query.ok\n    source: users\n"))
	f.Add([]byte("bindings:\n  - field: Query.ok\n    source: users\n  - field: Query.ok\n    source: users\n"))
	f.Fuzz(func(t *testing.T, input []byte) {
		table, err := binding.Compile(binding.Document{Name: "fuzz.yaml", Input: input}, schema, sourceOptions)
		if err != nil {
			if table != nil {
				t.Fatal("rejected input returned partial binding table")
			}
			return
		}
		if table == nil || table.Len() != 1 || table.SchemaAnchor() != schema.Anchor() {
			t.Fatalf("accepted input returned invalid table: %#v", table)
		}
	})
}
