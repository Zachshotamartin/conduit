package binding

import (
	"crypto/sha256"
	"encoding/json"
	"sort"

	"github.com/Zachshotamartin/conduit/internal/datasource"
	graphqlschema "github.com/Zachshotamartin/conduit/internal/graphql/schema"
)

type hashDocument struct {
	Version  string        `json:"version"`
	Schema   string        `json:"schema"`
	Bindings []hashBinding `json:"bindings"`
}

type hashBinding struct {
	Field      string   `json:"field"`
	Kind       Kind     `json:"kind"`
	SourceName string   `json:"source,omitempty"`
	ParentPath []string `json:"parent,omitempty"`
}

func semanticHash(schemaHash graphqlschema.Hash, entries map[datasource.FieldRef]Binding) Hash {
	fields := make([]datasource.FieldRef, 0, len(entries))
	for field := range entries {
		fields = append(fields, field)
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].String() < fields[j].String() })
	document := hashDocument{
		Version: "conduit.binding/v1",
		Schema:  schemaHash.String(),
	}
	for _, field := range fields {
		entry := entries[field]
		parent := cloneParentPath(entry.ParentPath)
		if entry.Kind == Parent && parent == nil {
			parent = []string{}
		}
		document.Bindings = append(document.Bindings, hashBinding{
			Field: field.String(), Kind: entry.Kind, SourceName: entry.SourceName, ParentPath: parent,
		})
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		panic("binding: semantic hash encoding failed: " + err.Error())
	}
	return sha256.Sum256(encoded)
}
