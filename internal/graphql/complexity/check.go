package complexity

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"

	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
	graphqlschema "github.com/Zachshotamartin/conduit/internal/graphql/schema"
)

const (
	defaultMaxDepth int   = 15
	defaultMaxCost  int64 = 10_000
)

// ExceededLimit identifies the first operation limit that rejected a request.
// Depth is always checked before cost.
type ExceededLimit string

const (
	NoLimit    ExceededLimit = ""
	DepthLimit ExceededLimit = "depth"
	CostLimit  ExceededLimit = "cost"
)

// Limits bounds semantic field depth and exact operation cost. A zero value
// selects the public defaults.
type Limits struct {
	MaxDepth int
	MaxCost  int64
}

// Resolve validates limits and fills their public defaults.
func (limits Limits) Resolve() (Limits, error) {
	if limits.MaxDepth < 0 {
		return Limits{}, fmt.Errorf("maximum query depth must be nonnegative")
	}
	if limits.MaxCost < 0 {
		return Limits{}, fmt.Errorf("maximum query complexity must be nonnegative")
	}
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaultMaxDepth
	}
	if limits.MaxCost == 0 {
		limits.MaxCost = defaultMaxCost
	}
	return limits, nil
}

// Assessment is the deterministic result of checking one selected operation.
// Cost is deliberately empty when depth rejected the operation because depth
// is checked before multiplier evaluation or cost arithmetic.
type Assessment struct {
	Depth    int
	Cost     string
	MaxDepth int
	MaxCost  string
	Exceeded ExceededLimit
}

// Check measures the selected, spec-valid operation after variable coercion
// and directive resolution. It expands each fragment-spread occurrence while
// memoizing fragment summaries so repeated DAG edges cannot cause exponential
// work.
func Check(
	schema *graphqlschema.Schema,
	operation graphqlast.ExecutableOperation,
	fragments []graphqlast.ExecutableFragment,
	variables map[string]any,
	limits Limits,
) (Assessment, error) {
	if schema == nil || schema.Anchor() == (graphqlast.SchemaAnchor{}) {
		return Assessment{}, fmt.Errorf("complexity check requires a validated schema")
	}
	resolvedLimits, err := limits.Resolve()
	if err != nil {
		return Assessment{}, err
	}
	walker, err := newWalker(schema, fragments, variables)
	if err != nil {
		return Assessment{}, err
	}
	assessment := Assessment{
		MaxDepth: resolvedLimits.MaxDepth,
		MaxCost:  strconv.FormatInt(resolvedLimits.MaxCost, 10),
	}
	assessment.Depth, err = walker.selectionDepth(operation.SelectionSet)
	if err != nil {
		return Assessment{}, err
	}
	if assessment.Depth > resolvedLimits.MaxDepth {
		assessment.Exceeded = DepthLimit
		return assessment, nil
	}
	cost, err := walker.selectionCost(operation.SelectionSet)
	if err != nil {
		return Assessment{}, err
	}
	assessment.Cost = cost.String()
	if cost.Cmp(big.NewInt(resolvedLimits.MaxCost)) > 0 {
		assessment.Exceeded = CostLimit
	}
	return assessment, nil
}

type walker struct {
	schema        *graphqlschema.Schema
	variables     map[string]any
	fragments     map[string]graphqlast.ExecutableFragment
	fields        map[string]graphqlast.FieldDefinition
	depthMemo     map[string]int
	costMemo      map[string]*big.Int
	depthVisiting map[string]bool
	costVisiting  map[string]bool
}

func newWalker(
	schema *graphqlschema.Schema,
	fragments []graphqlast.ExecutableFragment,
	variables map[string]any,
) (*walker, error) {
	fragmentIndex := make(map[string]graphqlast.ExecutableFragment, len(fragments))
	for _, fragment := range fragments {
		if fragment.Name == "" {
			return nil, fmt.Errorf("complexity fragment name must be nonempty")
		}
		if _, duplicate := fragmentIndex[fragment.Name]; duplicate {
			return nil, fmt.Errorf("complexity fragment %s is duplicated", fragment.Name)
		}
		fragmentIndex[fragment.Name] = fragment
	}
	snapshot := schema.Snapshot()
	fields := make(map[string]graphqlast.FieldDefinition)
	for _, definition := range snapshot.Types {
		for _, field := range definition.Fields {
			fields[fieldCoordinate(definition.Name, field.Name)] = field
		}
	}
	return &walker{
		schema: schema, variables: variables, fragments: fragmentIndex, fields: fields,
		depthMemo: make(map[string]int), costMemo: make(map[string]*big.Int),
		depthVisiting: make(map[string]bool), costVisiting: make(map[string]bool),
	}, nil
}

func (walker *walker) selectionDepth(selections []graphqlast.ExecutableSelection) (int, error) {
	maximum := 0
	for _, selection := range selections {
		active, err := selectionActive(selectionDirectives(selection), walker.variables)
		if err != nil {
			return 0, err
		}
		if !active {
			continue
		}
		depth := 0
		switch selection.Kind {
		case graphqlast.SelectionField:
			childDepth, childErr := walker.selectionDepth(selection.Field.SelectionSet)
			if childErr != nil {
				return 0, childErr
			}
			depth = 1 + childDepth
		case graphqlast.SelectionInlineFragment:
			depth, err = walker.selectionDepth(selection.SelectionSet)
		case graphqlast.SelectionFragmentSpread:
			depth, err = walker.fragmentDepth(selection.FragmentName)
		default:
			return 0, fmt.Errorf("unsupported complexity selection kind %s", selection.Kind)
		}
		if err != nil {
			return 0, err
		}
		if depth > maximum {
			maximum = depth
		}
	}
	return maximum, nil
}

func (walker *walker) fragmentDepth(name string) (int, error) {
	if depth, exists := walker.depthMemo[name]; exists {
		return depth, nil
	}
	fragment, exists := walker.fragments[name]
	if !exists {
		return 0, fmt.Errorf("complexity fragment %s is unavailable", name)
	}
	if walker.depthVisiting[name] {
		return 0, fmt.Errorf("complexity fragment cycle includes %s", name)
	}
	walker.depthVisiting[name] = true
	depth, err := walker.selectionDepth(fragment.SelectionSet)
	delete(walker.depthVisiting, name)
	if err != nil {
		return 0, err
	}
	walker.depthMemo[name] = depth
	return depth, nil
}

func (walker *walker) selectionCost(selections []graphqlast.ExecutableSelection) (*big.Int, error) {
	total := new(big.Int)
	for _, selection := range selections {
		active, err := selectionActive(selectionDirectives(selection), walker.variables)
		if err != nil {
			return nil, err
		}
		if !active {
			continue
		}
		var contribution *big.Int
		switch selection.Kind {
		case graphqlast.SelectionField:
			contribution, err = walker.fieldCost(selection.Field)
		case graphqlast.SelectionInlineFragment:
			contribution, err = walker.selectionCost(selection.SelectionSet)
		case graphqlast.SelectionFragmentSpread:
			contribution, err = walker.fragmentCost(selection.FragmentName)
		default:
			return nil, fmt.Errorf("unsupported complexity selection kind %s", selection.Kind)
		}
		if err != nil {
			return nil, err
		}
		total.Add(total, contribution)
	}
	return total, nil
}

func (walker *walker) fragmentCost(name string) (*big.Int, error) {
	if cost, exists := walker.costMemo[name]; exists {
		return new(big.Int).Set(cost), nil
	}
	fragment, exists := walker.fragments[name]
	if !exists {
		return nil, fmt.Errorf("complexity fragment %s is unavailable", name)
	}
	if walker.costVisiting[name] {
		return nil, fmt.Errorf("complexity fragment cycle includes %s", name)
	}
	walker.costVisiting[name] = true
	cost, err := walker.selectionCost(fragment.SelectionSet)
	delete(walker.costVisiting, name)
	if err != nil {
		return nil, err
	}
	walker.costMemo[name] = new(big.Int).Set(cost)
	return cost, nil
}

func (walker *walker) fieldCost(field graphqlast.ExecutableField) (*big.Int, error) {
	metadata, found := walker.schema.Field(field.ParentType, field.Name)
	declaredCost := 1
	var multiplierNames []string
	if found {
		declaredCost = metadata.Complexity.Cost
		multiplierNames = metadata.Complexity.Multipliers
	}
	factor, err := walker.fieldMultiplier(field, multiplierNames)
	if err != nil {
		return nil, err
	}
	children, err := walker.selectionCost(field.SelectionSet)
	if err != nil {
		return nil, err
	}
	relative := new(big.Int).Add(big.NewInt(int64(declaredCost)), children)
	return relative.Mul(relative, factor), nil
}

func (walker *walker) fieldMultiplier(
	field graphqlast.ExecutableField,
	names []string,
) (*big.Int, error) {
	factor := big.NewInt(1)
	if len(names) == 0 {
		return factor, nil
	}
	definition, exists := walker.fields[fieldCoordinate(field.ParentType, field.Name)]
	if !exists {
		return nil, fmt.Errorf("complexity field %s.%s is absent from schema", field.ParentType, field.Name)
	}
	authored := make(map[string]graphqlast.ExecutableArgument, len(field.Arguments))
	for _, argument := range field.Arguments {
		authored[argument.Name] = argument
	}
	definitions := make(map[string]graphqlast.ArgumentDefinition, len(definition.Arguments))
	for _, argument := range definition.Arguments {
		definitions[argument.Name] = argument
	}
	for _, name := range names {
		argumentDefinition, defined := definitions[name]
		if !defined {
			return nil, fmt.Errorf("complexity multiplier %s.%s(%s) is undefined", field.ParentType, field.Name, name)
		}
		value, present, err := effectiveIntArgument(authored[name], argumentDefinition, walker.variables)
		if err != nil {
			return nil, fmt.Errorf("complexity multiplier %s.%s(%s): %w", field.ParentType, field.Name, name, err)
		}
		if !present || value == nil {
			return nil, fmt.Errorf("complexity multiplier %s.%s(%s) must be present and non-null", field.ParentType, field.Name, name)
		}
		integer, ok := strictInt64(value)
		if !ok {
			return nil, fmt.Errorf("complexity multiplier %s.%s(%s) must be an integer", field.ParentType, field.Name, name)
		}
		if integer < 0 {
			return nil, fmt.Errorf("complexity multiplier %s.%s(%s) must be nonnegative", field.ParentType, field.Name, name)
		}
		factor.Mul(factor, big.NewInt(integer))
	}
	return factor, nil
}

func effectiveIntArgument(
	authored graphqlast.ExecutableArgument,
	definition graphqlast.ArgumentDefinition,
	variables map[string]any,
) (any, bool, error) {
	var value any
	present := authored.Name != ""
	var err error
	if present {
		value, present, err = evaluateValue(authored.Value, variables)
		if err != nil {
			return nil, false, err
		}
	}
	if !present && definition.DefaultValue != nil {
		value, present, err = evaluateValue(*definition.DefaultValue, nil)
	}
	return value, present, err
}

func selectionActive(directives []graphqlast.DirectiveUse, variables map[string]any) (bool, error) {
	for _, directive := range directives {
		if directive.Name != "skip" && directive.Name != "include" {
			continue
		}
		var condition graphqlast.Value
		found := false
		for _, argument := range directive.Arguments {
			if argument.Name == "if" {
				condition = argument.Value
				found = true
				break
			}
		}
		if !found {
			return false, fmt.Errorf("@%s requires if", directive.Name)
		}
		value, present, err := evaluateValue(condition, variables)
		if err != nil || !present {
			return false, fmt.Errorf("@%s condition is unavailable", directive.Name)
		}
		boolean, ok := value.(bool)
		if !ok {
			return false, fmt.Errorf("@%s condition must be boolean", directive.Name)
		}
		if directive.Name == "skip" && boolean {
			return false, nil
		}
		if directive.Name == "include" && !boolean {
			return false, nil
		}
	}
	return true, nil
}

func selectionDirectives(selection graphqlast.ExecutableSelection) []graphqlast.DirectiveUse {
	if selection.Kind == graphqlast.SelectionField {
		return selection.Field.Directives
	}
	return selection.Directives
}

func evaluateValue(value graphqlast.Value, variables map[string]any) (any, bool, error) {
	switch value.Kind {
	case graphqlast.ValueVariable:
		if variables == nil {
			return nil, false, nil
		}
		result, exists := variables[value.Raw]
		return result, exists, nil
	case graphqlast.ValueInt:
		integer, err := strconv.ParseInt(value.Raw, 10, 64)
		return integer, true, err
	case graphqlast.ValueBoolean:
		boolean, err := strconv.ParseBool(value.Raw)
		return boolean, true, err
	case graphqlast.ValueNull:
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("value kind %s is not valid here", value.Kind)
	}
}

func strictInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case json.Number:
		integer, err := typed.Int64()
		return integer, err == nil
	default:
		return 0, false
	}
}

func fieldCoordinate(parent, field string) string {
	return parent + "." + field
}
