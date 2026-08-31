package executor

import (
	"fmt"

	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
)

type collectedField struct {
	responseKey string
	occurrences []graphqlast.ExecutableField
}

func (executor *Executor) collectFields(
	runtimeType string,
	selectionSet []graphqlast.ExecutableSelection,
	fragments map[string]graphqlast.ExecutableFragment,
	variables map[string]any,
) ([]collectedField, error) {
	var fields []collectedField
	byResponseKey := make(map[string]int)
	visitedFragments := make(map[string]struct{})
	if err := executor.collectInto(
		runtimeType,
		selectionSet,
		fragments,
		variables,
		visitedFragments,
		&fields,
		byResponseKey,
	); err != nil {
		return nil, err
	}
	return fields, nil
}

func (executor *Executor) collectInto(
	runtimeType string,
	selectionSet []graphqlast.ExecutableSelection,
	fragments map[string]graphqlast.ExecutableFragment,
	variables map[string]any,
	visitedFragments map[string]struct{},
	fields *[]collectedField,
	byResponseKey map[string]int,
) error {
	for _, selection := range selectionSet {
		include, err := shouldInclude(selectionDirectives(selection), variables)
		if err != nil {
			return err
		}
		if !include {
			continue
		}
		switch selection.Kind {
		case graphqlast.SelectionField:
			responseKey := selection.Field.Alias
			if responseKey == "" {
				responseKey = selection.Field.Name
			}
			if index, exists := byResponseKey[responseKey]; exists {
				(*fields)[index].occurrences = append((*fields)[index].occurrences, selection.Field)
				continue
			}
			byResponseKey[responseKey] = len(*fields)
			*fields = append(*fields, collectedField{
				responseKey: responseKey,
				occurrences: []graphqlast.ExecutableField{selection.Field},
			})
		case graphqlast.SelectionFragmentSpread:
			if _, visited := visitedFragments[selection.FragmentName]; visited {
				continue
			}
			fragment, exists := fragments[selection.FragmentName]
			if !exists {
				return fmt.Errorf("fragment %s is unavailable", selection.FragmentName)
			}
			if !executor.typeConditionMatches(fragment.TypeCondition, runtimeType) {
				continue
			}
			visitedFragments[selection.FragmentName] = struct{}{}
			if err := executor.collectInto(
				runtimeType,
				fragment.SelectionSet,
				fragments,
				variables,
				visitedFragments,
				fields,
				byResponseKey,
			); err != nil {
				return err
			}
		case graphqlast.SelectionInlineFragment:
			if selection.TypeCondition != "" &&
				!executor.typeConditionMatches(selection.TypeCondition, runtimeType) {
				continue
			}
			if err := executor.collectInto(
				runtimeType,
				selection.SelectionSet,
				fragments,
				variables,
				visitedFragments,
				fields,
				byResponseKey,
			); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported executable selection kind %s", selection.Kind)
		}
	}
	return nil
}

func selectionDirectives(selection graphqlast.ExecutableSelection) []graphqlast.DirectiveUse {
	if selection.Kind == graphqlast.SelectionField {
		return selection.Field.Directives
	}
	return selection.Directives
}

func (executor *Executor) typeConditionMatches(condition, runtimeType string) bool {
	if condition == "" || condition == runtimeType {
		return true
	}
	definition, exists := executor.index.types[condition]
	if !exists {
		return false
	}
	switch definition.Kind {
	case "UNION":
		for _, member := range definition.UnionTypes {
			if member == runtimeType {
				return true
			}
		}
	case "INTERFACE":
		return executor.implementsInterface(runtimeType, condition, make(map[string]struct{}))
	}
	return false
}

func (executor *Executor) implementsInterface(
	typeName,
	interfaceName string,
	visited map[string]struct{},
) bool {
	if _, seen := visited[typeName]; seen {
		return false
	}
	visited[typeName] = struct{}{}
	definition, exists := executor.index.types[typeName]
	if !exists {
		return false
	}
	for _, implemented := range definition.Interfaces {
		if implemented == interfaceName || executor.implementsInterface(implemented, interfaceName, visited) {
			return true
		}
	}
	return false
}
