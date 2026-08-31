package binding

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Zachshotamartin/conduit/internal/datasource"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
	graphqlschema "github.com/Zachshotamartin/conduit/internal/graphql/schema"
)

type reachability uint8

const (
	reachableQuery reachability = 1 << iota
	reachableMutation
	reachableSubscription
)

type fieldInfo struct {
	definition   graphqlast.FieldDefinition
	reachable    reachability
	dispatchRoot bool
}

// Compile strictly parses document, cross-validates it against every
// operation-reachable field, and publishes no partial table on any error.
func Compile(document Document, serving *graphqlschema.Schema, options Options) (*Table, error) {
	if serving == nil || serving.Anchor() == (graphqlast.SchemaAnchor{}) {
		return nil, graphqlast.NewDiagnostics(graphqlast.Diagnostic{
			Rule: "binding.schema.unavailable", File: document.Name, Line: 1, Column: 1,
			Message: "binding compilation requires a validated serving schema",
		})
	}
	knownSources, diagnostics := validateOptions(document.Name, options)
	if len(diagnostics) > 0 {
		return nil, graphqlast.NewDiagnostics(diagnostics...)
	}
	parsed, diagnostics := parse(document)
	if len(diagnostics) > 0 {
		return nil, graphqlast.NewDiagnostics(diagnostics...)
	}

	snapshot := serving.Snapshot()
	fields, fallback := reachableFields(snapshot)
	provided := make(map[datasource.FieldRef]struct{}, len(parsed.entries))
	entries := make(map[datasource.FieldRef]Binding, len(parsed.entries))
	for _, entry := range parsed.entries {
		provided[entry.binding.Field] = struct{}{}
		info, exists := fields[entry.binding.Field]
		if !exists || info.reachable == 0 {
			relatedPosition := fallback
			relatedMessage := "operation reachability starts here"
			if exists {
				relatedPosition = info.definition.Position
				relatedMessage = "field is declared here but is unreachable from every operation root"
			}
			diagnostics = append(diagnostics, crossDiagnostic(
				"binding.schema.orphan", entry.fieldPosition,
				"binding coordinate is not reachable from an operation root",
				relatedPosition, relatedMessage,
			))
			continue
		}

		if diagnostic, invalid := validateTarget(entry, info, serving, knownSources); invalid {
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		compiled := entry.binding
		compiled.ParentPath = cloneParentPath(compiled.ParentPath)
		entries[compiled.Field] = compiled
	}

	reachableCoordinates := make([]datasource.FieldRef, 0, len(fields))
	for field, info := range fields {
		if info.reachable != 0 {
			reachableCoordinates = append(reachableCoordinates, field)
		}
	}
	sort.Slice(reachableCoordinates, func(i, j int) bool {
		return reachableCoordinates[i].String() < reachableCoordinates[j].String()
	})
	for _, field := range reachableCoordinates {
		if _, ok := provided[field]; ok {
			continue
		}
		info := fields[field]
		diagnostics = append(diagnostics, crossDiagnostic(
			"binding.schema.missing", info.definition.Position,
			fmt.Sprintf("operation-reachable field %s has no binding", field.String()),
			parsed.rootPosition, "binding document is declared here",
		))
	}
	if len(diagnostics) > 0 {
		return nil, graphqlast.NewDiagnostics(diagnostics...)
	}

	table := &Table{anchor: serving.Anchor(), entries: entries}
	table.hash = semanticHash(serving.Hash(), entries)
	return table, nil
}

func validateOptions(file string, options Options) (map[string]struct{}, []graphqlast.Diagnostic) {
	known := make(map[string]struct{}, len(options.SourceNames))
	for _, name := range options.SourceNames {
		if strings.TrimSpace(name) == "" {
			return nil, one("binding.source.registry", file, 1, 1,
				"configured source names must be nonempty")
		}
		if _, duplicate := known[name]; duplicate {
			return nil, one("binding.source.registry", file, 1, 1,
				"configured source names must be unique")
		}
		known[name] = struct{}{}
	}
	return known, nil
}

func validateTarget(
	entry parsedEntry,
	info fieldInfo,
	serving *graphqlschema.Schema,
	knownSources map[string]struct{},
) (graphqlast.Diagnostic, bool) {
	metadata, _ := serving.Field(entry.binding.Field.ParentType(), entry.binding.Field.Field())
	directivePosition := sourceDirectivePosition(info.definition)

	if entry.binding.Kind == Source {
		if _, known := knownSources[entry.binding.SourceName]; !known {
			return crossDiagnostic(
				"binding.source.unknown", entry.targetPosition,
				"binding source is not configured", info.definition.Position, "bound field is declared here",
			), true
		}
		if info.reachable&reachableSubscription != 0 {
			return crossDiagnostic(
				"binding.subscription.parent_required", entry.targetPosition,
				"subscription-reachable fields must use parent projection",
				info.definition.Position, "subscription-reachable field is declared here",
			), true
		}
		if !metadata.HasSource {
			return crossDiagnostic(
				"binding.source.annotation_missing", entry.targetPosition,
				"source binding requires a matching SDL @source annotation",
				info.definition.Position, "field without @source is declared here",
			), true
		}
		if metadata.SourceName != entry.binding.SourceName {
			return crossDiagnostic(
				"binding.source.mismatch", entry.targetPosition,
				"binding source differs from the SDL @source name",
				directivePosition, "SDL @source is declared here",
			), true
		}
		return graphqlast.Diagnostic{}, false
	}

	if info.dispatchRoot {
		return crossDiagnostic(
			"binding.root.source_required", entry.targetPosition,
			"query and mutation root fields must use a source binding",
			info.definition.Position, "operation root field is declared here",
		), true
	}
	if metadata.HasSource {
		return crossDiagnostic(
			"binding.parent.annotation_conflict", entry.targetPosition,
			"parent projection conflicts with the SDL @source annotation",
			directivePosition, "SDL @source is declared here",
		), true
	}
	return graphqlast.Diagnostic{}, false
}

func reachableFields(snapshot graphqlast.SchemaSnapshot) (map[datasource.FieldRef]fieldInfo, graphqlast.SourcePosition) {
	types := make(map[string]graphqlast.TypeDefinition, len(snapshot.Types))
	implementors := make(map[string][]string)
	fields := make(map[datasource.FieldRef]fieldInfo)
	for _, definition := range snapshot.Types {
		types[definition.Name] = definition
		for _, interfaceName := range definition.Interfaces {
			implementors[interfaceName] = append(implementors[interfaceName], definition.Name)
		}
		for _, fieldDefinition := range definition.Fields {
			field, err := datasource.NewFieldRef(definition.Name, fieldDefinition.Name)
			if err == nil {
				fields[field] = fieldInfo{
					definition:   fieldDefinition,
					dispatchRoot: definition.Name == snapshot.Query || definition.Name == snapshot.Mutation,
				}
			}
		}
	}
	for name := range implementors {
		sort.Strings(implementors[name])
	}

	type work struct {
		name string
		bits reachability
	}
	var queue []work
	typeReach := make(map[string]reachability)
	processed := make(map[string]reachability)
	add := func(name string, bits reachability) {
		if name == "" {
			return
		}
		newBits := bits &^ typeReach[name]
		if newBits == 0 {
			return
		}
		typeReach[name] |= newBits
		queue = append(queue, work{name: name, bits: newBits})
	}
	add(snapshot.Query, reachableQuery)
	add(snapshot.Mutation, reachableMutation)
	add(snapshot.Subscription, reachableSubscription)

	fallback := firstRootPosition(snapshot, types)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		bits := current.bits &^ processed[current.name]
		if bits == 0 {
			continue
		}
		processed[current.name] |= bits
		definition, exists := types[current.name]
		if !exists {
			continue
		}
		for _, fieldDefinition := range definition.Fields {
			field, err := datasource.NewFieldRef(definition.Name, fieldDefinition.Name)
			if err == nil {
				info := fields[field]
				info.reachable |= bits
				fields[field] = info
			}
			add(namedType(fieldDefinition.Type), bits)
		}
		for _, interfaceName := range definition.Interfaces {
			add(interfaceName, bits)
		}
		for _, member := range definition.UnionTypes {
			add(member, bits)
		}
		if definition.Kind == "INTERFACE" {
			for _, implementor := range implementors[definition.Name] {
				add(implementor, bits)
			}
		}
	}
	return fields, fallback
}

func firstRootPosition(
	snapshot graphqlast.SchemaSnapshot,
	types map[string]graphqlast.TypeDefinition,
) graphqlast.SourcePosition {
	for _, name := range []string{snapshot.Query, snapshot.Mutation, snapshot.Subscription} {
		if definition, ok := types[name]; ok {
			return definition.Position
		}
	}
	return graphqlast.SourcePosition{Line: 1, Column: 1}
}

func namedType(reference graphqlast.TypeRef) string {
	for reference.Element != nil {
		reference = *reference.Element
	}
	return reference.Named
}

func sourceDirectivePosition(field graphqlast.FieldDefinition) graphqlast.SourcePosition {
	for _, directive := range field.Directives {
		if directive.Name == "source" {
			return directive.Position
		}
	}
	return field.Position
}

func crossDiagnostic(
	rule string,
	primary graphqlast.SourcePosition,
	message string,
	related graphqlast.SourcePosition,
	relatedMessage string,
) graphqlast.Diagnostic {
	return graphqlast.Diagnostic{
		Rule: rule, File: primary.File, Line: primary.Line, Column: primary.Column, Message: message,
		Related: []graphqlast.RelatedLocation{{
			File: related.File, Line: related.Line, Column: related.Column, Message: relatedMessage,
		}},
	}
}
