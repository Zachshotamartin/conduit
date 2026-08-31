package datasource_test

import (
	"encoding/json"
	"testing"

	"github.com/Zachshotamartin/conduit/internal/datasource"
)

func FuzzFieldRef(f *testing.F) {
	f.Add("Query.viewer")
	f.Add("invalid")
	f.Fuzz(func(t *testing.T, input string) {
		field, err := datasource.ParseFieldRef(input)
		if err != nil {
			if field.Valid() {
				t.Fatal("rejected FieldRef is valid")
			}
			return
		}
		if !field.Valid() || field.String() != input {
			t.Fatalf("accepted FieldRef does not round-trip: %q -> %q", input, field.String())
		}
	})
}

func FuzzArgumentValues(f *testing.F) {
	f.Add([]byte(`{"b":2,"a":1}`))
	f.Add([]byte(`{"nested":{"x":1,"x":2}}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		arguments, err := datasource.NewArgumentValues(json.RawMessage(input))
		if err != nil {
			return
		}
		canonical := arguments.CanonicalJSON()
		reparsed, err := datasource.NewArgumentValues(canonical)
		if err != nil || !arguments.Equal(reparsed) {
			t.Fatalf("canonical reparse = (%v, %v), JSON %q", reparsed, err, canonical)
		}
	})
}
