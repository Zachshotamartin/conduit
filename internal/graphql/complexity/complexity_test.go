package complexity_test

import (
	"math"
	"math/big"
	"testing"

	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
	"github.com/Zachshotamartin/conduit/internal/graphql/complexity"
	graphqlschema "github.com/Zachshotamartin/conduit/internal/graphql/schema"
)

const complexitySchema = `
type Query {
  node(first: Int = 2): Node @source(name: "fixture") @complexity(cost: 2, multipliers: ["first"])
  plain: Int @source(name: "fixture") @complexity(cost: 1)
  search(first: Int): Int @source(name: "fixture") @complexity(cost: 1, multipliers: ["first"])
  members: [Node!]! @source(name: "fixture") @complexity(cost: 1)
  result: Search @source(name: "fixture") @complexity(cost: 1)
}

interface Named { value: Int }

type Node implements Named {
  value: Int @complexity(cost: 3)
  child(size: Int = 3): Node @complexity(cost: 1, multipliers: ["size"])
}

type Other implements Named { value: Int @complexity(cost: 3) }
union Search = Node | Other
`

func TestUNIT007_DepthAndCostBoundariesAreDistinctAndInclusive(t *testing.T) {
	t.Parallel()
	schema := loadComplexitySchema(t, complexitySchema)
	operation, fragments := parseComplexityOperation(t, schema, `{
  node(first: 2) { child(size: 3) { value } }
}`)

	tests := []struct {
		name      string
		limits    complexity.Limits
		wantLimit complexity.ExceededLimit
		wantDepth int
		wantCost  string
	}{
		{name: "equal limits accepted", limits: complexity.Limits{MaxDepth: 3, MaxCost: 28}, wantDepth: 3, wantCost: "28"},
		{name: "depth over by one", limits: complexity.Limits{MaxDepth: 2, MaxCost: 27}, wantLimit: complexity.DepthLimit, wantDepth: 3},
		{name: "cost over by one", limits: complexity.Limits{MaxDepth: 3, MaxCost: 27}, wantLimit: complexity.CostLimit, wantDepth: 3, wantCost: "28"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assessment, err := complexity.Check(schema, operation, fragments, nil, test.limits)
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if assessment.Exceeded != test.wantLimit || assessment.Depth != test.wantDepth || assessment.Cost != test.wantCost {
				t.Fatalf("assessment = %#v, want limit=%q depth=%d cost=%q", assessment, test.wantLimit, test.wantDepth, test.wantCost)
			}
			if assessment.MaxDepth != test.limits.MaxDepth || assessment.MaxCost != big.NewInt(test.limits.MaxCost).String() {
				t.Fatalf("assessment limits = %#v", assessment)
			}
		})
	}
}

func TestUNIT007_ChargesSyntacticOccurrencesAndActiveExpansions(t *testing.T) {
	t.Parallel()
	schema := loadComplexitySchema(t, complexitySchema)
	tests := []struct {
		name      string
		document  string
		variables map[string]any
		wantDepth int
		wantCost  string
	}{
		{
			name: "duplicates aliases and repeated spreads",
			document: `query { node(first: 1) { a: value value ...Bits ...Bits } }
fragment Bits on Node { value }`,
			wantDepth: 2,
			wantCost:  "14",
		},
		{
			name: "inactive selections are free",
			document: `query($on: Boolean!) {
  plain @skip(if: $on)
  node(first: 1) @include(if: $on) { value }
}`,
			variables: map[string]any{"on": true},
			wantDepth: 2,
			wantCost:  "5",
		},
		{
			name: "alternate active directive branch",
			document: `query($on: Boolean!) {
  plain @skip(if: $on)
  node(first: 1) @include(if: $on) { value }
}`,
			variables: map[string]any{"on": false},
			wantDepth: 1,
			wantCost:  "1",
		},
		{
			name:      "all statically valid type branches",
			document:  `{ result { ... on Node { value } ... on Other { value } } }`,
			wantDepth: 2,
			wantCost:  "7",
		},
		{
			name:      "list and non-null wrappers are transparent",
			document:  `{ members { value } }`,
			wantDepth: 2,
			wantCost:  "4",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			operation, fragments := parseComplexityOperation(t, schema, test.document)
			assessment, err := complexity.Check(
				schema, operation, fragments, test.variables,
				complexity.Limits{MaxDepth: 100, MaxCost: math.MaxInt64},
			)
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if assessment.Exceeded != complexity.NoLimit || assessment.Depth != test.wantDepth || assessment.Cost != test.wantCost {
				t.Fatalf("assessment = %#v, want depth=%d cost=%s", assessment, test.wantDepth, test.wantCost)
			}
		})
	}
}

func TestUNIT007_MultipliersUseEffectiveValuesAndScaleDescendants(t *testing.T) {
	t.Parallel()
	schema := loadComplexitySchema(t, complexitySchema)
	tests := []struct {
		name      string
		document  string
		variables map[string]any
		wantCost  string
		wantError bool
	}{
		{name: "schema default", document: `{ node { value } }`, wantCost: "10"},
		{name: "resolved variable", document: `query($n: Int!) { node(first: $n) { value } }`, variables: map[string]any{"n": int64(3)}, wantCost: "15"},
		{name: "ancestor product", document: `{ node(first: 2) { child(size: 3) { value } } }`, wantCost: "28"},
		{name: "zero scales whole subtree", document: `{ node(first: 0) { child(size: 3) { value } } }`, wantCost: "0"},
		{name: "missing effective value", document: `{ search }`, wantError: true},
		{name: "null effective value", document: `{ search(first: null) }`, wantError: true},
		{name: "negative literal", document: `{ search(first: -1) }`, wantError: true},
		{name: "negative variable", document: `query($n: Int) { search(first: $n) }`, variables: map[string]any{"n": int64(-1)}, wantError: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			operation, fragments := parseComplexityOperation(t, schema, test.document)
			assessment, err := complexity.Check(
				schema, operation, fragments, test.variables,
				complexity.Limits{MaxDepth: 100, MaxCost: math.MaxInt64},
			)
			if test.wantError {
				if err == nil {
					t.Fatalf("Check() succeeded with assessment %#v", assessment)
				}
				return
			}
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if assessment.Cost != test.wantCost {
				t.Fatalf("cost = %s, want %s", assessment.Cost, test.wantCost)
			}
		})
	}
}

func TestUNIT007_CostArithmeticIsExactBeyondMachineWidth(t *testing.T) {
	t.Parallel()
	schema := loadComplexitySchema(t, `
type Query {
  huge(n: Int = 2147483647): Huge @source(name: "fixture") @complexity(cost: 1, multipliers: ["n"])
}
type Huge {
  next(n: Int = 2147483647): Huge @complexity(cost: 1, multipliers: ["n"])
  leaf: Int @complexity(cost: 1)
}`)
	operation, fragments := parseComplexityOperation(t, schema, `{ huge { next { next { leaf } } } }`)
	assessment, err := complexity.Check(
		schema, operation, fragments, nil,
		complexity.Limits{MaxDepth: 10, MaxCost: math.MaxInt64},
	)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	p := big.NewInt(math.MaxInt32)
	p2 := new(big.Int).Mul(new(big.Int).Set(p), p)
	p3 := new(big.Int).Mul(new(big.Int).Set(p2), p)
	want := new(big.Int).Add(new(big.Int).Set(p), p2)
	want.Add(want, new(big.Int).Mul(big.NewInt(2), p3))
	if assessment.Cost != want.String() || assessment.Exceeded != complexity.CostLimit {
		t.Fatalf("assessment = %#v, want exact cost %s and cost rejection", assessment, want)
	}
}

func TestUNIT007_DefaultsAndInvalidLimitsFailClosed(t *testing.T) {
	t.Parallel()
	schema := loadComplexitySchema(t, complexitySchema)
	operation, fragments := parseComplexityOperation(t, schema, `{ plain }`)
	assessment, err := complexity.Check(schema, operation, fragments, nil, complexity.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.MaxDepth != 15 || assessment.MaxCost != "10000" {
		t.Fatalf("default limits = %#v", assessment)
	}
	if _, err := complexity.Check(
		schema, operation, fragments, nil, complexity.Limits{MaxDepth: -1, MaxCost: 1},
	); err == nil {
		t.Fatal("negative depth limit accepted")
	}
	if _, err := complexity.Check(
		schema, operation, fragments, nil, complexity.Limits{MaxDepth: 1, MaxCost: -1},
	); err == nil {
		t.Fatal("negative cost limit accepted")
	}
}

func loadComplexitySchema(t *testing.T, input string) *graphqlschema.Schema {
	t.Helper()
	loaded, err := graphqlschema.LoadSources(
		[]graphqlast.SchemaSource{{Name: "schema.graphql", Input: []byte(input)}},
		graphqlschema.Options{SourceNames: []string{"fixture"}},
	)
	if err != nil {
		t.Fatalf("LoadSources() error = %v", err)
	}
	return loaded
}

func parseComplexityOperation(
	t *testing.T,
	schema *graphqlschema.Schema,
	document string,
) (graphqlast.ExecutableOperation, []graphqlast.ExecutableFragment) {
	t.Helper()
	parsed, err := graphqlast.Intake([]byte(document), graphqlast.IntakeLimits{}, schema.Executable())
	if err != nil {
		t.Fatalf("Intake() error = %v", err)
	}
	snapshot := parsed.Snapshot()
	if len(snapshot.Operations) != 1 {
		t.Fatalf("operation count = %d", len(snapshot.Operations))
	}
	return snapshot.Operations[0], snapshot.Fragments
}
