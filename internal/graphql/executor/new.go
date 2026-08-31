package executor

import (
	"fmt"
	"strings"

	conduiterrors "github.com/Zachshotamartin/conduit/internal/errors"
	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
	"github.com/Zachshotamartin/conduit/internal/graphql/complexity"
)

// New validates a complete schema generation and constructs an immutable
// executor. It publishes no partial executor on configuration failure.
func New(config Config) (*Executor, error) {
	if config.Schema == nil || config.Schema.Anchor() == (graphqlast.SchemaAnchor{}) {
		return nil, invalidConfiguration("executor requires a validated schema")
	}
	if config.Bindings == nil || config.Bindings.SchemaAnchor() == (graphqlast.SchemaAnchor{}) {
		return nil, invalidConfiguration("executor requires a compiled binding table")
	}
	if config.Schema.Anchor() != config.Bindings.SchemaAnchor() {
		return nil, invalidConfiguration("schema and binding identities differ")
	}
	concurrency := config.MaxSourceConcurrency
	if concurrency == 0 {
		concurrency = defaultSourceConcurrency
	}
	if concurrency < 1 {
		return nil, invalidConfiguration("source concurrency must be positive")
	}
	limits, err := (complexity.Limits{
		MaxDepth: config.MaxQueryDepth, MaxCost: config.MaxQueryComplexity,
	}).Resolve()
	if err != nil {
		return nil, invalidConfiguration(err.Error())
	}

	sources := make(map[string]sourceRuntime, len(config.Sources))
	for _, source := range config.Sources {
		if source == nil {
			return nil, invalidConfiguration("executor source must be nonnil")
		}
		name := source.Name()
		if strings.TrimSpace(name) == "" {
			return nil, invalidConfiguration("executor source name must be nonempty")
		}
		if _, duplicate := sources[name]; duplicate {
			return nil, invalidConfiguration("executor source names must be unique")
		}
		sources[name] = sourceRuntime{source: source, semaphore: make(chan struct{}, concurrency)}
	}
	for _, required := range config.Bindings.SourceNames() {
		if _, registered := sources[required]; !registered {
			return nil, invalidConfiguration("binding references an unregistered executor source")
		}
	}

	snapshot := config.Schema.Snapshot()
	index := schemaIndex{
		snapshot: snapshot,
		types:    make(map[string]graphqlast.TypeDefinition, len(snapshot.Types)),
		fields:   make(map[string]graphqlast.FieldDefinition),
	}
	for _, definition := range snapshot.Types {
		index.types[definition.Name] = definition
		for _, field := range definition.Fields {
			index.fields[fieldCoordinate(definition.Name, field.Name)] = field
		}
	}
	return &Executor{
		schema: config.Schema, bindings: config.Bindings, sources: sources, index: index, limits: limits,
	}, nil
}

func invalidConfiguration(message string) error {
	return conduiterrors.Wrap(conduiterrors.InvalidConfiguration, fmt.Errorf("executor configuration: %s", message))
}

func fieldCoordinate(parent, field string) string {
	return parent + "." + field
}
