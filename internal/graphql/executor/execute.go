package executor

import (
	"context"
	stderrors "errors"
	"fmt"
	"sync"

	"github.com/Zachshotamartin/conduit/internal/datasource"
	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
	"github.com/Zachshotamartin/conduit/internal/graphql/binding"
	"github.com/Zachshotamartin/conduit/internal/graphql/complexity"
)

type executionState struct {
	request   Request
	variables map[string]any
	fragments map[string]graphqlast.ExecutableFragment
}

type fieldResult struct {
	value  any
	errors []Error
	bubble bool
}

// Execute selects, coerces, collects, dispatches, and completes one query or
// mutation. Every request failure is returned as a GraphQL Result and no
// adapter receives unvalidated variables.
func (executor *Executor) Execute(ctx context.Context, request Request) Result {
	if ctx == nil || executor == nil || executor.schema == nil || executor.bindings == nil ||
		request.Operation == nil || request.Operation.Anchor() != executor.schema.Anchor() ||
		request.Operation.Anchor() != executor.bindings.SchemaAnchor() || !request.Tenant.Valid() ||
		request.Principal.Subject() == "" || request.Principal.Tenant() != request.Tenant || request.Deadline.IsZero() {
		return requestFailure()
	}

	snapshot := request.Operation.Snapshot()
	operation, err := selectOperation(snapshot.Operations, request.OperationName)
	if err != nil || operation.Kind == graphqlast.OperationSubscription {
		return requestFailure()
	}
	variables, err := executor.coerceVariables(request.Variables, operation.Variables)
	if err != nil {
		return requestFailure()
	}
	assessment, err := complexity.Check(
		executor.schema, operation, snapshot.Fragments, variables, executor.limits,
	)
	if err != nil {
		return requestFailure()
	}
	switch assessment.Exceeded {
	case complexity.DepthLimit:
		return depthLimitFailure(assessment.Depth, assessment.MaxDepth)
	case complexity.CostLimit:
		return costLimitFailure(assessment.Cost, assessment.MaxCost)
	}
	fragments := make(map[string]graphqlast.ExecutableFragment, len(snapshot.Fragments))
	for _, fragment := range snapshot.Fragments {
		fragments[fragment.Name] = fragment
	}
	rootType := executor.rootType(operation.Kind)
	if rootType == "" {
		return requestFailure()
	}

	ctx, cancel := context.WithDeadline(ctx, request.Deadline)
	defer cancel()
	state := executionState{request: request, variables: variables, fragments: fragments}
	data, failures, bubble := executor.executeSelectionSet(
		ctx,
		state,
		rootType,
		nil,
		operation.SelectionSet,
		nil,
		operation.Kind == graphqlast.OperationMutation,
	)
	if bubble {
		return Result{Data: jsonNull(), Errors: failures}
	}
	return Result{Data: marshalOutput(data), Errors: failures}
}

func selectOperation(
	operations []graphqlast.ExecutableOperation,
	name string,
) (graphqlast.ExecutableOperation, error) {
	if name == "" {
		if len(operations) != 1 {
			return graphqlast.ExecutableOperation{}, fmt.Errorf("operation name is required")
		}
		return operations[0], nil
	}
	for _, operation := range operations {
		if operation.Name == name {
			return operation, nil
		}
	}
	return graphqlast.ExecutableOperation{}, fmt.Errorf("operation %s is undefined", name)
}

func (executor *Executor) rootType(kind graphqlast.OperationKind) string {
	switch kind {
	case graphqlast.OperationQuery:
		return executor.index.snapshot.Query
	case graphqlast.OperationMutation:
		return executor.index.snapshot.Mutation
	case graphqlast.OperationSubscription:
		return executor.index.snapshot.Subscription
	default:
		return ""
	}
}

func (executor *Executor) executeSelectionSet(
	ctx context.Context,
	state executionState,
	runtimeType string,
	parent any,
	selectionSet []graphqlast.ExecutableSelection,
	path []any,
	serial bool,
) (orderedObject, []Error, bool) {
	fields, err := executor.collectFields(runtimeType, selectionSet, state.fragments, state.variables)
	if err != nil {
		return orderedObject{}, []Error{newExecutionError(conduiterrors.InvalidRequest, path)}, true
	}
	results := make([]fieldResult, len(fields))
	if serial || len(fields) < 2 {
		for index := range fields {
			results[index] = executor.executeField(ctx, state, runtimeType, parent, fields[index], path)
		}
	} else {
		var wait sync.WaitGroup
		wait.Add(len(fields))
		for index := range fields {
			index := index
			go func() {
				defer wait.Done()
				results[index] = executor.executeField(ctx, state, runtimeType, parent, fields[index], path)
			}()
		}
		wait.Wait()
	}

	object := orderedObject{fields: make([]orderedField, len(fields))}
	var failures []Error
	bubble := false
	for index, field := range fields {
		object.fields[index] = orderedField{name: field.responseKey, value: results[index].value}
		failures = append(failures, results[index].errors...)
		bubble = bubble || results[index].bubble
	}
	return object, failures, bubble
}

func (executor *Executor) executeField(
	ctx context.Context,
	state executionState,
	runtimeType string,
	parent any,
	field collectedField,
	path []any,
) fieldResult {
	first := field.occurrences[0]
	fieldPath := appendPath(path, field.responseKey)
	if first.Name == "__typename" {
		return fieldResult{value: runtimeType}
	}
	arguments, err := executor.coerceArguments(first, state.variables)
	if err != nil {
		return fieldResult{
			value: nil, errors: []Error{newExecutionError(conduiterrors.InvalidRequest, fieldPath)},
			bubble: first.Type.NonNull,
		}
	}
	coordinate, err := datasource.NewFieldRef(first.ParentType, first.Name)
	if err != nil {
		return fieldResult{
			value: nil, errors: []Error{newExecutionError(conduiterrors.InternalInvariant, fieldPath)},
			bubble: first.Type.NonNull,
		}
	}
	resolver, ok := executor.bindings.Lookup(coordinate)
	if !ok {
		return fieldResult{
			value: nil, errors: []Error{newExecutionError(conduiterrors.InternalInvariant, fieldPath)},
			bubble: first.Type.NonNull,
		}
	}

	var value any
	if resolver.Kind == binding.Parent {
		value, ok = projectParent(parent, resolver.ParentPath)
		if !ok {
			return fieldResult{
				value: nil, errors: []Error{newExecutionError(conduiterrors.SourceInvalidResponse, fieldPath)},
				bubble: first.Type.NonNull,
			}
		}
	} else {
		value, err = executor.resolveSource(ctx, state.request, resolver.SourceName, coordinate, arguments, parent)
		if err != nil {
			category := sourceErrorCategory(err)
			return fieldResult{
				value: nil, errors: []Error{newExecutionError(category, fieldPath)}, bubble: first.Type.NonNull,
			}
		}
	}

	var childSelections []graphqlast.ExecutableSelection
	for _, occurrence := range field.occurrences {
		childSelections = append(childSelections, occurrence.SelectionSet...)
	}
	return executor.completeValue(ctx, state, first.Type, value, childSelections, fieldPath)
}

func (executor *Executor) resolveSource(
	ctx context.Context,
	request Request,
	sourceName string,
	field datasource.FieldRef,
	arguments datasource.ArgumentValues,
	parent any,
) (any, error) {
	runtime, exists := executor.sources[sourceName]
	if !exists {
		return nil, conduiterrors.New(conduiterrors.SourceUnavailable)
	}
	select {
	case runtime.semaphore <- struct{}{}:
		defer func() { <-runtime.semaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var parentJSON []byte
	var err error
	if parent != nil {
		parentJSON, err = canonicalParent(parent)
		if err != nil {
			return nil, conduiterrors.Wrap(conduiterrors.SourceInvalidResponse, err)
		}
	}
	response, err := runtime.source.Resolve(ctx, &datasource.SourceRequest{
		Field: field, Tenant: request.Tenant, Args: arguments, Parent: parentJSON,
		Principal: request.Principal, Deadline: request.Deadline,
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, conduiterrors.New(conduiterrors.SourceInvalidResponse)
	}
	value, err := decodeSourceJSON(response.Data)
	if err != nil {
		return nil, conduiterrors.Wrap(conduiterrors.SourceInvalidResponse, err)
	}
	return value, nil
}

func sourceErrorCategory(err error) conduiterrors.Category {
	if stderrors.Is(err, context.DeadlineExceeded) {
		return conduiterrors.SourceTimeout
	}
	if stderrors.Is(err, context.Canceled) {
		return conduiterrors.Cancelled
	}
	var classified *conduiterrors.Error
	if stderrors.As(err, &classified) {
		return classified.Category()
	}
	return conduiterrors.InternalInvariant
}

func projectParent(parent any, path []string) (any, bool) {
	current := parent
	for _, segment := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	if parent == nil {
		return nil, false
	}
	return current, true
}

func requestFailure() Result {
	return Result{
		Data: jsonNull(), Errors: []Error{newExecutionError(conduiterrors.InvalidRequest, nil)},
	}
}

func depthLimitFailure(depth, maximum int) Result {
	failure := newExecutionError(conduiterrors.InvalidRequest, nil)
	failure.depth = &depth
	failure.maxDepth = &maximum
	return Result{Data: jsonNull(), Errors: []Error{failure}}
}

func costLimitFailure(cost, maximum string) Result {
	failure := newExecutionError(conduiterrors.ComplexityExceeded, nil)
	failure.cost = cost
	failure.maxCost = maximum
	return Result{Data: jsonNull(), Errors: []Error{failure}}
}

func jsonNull() []byte { return []byte("null") }
