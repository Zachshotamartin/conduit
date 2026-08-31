package executor

import (
	"encoding/json"
	"time"

	"github.com/Zachshotamartin/conduit/internal/datasource"
	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
	"github.com/Zachshotamartin/conduit/internal/graphql/binding"
	"github.com/Zachshotamartin/conduit/internal/graphql/complexity"
	graphqlschema "github.com/Zachshotamartin/conduit/internal/graphql/schema"
)

const defaultSourceConcurrency = 10

// Config is the immutable artifact and adapter set for one executor.
type Config struct {
	Schema               *graphqlschema.Schema
	Bindings             *binding.Table
	Sources              []datasource.DataSource
	MaxSourceConcurrency int
	MaxQueryDepth        int
	MaxQueryComplexity   int64
}

// Request is one already-intaken operation with narrowed identity and timing
// data. Variables is either empty or exactly one JSON object.
type Request struct {
	Operation     *graphqlast.Operation
	OperationName string
	Variables     json.RawMessage
	Tenant        datasource.TenantID
	Principal     datasource.PrincipalView
	Deadline      time.Time
}

// Error is a structured GraphQL execution failure. R1.06 extends its wire
// formatting; the category and path are stable from the executor boundary.
type Error struct {
	Message  string
	Path     []any
	Code     conduiterrors.Category
	depth    *int
	maxDepth *int
	cost     string
	maxCost  string
}

// MarshalJSON emits the spec-sanctioned error surface in deterministic key
// order.
func (failure Error) MarshalJSON() ([]byte, error) {
	extensions := struct {
		Code     conduiterrors.Category `json:"code"`
		Depth    *int                   `json:"depth,omitempty"`
		MaxDepth *int                   `json:"max_depth,omitempty"`
		Cost     string                 `json:"cost,omitempty"`
		MaxCost  string                 `json:"max_cost,omitempty"`
	}{
		Code: failure.Code, Depth: failure.depth, MaxDepth: failure.maxDepth,
		Cost: failure.cost, MaxCost: failure.maxCost,
	}
	payload := struct {
		Message    string `json:"message"`
		Path       []any  `json:"path,omitempty"`
		Extensions any    `json:"extensions"`
	}{Message: failure.Message, Path: failure.Path, Extensions: extensions}
	return json.Marshal(payload)
}

// Result is one deterministic GraphQL response payload.
type Result struct {
	Data   json.RawMessage
	Errors []Error
}

// MarshalJSON emits data first and omits the errors member on success.
func (result Result) MarshalJSON() ([]byte, error) {
	data := result.Data
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	payload := struct {
		Data   json.RawMessage `json:"data"`
		Errors []Error         `json:"errors,omitempty"`
	}{Data: data, Errors: result.Errors}
	return json.Marshal(payload)
}

type sourceRuntime struct {
	source    datasource.DataSource
	semaphore chan struct{}
}

type schemaIndex struct {
	snapshot graphqlast.SchemaSnapshot
	types    map[string]graphqlast.TypeDefinition
	fields   map[string]graphqlast.FieldDefinition
}

// Executor owns field collection, input coercion, dispatch, and value
// completion for one immutable schema generation.
type Executor struct {
	schema   *graphqlschema.Schema
	bindings *binding.Table
	sources  map[string]sourceRuntime
	index    schemaIndex
	limits   complexity.Limits
}
