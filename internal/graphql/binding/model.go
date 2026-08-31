package binding

import (
	"encoding/hex"
	"sort"

	"github.com/Zachshotamartin/conduit/internal/datasource"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
)

// MaxDocumentBytes is the hard pre-parse bound for one binding document.
const MaxDocumentBytes = 1 << 20

// Document is one in-memory binding configuration with an operator-facing
// logical name. Input is copied into immutable compiled values.
type Document struct {
	Name  string
	Input []byte
}

// Options supplies the exact configured data-source registry.
type Options struct {
	SourceNames []string
}

// Kind identifies the only two resolver targets admitted by a binding.
type Kind string

const (
	// Source dispatches a field to the named DataSource.
	Source Kind = "source"
	// Parent projects a literal object-key path from the immediate parent.
	Parent Kind = "parent"
)

// Binding is one immutable-by-copy resolver decision.
type Binding struct {
	Field      datasource.FieldRef
	Kind       Kind
	SourceName string
	ParentPath []string
}

// Hash is the semantic SHA-256 of a schema-bound resolver table.
type Hash [32]byte

// String renders the algorithm-qualified digest used by logs and admin APIs.
func (hash Hash) String() string {
	return "sha256:" + hex.EncodeToString(hash[:])
}

// Table is the complete immutable resolver table for exactly one serving
// schema identity.
type Table struct {
	anchor  graphqlast.SchemaAnchor
	hash    Hash
	entries map[datasource.FieldRef]Binding
}

// Len returns the number of operation-reachable bound fields.
func (table *Table) Len() int {
	if table == nil {
		return 0
	}
	return len(table.entries)
}

// SchemaAnchor returns the unforgeable schema identity this table compiled
// against.
func (table *Table) SchemaAnchor() graphqlast.SchemaAnchor {
	if table == nil {
		return graphqlast.SchemaAnchor{}
	}
	return table.anchor
}

// Hash returns the semantic schema-and-binding digest.
func (table *Table) Hash() Hash {
	if table == nil {
		return Hash{}
	}
	return table.hash
}

// Lookup returns a defensive copy of a field binding.
func (table *Table) Lookup(field datasource.FieldRef) (Binding, bool) {
	if table == nil || !field.Valid() {
		return Binding{}, false
	}
	entry, ok := table.entries[field]
	if !ok {
		return Binding{}, false
	}
	entry.ParentPath = cloneParentPath(entry.ParentPath)
	return entry, true
}

// SourceNames returns the sorted unique adapter names referenced by this
// complete table.
func (table *Table) SourceNames() []string {
	if table == nil {
		return nil
	}
	set := make(map[string]struct{})
	for _, entry := range table.entries {
		if entry.Kind == Source {
			set[entry.SourceName] = struct{}{}
		}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func cloneParentPath(path []string) []string {
	if path == nil {
		return nil
	}
	return append([]string{}, path...)
}
