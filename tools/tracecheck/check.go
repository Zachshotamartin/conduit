package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	requirementPattern = regexp.MustCompile(`\b(?:FR|NFR)-[A-Z]+-[0-9]{3}\b`)
	sectionPattern     = regexp.MustCompile(`^##[[:space:]]+([0-9]+)(?:\.|[[:space:]]|$)`)
	rowPattern         = regexp.MustCompile(`^[A-Z]+-[0-9]{3}$`)
	testNamePattern    = regexp.MustCompile(`^Test([A-Z]+)([0-9]{3})(?:_|$)`)
	gatePattern        = regexp.MustCompile(`\bR(10|[0-9])(?:\.[0-9]+)?\b`)
)

// CheckOptions names every normative input to trace-check. Explicit paths
// make the parser independently testable without mutating repository files.
type CheckOptions struct {
	ProductRequirementsPath string
	BuildPlanPath           string
	OperationsTestPlanPath  string
	GateStatusPath          string
	OwnershipPath           string
	TestRoots               []string
}

// Violation is one deterministic traceability failure.
type Violation struct {
	Kind      string
	Reference string
	Path      string
	Message   string
}

func (violation Violation) String() string {
	return fmt.Sprintf("%s: %s (%s): %s", violation.Kind, violation.Reference, violation.Path, violation.Message)
}

type ownershipFile struct {
	Version      int               `json:"version"`
	Requirements []ownershipRecord `json:"requirements"`
}

type ownershipRecord struct {
	ID           string `json:"id"`
	TerminalGate string `json:"terminal_gate"`
}

type matrixRow struct {
	ID           string
	EarliestGate int
	Requirements []string
	Path         string
}

type testInventory struct {
	Rows                map[string]bool
	RequirementEvidence map[string]bool
	Citations           []citation
}

type citation struct {
	ID   string
	Path string
}

// ExtractRequirementIDs returns the sorted, unique requirement definitions
// from PRODUCT_REQUIREMENTS sections 7 and 9 only. References elsewhere in
// the document cannot mint IDs.
func ExtractRequirementIDs(reader io.Reader) ([]string, error) {
	sections, err := scanSections(reader, map[int]bool{7: true, 9: true})
	if err != nil {
		return nil, err
	}
	ids := uniqueRequirements(sections)
	if len(ids) == 0 {
		return nil, fmt.Errorf("PRODUCT_REQUIREMENTS sections 7 and 9 contain no requirement IDs")
	}
	return ids, nil
}

// Check validates the requirement inventory, ownership mirror, matrix rows,
// gate activation, and Go test declarations.
func Check(ctx context.Context, options CheckOptions) ([]Violation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	requirements, err := extractRequirementsFile(options.ProductRequirementsPath)
	if err != nil {
		return nil, err
	}
	requirementSet := toSet(requirements)

	buildOwners, buildDuplicates, err := extractBuildOwners(options.BuildPlanPath)
	if err != nil {
		return nil, err
	}
	mirror, mirrorDuplicates, err := readOwnership(options.OwnershipPath)
	if err != nil {
		return nil, err
	}
	statuses, err := readGateStatuses(options.GateStatusPath)
	if err != nil {
		return nil, err
	}
	rows, rowDuplicates, rowCitations, err := extractMatrixRows(options.OperationsTestPlanPath)
	if err != nil {
		return nil, err
	}
	tests, err := discoverTests(ctx, options.TestRoots)
	if err != nil {
		return nil, err
	}

	var violations []Violation
	for _, id := range buildDuplicates {
		violations = append(violations, Violation{
			Kind: "duplicate-build-owner", Reference: id, Path: options.BuildPlanPath,
			Message: "requirement appears more than once in BUILD_PLAN section 19",
		})
	}
	for _, id := range mirrorDuplicates {
		violations = append(violations, Violation{
			Kind: "duplicate-ownership", Reference: id, Path: options.OwnershipPath,
			Message: "requirement appears more than once in the ownership mirror",
		})
	}
	for _, id := range rowDuplicates {
		violations = append(violations, Violation{
			Kind: "duplicate-test-row", Reference: id, Path: options.OperationsTestPlanPath,
			Message: "test row appears more than once in Operations Test Plan section 10",
		})
	}

	for _, id := range requirements {
		buildGate, inBuild := buildOwners[id]
		mirrorGate, inMirror := mirror[id]
		if !inBuild {
			violations = append(violations, Violation{
				Kind: "missing-build-owner", Reference: id, Path: options.BuildPlanPath,
				Message: "real PRD requirement has no BUILD_PLAN section 19 owner",
			})
		}
		if !inMirror {
			violations = append(violations, Violation{
				Kind: "missing-ownership", Reference: id, Path: options.OwnershipPath,
				Message: "real PRD requirement is absent from the machine-readable ownership mirror",
			})
		}
		if inBuild && inMirror && buildGate != mirrorGate {
			violations = append(violations, Violation{
				Kind: "ownership-drift", Reference: id, Path: options.OwnershipPath,
				Message: fmt.Sprintf("mirror says R%d but BUILD_PLAN section 19 says R%d", mirrorGate, buildGate),
			})
		}
	}
	for id := range buildOwners {
		if !requirementSet[id] {
			violations = append(violations, Violation{
				Kind: "invented-build-owner", Reference: id, Path: options.BuildPlanPath,
				Message: "BUILD_PLAN section 19 cites an ID not defined by PRD sections 7 or 9",
			})
		}
	}
	for id := range mirror {
		if !requirementSet[id] {
			violations = append(violations, Violation{
				Kind: "invented-ownership", Reference: id, Path: options.OwnershipPath,
				Message: "ownership mirror cites an ID not defined by PRD sections 7 or 9",
			})
		}
	}

	for _, cited := range append(rowCitations, tests.Citations...) {
		if !requirementSet[cited.ID] {
			violations = append(violations, Violation{
				Kind: "invented-requirement", Reference: cited.ID, Path: cited.Path,
				Message: "test evidence cites an ID not defined by PRD sections 7 or 9",
			})
		}
	}

	highestStarted, highestAccepted := gateFrontiers(statuses)
	for _, row := range rows {
		if row.EarliestGate <= highestStarted && !tests.Rows[row.ID] {
			violations = append(violations, Violation{
				Kind: "missing-test-row", Reference: row.ID, Path: options.OperationsTestPlanPath,
				Message: fmt.Sprintf("earliest gate R%d has started but no Test%s function exists", row.EarliestGate, strings.ReplaceAll(row.ID, "-", "")),
			})
		}
		if tests.Rows[row.ID] {
			for _, id := range row.Requirements {
				tests.RequirementEvidence[id] = true
			}
		}
	}
	if highestAccepted >= 0 {
		for _, id := range requirements {
			owner, ok := mirror[id]
			if ok && owner <= highestAccepted && !tests.RequirementEvidence[id] {
				violations = append(violations, Violation{
					Kind: "missing-requirement-evidence", Reference: id, Path: options.OperationsTestPlanPath,
					Message: fmt.Sprintf("terminal owner R%d is accepted but no existing test cites the requirement", owner),
				})
			}
		}
	}

	sortViolations(violations)
	return deduplicateViolations(violations), nil
}

// CheckRepository applies the conventional repository paths.
func CheckRepository(ctx context.Context, root string) ([]Violation, error) {
	return Check(ctx, CheckOptions{
		ProductRequirementsPath: filepath.Join(root, "docs", "PRODUCT_REQUIREMENTS.md"),
		BuildPlanPath:           filepath.Join(root, "docs", "BUILD_PLAN.md"),
		OperationsTestPlanPath:  filepath.Join(root, "docs", "OPERATIONS_TEST_PLAN.md"),
		GateStatusPath:          filepath.Join(root, "docs", "gate-status.json"),
		OwnershipPath:           filepath.Join(root, "docs", "requirement-ownership.json"),
		TestRoots:               []string{root},
	})
}

func extractRequirementsFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open PRODUCT_REQUIREMENTS %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	ids, err := ExtractRequirementIDs(file)
	if err != nil {
		return nil, fmt.Errorf("extract PRODUCT_REQUIREMENTS %s: %w", path, err)
	}
	return ids, nil
}

func scanSections(reader io.Reader, selected map[int]bool) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	current := -1
	var content strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if match := sectionPattern.FindStringSubmatch(line); match != nil {
			number, err := strconv.Atoi(match[1])
			if err != nil {
				return "", fmt.Errorf("parse section number %q: %w", match[1], err)
			}
			current = number
		}
		if selected[current] {
			content.WriteString(line)
			content.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan markdown: %w", err)
	}
	return content.String(), nil
}

func uniqueRequirements(content string) []string {
	seen := make(map[string]bool)
	for _, id := range requirementPattern.FindAllString(content, -1) {
		seen[id] = true
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func extractBuildOwners(path string) (map[string]int, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open BUILD_PLAN %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	content, err := scanSections(file, map[int]bool{19: true})
	if err != nil {
		return nil, nil, fmt.Errorf("scan BUILD_PLAN %s: %w", path, err)
	}

	owners := make(map[string]int)
	var duplicates []string
	for _, line := range strings.Split(content, "\n") {
		ids := requirementPattern.FindAllString(line, -1)
		if len(ids) == 0 || !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := markdownCells(line)
		if len(cells) < 2 || cells[0] != ids[0] {
			continue
		}
		gate, ok := highestGate(cells[1])
		if !ok {
			return nil, nil, fmt.Errorf("BUILD_PLAN row %s has no terminal gate", ids[0])
		}
		if _, exists := owners[ids[0]]; exists {
			duplicates = append(duplicates, ids[0])
			continue
		}
		owners[ids[0]] = gate
	}
	sort.Strings(duplicates)
	return owners, duplicates, nil
}

func readOwnership(path string) (map[string]int, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read ownership mirror %s: %w", path, err)
	}
	var document ownershipFile
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, nil, fmt.Errorf("decode ownership mirror %s: %w", path, err)
	}
	if document.Version != 1 {
		return nil, nil, fmt.Errorf("ownership mirror %s has version %d; want 1", path, document.Version)
	}
	owners := make(map[string]int, len(document.Requirements))
	var duplicates []string
	for _, record := range document.Requirements {
		gate, ok := parseGate(record.TerminalGate)
		if !ok {
			return nil, nil, fmt.Errorf("ownership mirror %s gives %s invalid terminal gate %q", path, record.ID, record.TerminalGate)
		}
		if _, exists := owners[record.ID]; exists {
			duplicates = append(duplicates, record.ID)
			continue
		}
		owners[record.ID] = gate
	}
	sort.Strings(duplicates)
	return owners, duplicates, nil
}

func readGateStatuses(path string) (map[int]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read gate statuses %s: %w", path, err)
	}
	var raw map[string]string
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode gate statuses %s: %w", path, err)
	}
	statuses := make(map[int]string, 11)
	for gate := 0; gate <= 10; gate++ {
		name := fmt.Sprintf("R%d", gate)
		status, ok := raw[name]
		if !ok {
			return nil, fmt.Errorf("gate statuses %s omit %s", path, name)
		}
		switch status {
		case "planned", "in progress", "accepted", "deferred":
		default:
			return nil, fmt.Errorf("gate statuses %s give %s invalid status %q", path, name, status)
		}
		statuses[gate] = status
	}
	return statuses, nil
}

func extractMatrixRows(path string) ([]matrixRow, []string, []citation, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open OPERATIONS_TEST_PLAN %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	content, err := scanSections(file, map[int]bool{10: true})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("scan OPERATIONS_TEST_PLAN %s: %w", path, err)
	}

	byID := make(map[string]matrixRow)
	var duplicates []string
	var citations []citation
	for _, line := range strings.Split(content, "\n") {
		cells := markdownCells(line)
		if len(cells) < 4 || !rowPattern.MatchString(cells[0]) {
			continue
		}
		gate, ok := parseGate(cells[len(cells)-1])
		if !ok {
			return nil, nil, nil, fmt.Errorf("test row %s has invalid earliest gate %q", cells[0], cells[len(cells)-1])
		}
		ids := uniqueRequirements(line)
		row := matrixRow{ID: cells[0], EarliestGate: gate, Requirements: ids, Path: path}
		if _, exists := byID[row.ID]; exists {
			duplicates = append(duplicates, row.ID)
			continue
		}
		byID[row.ID] = row
		for _, id := range ids {
			citations = append(citations, citation{ID: id, Path: path})
		}
	}
	rows := make([]matrixRow, 0, len(byID))
	for _, row := range byID {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	sort.Strings(duplicates)
	return rows, duplicates, citations, nil
}

func discoverTests(ctx context.Context, roots []string) (testInventory, error) {
	inventory := testInventory{
		Rows:                make(map[string]bool),
		RequirementEvidence: make(map[string]bool),
	}
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return testInventory{}, err
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != root && skippedDirectory(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
			if err != nil {
				return fmt.Errorf("parse Go test %s: %w", path, err)
			}
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv != nil || function.Type.Params == nil || len(function.Type.Params.List) != 1 {
					continue
				}
				match := testNamePattern.FindStringSubmatch(function.Name.Name)
				if match == nil {
					continue
				}
				row := match[1] + "-" + match[2]
				inventory.Rows[row] = true
				if function.Doc != nil {
					for _, id := range uniqueRequirements(function.Doc.Text()) {
						inventory.RequirementEvidence[id] = true
						inventory.Citations = append(inventory.Citations, citation{ID: id, Path: path})
					}
				}
			}
			for _, group := range file.Comments {
				for _, id := range uniqueRequirements(group.Text()) {
					inventory.Citations = append(inventory.Citations, citation{ID: id, Path: path})
				}
			}
			return nil
		})
		if err != nil {
			return testInventory{}, fmt.Errorf("discover tests under %s: %w", root, err)
		}
	}
	return inventory, nil
}

func skippedDirectory(name string) bool {
	switch name {
	case ".git", "bin", "dist", "node_modules", "testdata", "vendor":
		return true
	default:
		return false
	}
}

func markdownCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return nil
	}
	parts := strings.Split(strings.Trim(trimmed, "|"), "|")
	for i := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(parts[i]), "`")
	}
	return parts
}

func highestGate(text string) (int, bool) {
	highest := -1
	for _, match := range gatePattern.FindAllStringSubmatch(text, -1) {
		gate, err := strconv.Atoi(match[1])
		if err == nil && gate > highest {
			highest = gate
		}
	}
	return highest, highest >= 0
}

func parseGate(text string) (int, bool) {
	trimmed := strings.TrimSpace(text)
	match := gatePattern.FindStringSubmatch(trimmed)
	if match == nil || match[0] != trimmed {
		return 0, false
	}
	gate, err := strconv.Atoi(match[1])
	return gate, err == nil
}

func gateFrontiers(statuses map[int]string) (started int, accepted int) {
	started, accepted = -1, -1
	for gate := 0; gate <= 10; gate++ {
		switch statuses[gate] {
		case "accepted":
			if gate > accepted {
				accepted = gate
			}
			if gate > started {
				started = gate
			}
		case "in progress":
			if gate > started {
				started = gate
			}
		}
	}
	return started, accepted
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func sortViolations(violations []Violation) {
	sort.Slice(violations, func(i, j int) bool {
		left := violations[i]
		right := violations[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Reference != right.Reference {
			return left.Reference < right.Reference
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Message < right.Message
	})
}

func deduplicateViolations(violations []Violation) []Violation {
	if len(violations) < 2 {
		return violations
	}
	result := violations[:1]
	for _, violation := range violations[1:] {
		previous := result[len(result)-1]
		if violation == previous {
			continue
		}
		result = append(result, violation)
	}
	return result
}
