package ast

// OperationKind is an executable operation category.
type OperationKind string

const (
	OperationQuery        OperationKind = "query"
	OperationMutation     OperationKind = "mutation"
	OperationSubscription OperationKind = "subscription"
)

// SelectionKind identifies one executable selection variant.
type SelectionKind string

const (
	SelectionField          SelectionKind = "field"
	SelectionFragmentSpread SelectionKind = "fragment_spread"
	SelectionInlineFragment SelectionKind = "inline_fragment"
)

// OperationSnapshot is a defensive parser-neutral executable document.
type OperationSnapshot struct {
	Operations []ExecutableOperation
	Fragments  []ExecutableFragment
}

// ExecutableOperation is one named or anonymous operation definition.
type ExecutableOperation struct {
	Kind         OperationKind
	Name         string
	Variables    []VariableDefinition
	Directives   []DirectiveUse
	SelectionSet []ExecutableSelection
	Position     SourcePosition
}

// VariableDefinition is one operation variable and its optional authored
// default value.
type VariableDefinition struct {
	Name         string
	Type         TypeRef
	DefaultValue *Value
	Directives   []DirectiveUse
	Position     SourcePosition
}

// ExecutableFragment is one validated named fragment definition.
type ExecutableFragment struct {
	Name          string
	TypeCondition string
	Variables     []VariableDefinition
	Directives    []DirectiveUse
	SelectionSet  []ExecutableSelection
	Position      SourcePosition
}

// ExecutableSelection is a tagged field, fragment spread, or inline fragment.
type ExecutableSelection struct {
	Kind          SelectionKind
	Field         ExecutableField
	FragmentName  string
	TypeCondition string
	Directives    []DirectiveUse
	SelectionSet  []ExecutableSelection
	Position      SourcePosition
}

// ExecutableField carries the validated parent coordinate and return type
// needed by Conduit-owned collection and completion.
type ExecutableField struct {
	Alias        string
	Name         string
	ParentType   string
	Type         TypeRef
	Arguments    []ExecutableArgument
	Directives   []DirectiveUse
	SelectionSet []ExecutableSelection
	Position     SourcePosition
}

// ExecutableArgument is one authored field argument.
type ExecutableArgument struct {
	Name     string
	Value    Value
	Position SourcePosition
}
