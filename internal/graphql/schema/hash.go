package schema

import (
	"crypto/sha256"
	"encoding/json"
	"sort"
	"strconv"

	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
)

const hashDomain = "conduit-schema-v1\x00"

func semanticHash(snapshot graphqlast.SchemaSnapshot) Hash {
	canonicalizeSnapshot(&snapshot)
	payload, err := json.Marshal(snapshot)
	if err != nil {
		panic("schema snapshot contains only JSON-safe values: " + err.Error())
	}
	digest := sha256.Sum256(append([]byte(hashDomain), payload...))
	return Hash(digest)
}

func canonicalizeSnapshot(snapshot *graphqlast.SchemaSnapshot) {
	repeatable := make(map[string]bool, len(snapshot.Directives))
	for _, definition := range snapshot.Directives {
		repeatable[definition.Name] = definition.Repeatable
	}
	canonicalizeDirectives(snapshot.SchemaDirectives, repeatable)
	for index := range snapshot.Types {
		definition := &snapshot.Types[index]
		definition.Position = graphqlast.SourcePosition{}
		sort.Strings(definition.Interfaces)
		sort.Strings(definition.UnionTypes)
		canonicalizeDirectives(definition.Directives, repeatable)
		for fieldIndex := range definition.Fields {
			field := &definition.Fields[fieldIndex]
			field.Position = graphqlast.SourcePosition{}
			canonicalizeValuePointer(field.DefaultValue)
			canonicalizeDirectives(field.Directives, repeatable)
			for argumentIndex := range field.Arguments {
				canonicalizeArgument(&field.Arguments[argumentIndex], repeatable)
			}
			sort.Slice(field.Arguments, func(i, j int) bool { return field.Arguments[i].Name < field.Arguments[j].Name })
		}
		for valueIndex := range definition.EnumValues {
			value := &definition.EnumValues[valueIndex]
			value.Position = graphqlast.SourcePosition{}
			canonicalizeDirectives(value.Directives, repeatable)
		}
		sort.Slice(definition.Fields, func(i, j int) bool { return definition.Fields[i].Name < definition.Fields[j].Name })
		sort.Slice(definition.EnumValues, func(i, j int) bool { return definition.EnumValues[i].Name < definition.EnumValues[j].Name })
	}
	for index := range snapshot.Directives {
		definition := &snapshot.Directives[index]
		definition.Position = graphqlast.SourcePosition{}
		for argumentIndex := range definition.Arguments {
			canonicalizeArgument(&definition.Arguments[argumentIndex], repeatable)
		}
		sort.Slice(definition.Arguments, func(i, j int) bool { return definition.Arguments[i].Name < definition.Arguments[j].Name })
		sort.Strings(definition.Locations)
	}
	sort.Slice(snapshot.Types, func(i, j int) bool { return snapshot.Types[i].Name < snapshot.Types[j].Name })
	sort.Slice(snapshot.Directives, func(i, j int) bool { return snapshot.Directives[i].Name < snapshot.Directives[j].Name })
}

func canonicalizeArgument(argument *graphqlast.ArgumentDefinition, repeatable map[string]bool) {
	argument.Position = graphqlast.SourcePosition{}
	canonicalizeValuePointer(argument.DefaultValue)
	canonicalizeDirectives(argument.Directives, repeatable)
}

func canonicalizeDirectives(directives []graphqlast.DirectiveUse, repeatable map[string]bool) {
	hasOrderedUse := false
	for index := range directives {
		directive := &directives[index]
		hasOrderedUse = hasOrderedUse || repeatable[directive.Name]
		directive.Position = graphqlast.SourcePosition{}
		for argumentIndex := range directive.Arguments {
			argument := &directive.Arguments[argumentIndex]
			argument.Position = graphqlast.SourcePosition{}
			canonicalizeValue(&argument.Value)
		}
		sort.Slice(directive.Arguments, func(i, j int) bool { return directive.Arguments[i].Name < directive.Arguments[j].Name })
	}
	if hasOrderedUse {
		return
	}
	sort.Slice(directives, func(i, j int) bool {
		if directives[i].Name != directives[j].Name {
			return directives[i].Name < directives[j].Name
		}
		left, _ := json.Marshal(directives[i])
		right, _ := json.Marshal(directives[j])
		return string(left) < string(right)
	})
}

func canonicalizeValuePointer(value *graphqlast.Value) {
	if value != nil {
		canonicalizeValue(value)
	}
}

func canonicalizeValue(value *graphqlast.Value) {
	value.Position = graphqlast.SourcePosition{}
	switch value.Kind {
	case graphqlast.ValueInt:
		if parsed, err := strconv.ParseInt(value.Raw, 10, 64); err == nil {
			value.Raw = strconv.FormatInt(parsed, 10)
		}
	case graphqlast.ValueFloat:
		if parsed, err := strconv.ParseFloat(value.Raw, 64); err == nil {
			value.Raw = strconv.FormatFloat(parsed, 'g', -1, 64)
		}
	}
	for index := range value.Children {
		canonicalizeValue(&value.Children[index].Value)
	}
	if value.Kind == graphqlast.ValueObject {
		sort.Slice(value.Children, func(i, j int) bool { return value.Children[i].Name < value.Children[j].Name })
	}
}
