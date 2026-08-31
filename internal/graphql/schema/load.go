package schema

import (
	"fmt"
	"io"
	"os"
	"strings"

	graphqlast "github.com/Zachshotamartin/conduit/internal/graphql/ast"
)

const (
	maxSchemaBytes     = 4 << 20
	conduitPreludeName = "__conduit/directives.graphql"
	conduitPrelude     = `
enum ConduitBackpressurePolicy {
  DROP_OLDEST
  COALESCE_BY_KEY
  DISCONNECT
}

directive @source(name: String!) on FIELD_DEFINITION
directive @auth(rule: String!) on FIELD_DEFINITION
directive @filterable on ARGUMENT_DEFINITION
directive @backpressure(policy: ConduitBackpressurePolicy, queue: Int, coalesceKey: String) on FIELD_DEFINITION
directive @complexity(cost: Int = 1, multipliers: [String!]) on FIELD_DEFINITION
`
)

// LoadSources compiles SDL and validates every Conduit directive before
// publishing any schema, metadata, or hash.
func LoadSources(sources []graphqlast.SchemaSource, options Options) (*Schema, error) {
	diagnostics, sourceNames, authRules := validateOptions(options)
	if len(diagnostics) > 0 {
		return nil, graphqlast.NewDiagnostics(diagnostics...)
	}
	inputs := make([]graphqlast.SchemaSource, 0, len(sources)+1)
	for _, source := range sources {
		inputs = append(inputs, graphqlast.SchemaSource{
			Name: source.Name, Input: append([]byte(nil), source.Input...), BuiltIn: false,
		})
	}
	inputs = append(inputs, graphqlast.SchemaSource{
		Name: conduitPreludeName, Input: []byte(conduitPrelude), BuiltIn: true,
	})
	executable, err := graphqlast.LoadSchema(inputs, graphqlast.SchemaLimits{})
	if err != nil {
		return nil, err
	}
	snapshot := executable.Snapshot()
	fields, arguments, directiveDiagnostics := validateConduitDirectives(snapshot, sourceNames, authRules)
	if len(directiveDiagnostics) > 0 {
		return nil, graphqlast.NewDiagnostics(directiveDiagnostics...)
	}
	return &Schema{
		executable: executable,
		hash:       semanticHash(snapshot),
		fields:     fields,
		arguments:  arguments,
	}, nil
}

// LoadFiles reads bounded physical files while retaining only logical names in
// all operator-facing diagnostics.
func LoadFiles(files []File, options Options) (*Schema, error) {
	sources := make([]graphqlast.SchemaSource, 0, len(files))
	normalizedNames := make([]string, len(files))
	seenNames := make(map[string]struct{}, len(files))
	var diagnostics []graphqlast.Diagnostic
	for index, file := range files {
		name, ok := graphqlast.NormalizeSchemaSourceName(file.Name)
		if !ok {
			diagnostics = append(diagnostics, graphqlast.Diagnostic{
				Rule: "sdl.file.logical_name", Message: "SDL logical name must be a unique relative path",
			})
			continue
		}
		if _, duplicate := seenNames[name]; duplicate {
			diagnostics = append(diagnostics, graphqlast.Diagnostic{
				Rule: "sdl.file.logical_name", File: name,
				Message: "SDL logical name is duplicated after normalization",
			})
			continue
		}
		seenNames[name] = struct{}{}
		normalizedNames[index] = name
	}
	if len(diagnostics) > 0 {
		return nil, graphqlast.NewDiagnostics(diagnostics...)
	}

	remaining := maxSchemaBytes
	for index, file := range files {
		input, readErr := readBounded(file.Path, remaining)
		if readErr != nil {
			rule := "sdl.file.read"
			message := "unable to read SDL file"
			if readErr == errSchemaFileTooLarge {
				rule = "sdl.limit.bytes"
				message = fmt.Sprintf("aggregate SDL byte limit %d exceeded", maxSchemaBytes)
			}
			diagnostics = append(diagnostics, graphqlast.Diagnostic{
				Rule: rule, File: normalizedNames[index], Line: 1, Column: 1, Message: message,
			})
			continue
		}
		remaining -= len(input)
		sources = append(sources, graphqlast.SchemaSource{Name: normalizedNames[index], Input: input})
	}
	if len(diagnostics) > 0 {
		return nil, graphqlast.NewDiagnostics(diagnostics...)
	}
	return LoadSources(sources, options)
}

var errSchemaFileTooLarge = fmt.Errorf("schema file exceeds remaining aggregate bound")

func readBounded(path string, remaining int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	input, err := io.ReadAll(io.LimitReader(file, int64(remaining)+1))
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(input) > remaining {
		return nil, errSchemaFileTooLarge
	}
	return input, nil
}

func validateOptions(options Options) ([]graphqlast.Diagnostic, map[string]struct{}, map[string]struct{}) {
	var diagnostics []graphqlast.Diagnostic
	sources := make(map[string]struct{}, len(options.SourceNames))
	for _, name := range options.SourceNames {
		if strings.TrimSpace(name) == "" {
			diagnostics = append(diagnostics, graphqlast.Diagnostic{
				Rule: "conduit.source.registry", Message: "source registry names must be nonempty",
			})
			continue
		}
		if _, duplicate := sources[name]; duplicate {
			diagnostics = append(diagnostics, graphqlast.Diagnostic{
				Rule: "conduit.source.registry", Message: "source registry names must be unique",
			})
		}
		sources[name] = struct{}{}
	}
	rules := make(map[string]struct{}, len(options.AuthRules))
	for _, name := range options.AuthRules {
		if strings.TrimSpace(name) == "" {
			diagnostics = append(diagnostics, graphqlast.Diagnostic{
				Rule: "conduit.auth.registry", Message: "auth rule names must be nonempty",
			})
			continue
		}
		if _, duplicate := rules[name]; duplicate {
			diagnostics = append(diagnostics, graphqlast.Diagnostic{
				Rule: "conduit.auth.registry", Message: "auth rule names must be unique",
			})
		}
		rules[name] = struct{}{}
	}
	return diagnostics, sources, rules
}
