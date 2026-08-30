package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type moduleDescriptor struct {
	Path string
	Dir  string
}

type packageError struct {
	Err string
}

type listedPackage struct {
	ImportPath     string
	Dir            string
	Imports        []string
	TestImports    []string
	XTestImports   []string
	GoFiles        []string
	CgoFiles       []string
	IgnoredGoFiles []string
	TestGoFiles    []string
	XTestGoFiles   []string
	Module         *struct {
		Path string
		Main bool
	}
	Error      *packageError
	DepsErrors []packageError
}

type packageNode struct {
	ImportPath string
	Relative   string
	Dir        string
	Imports    []string
	Files      []*ast.File
}

type moduleGraph struct {
	Module   moduleDescriptor
	Packages []packageNode
}

func loadModuleGraph(ctx context.Context, root string) (moduleGraph, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return moduleGraph{}, fmt.Errorf("resolve module root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return moduleGraph{}, fmt.Errorf("stat module root: %w", err)
	}
	if !info.IsDir() {
		return moduleGraph{}, fmt.Errorf("module root %q is not a directory", absRoot)
	}

	moduleJSON, stderr, err := runGo(ctx, absRoot, "list", "-m", "-json")
	if err != nil {
		return moduleGraph{}, fmt.Errorf("load module metadata: %w%s", err, commandStderr(stderr))
	}
	var module moduleDescriptor
	if err := json.Unmarshal(moduleJSON, &module); err != nil {
		return moduleGraph{}, fmt.Errorf("decode module metadata: %w", err)
	}
	if module.Path == "" {
		return moduleGraph{}, fmt.Errorf("module at %q has an empty module path", absRoot)
	}
	if module.Dir == "" {
		module.Dir = absRoot
	}

	packageJSON, stderr, err := runGo(ctx, absRoot, "list", "-mod=readonly", "-deps", "-json", "./...")
	if err != nil {
		return moduleGraph{}, fmt.Errorf("load package graph: %w%s", err, commandStderr(stderr))
	}

	decoder := json.NewDecoder(bytes.NewReader(packageJSON))
	packages := make(map[string]packageNode)
	for decoder.More() {
		var listed listedPackage
		if err := decoder.Decode(&listed); err != nil {
			return moduleGraph{}, fmt.Errorf("decode package graph: %w", err)
		}
		if !belongsToModule(listed, module.Path) {
			continue
		}
		if listed.Error != nil {
			return moduleGraph{}, fmt.Errorf("load package %s: %s", listed.ImportPath, listed.Error.Err)
		}
		if len(listed.DepsErrors) != 0 {
			return moduleGraph{}, fmt.Errorf("load dependencies for %s: %s", listed.ImportPath, listed.DepsErrors[0].Err)
		}

		node, err := buildPackageNode(listed, module.Path)
		if err != nil {
			return moduleGraph{}, err
		}
		packages[node.ImportPath] = node
	}
	if len(packages) == 0 {
		return moduleGraph{}, fmt.Errorf("module %s contains no listed Go packages", module.Path)
	}

	graph := moduleGraph{Module: module, Packages: make([]packageNode, 0, len(packages))}
	for _, node := range packages {
		graph.Packages = append(graph.Packages, node)
	}
	sort.Slice(graph.Packages, func(i, j int) bool {
		return graph.Packages[i].ImportPath < graph.Packages[j].ImportPath
	})
	return graph, nil
}

func runGo(ctx context.Context, dir string, args ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, "go", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func commandStderr(stderr []byte) string {
	message := strings.TrimSpace(string(stderr))
	if message == "" {
		return ""
	}
	return ": " + message
}

func belongsToModule(pkg listedPackage, modulePath string) bool {
	if pkg.Module != nil && pkg.Module.Main {
		return true
	}
	return pkg.ImportPath == modulePath || strings.HasPrefix(pkg.ImportPath, modulePath+"/")
}

func buildPackageNode(listed listedPackage, modulePath string) (packageNode, error) {
	node := packageNode{
		ImportPath: listed.ImportPath,
		Relative:   relativeImportPath(listed.ImportPath, modulePath),
		Dir:        listed.Dir,
	}

	imports := make(map[string]struct{})
	for _, imported := range appendSlices(listed.Imports, listed.TestImports, listed.XTestImports) {
		imports[imported] = struct{}{}
	}

	fileNames := appendSlices(
		listed.GoFiles,
		listed.CgoFiles,
		listed.IgnoredGoFiles,
		listed.TestGoFiles,
		listed.XTestGoFiles,
	)
	seenFiles := make(map[string]struct{}, len(fileNames))
	files := make([]*ast.File, 0, len(fileNames))
	fileSet := token.NewFileSet()
	for _, name := range fileNames {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(listed.Dir, path)
		}
		path = filepath.Clean(path)
		if _, ok := seenFiles[path]; ok {
			continue
		}
		seenFiles[path] = struct{}{}

		file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err != nil {
			return packageNode{}, fmt.Errorf("parse %s: %w", path, err)
		}
		files = append(files, file)
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return packageNode{}, fmt.Errorf("parse import in %s: %w", path, err)
			}
			if imported != "C" {
				imports[imported] = struct{}{}
			}
		}
	}

	node.Imports = make([]string, 0, len(imports))
	for imported := range imports {
		node.Imports = append(node.Imports, imported)
	}
	sort.Strings(node.Imports)
	node.Files = files
	return node, nil
}

func appendSlices(slices ...[]string) []string {
	var length int
	for _, values := range slices {
		length += len(values)
	}
	result := make([]string, 0, length)
	for _, values := range slices {
		result = append(result, values...)
	}
	return result
}

func relativeImportPath(importPath, modulePath string) string {
	if importPath == modulePath {
		return "."
	}
	return strings.TrimPrefix(importPath, modulePath+"/")
}
