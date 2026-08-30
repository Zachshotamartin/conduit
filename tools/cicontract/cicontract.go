// Command cicontract validates the checked-in GitHub Actions workflows
// against Conduit's permanent required-check contract.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ContextMapping records the workflow and displayed job name that produce a
// protected GitHub check context.
type ContextMapping struct {
	Context  string
	Workflow string
	Job      string
}

// Finding is one stable, actionable CI contract violation.
type Finding struct {
	Code    string
	File    string
	Path    string
	Message string
}

// Report is the complete result of checking one repository root.
type Report struct {
	Contexts []ContextMapping
	Findings []Finding
}

// Run executes the CI contract checker without terminating the process.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("cicontract", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root to inspect")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "cicontract: unexpected arguments: %v\n", flags.Args())
		return 2
	}

	report, err := Check(*root)
	if err != nil {
		fmt.Fprintf(stderr, "cicontract: %v\n", err)
		return 2
	}
	for _, finding := range report.Findings {
		location := finding.File
		if finding.Path != "" {
			location += ":" + finding.Path
		}
		fmt.Fprintf(stderr, "%s: %s: %s\n", finding.Code, location, finding.Message)
	}
	if len(report.Findings) != 0 {
		return 1
	}

	fmt.Fprintf(stdout, "cicontract: %d protected contexts verified\n", len(report.Contexts))
	return 0
}

func main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}

type workflowContract struct {
	name          string
	jobs          []string
	maxMinutes    int
	retentionDays int
}

var workflowContracts = []workflowContract{
	{
		name: "pr",
		jobs: []string{
			"lint", "vet", "arch-check", "unit-race", "proto-race",
			"authz-race", "index-race", "docs-status-lint",
			"metrics-contract", "deps-audit", "trace-check",
		},
		maxMinutes:    15,
		retentionDays: 30,
	},
	{
		name: "integration",
		jobs: []string{
			"conformance-node", "integration-nats", "integration-postgres", "socket-hostile",
		},
		maxMinutes:    25,
		retentionDays: 30,
	},
	{
		name: "nightly",
		jobs: []string{
			"fuzz", "soak-accelerated", "chaos-full", "nats-matrix",
			"bench-regression", "index-property-extended",
		},
		maxMinutes:    8 * 60,
		retentionDays: 90,
	},
	{
		name: "release",
		jobs: []string{
			"package", "provenance", "cross-version-fixtures", "image-scan", "kind-install",
		},
		maxMinutes:    60,
		retentionDays: 90,
	},
}

var raceJobs = []string{"unit-race", "proto-race", "authz-race", "index-race"}

var integrationPaths = []string{
	"internal/transport/**",
	"internal/bus/**",
	"internal/datasource/**",
	"internal/auth/**",
}

// Check reads branch-protection.json and the four workflow files below root.
// Read and syntax failures are returned as errors; semantic drift is returned
// as sorted Findings so callers can report every violation in one run.
func Check(root string) (Report, error) {
	protection, err := loadProtection(filepath.Join(root, ".github", "branch-protection.json"))
	if err != nil {
		return Report{}, err
	}

	workflows := make(map[string]*workflow, len(workflowContracts))
	report := Report{}
	for _, contract := range workflowContracts {
		relative := filepath.Join(".github", "workflows", contract.name+".yml")
		parsed, parseErr := parseWorkflow(filepath.Join(root, relative))
		if parseErr != nil {
			return Report{}, parseErr
		}
		workflows[contract.name] = parsed
		validateWorkflow(&report, relative, parsed, contract)
	}

	validateTriggers(&report, workflows)
	validateRaceJobs(&report, workflows["pr"])
	validateRequiredCommands(&report, workflows["pr"])
	validateVendorMode(&report, workflows)
	validateMacOSCorrectness(&report, workflows["pr"])
	validateContexts(&report, protection, workflows)

	sort.Slice(report.Contexts, func(i, j int) bool {
		return report.Contexts[i].Context < report.Contexts[j].Context
	})
	sort.Slice(report.Findings, func(i, j int) bool {
		left, right := report.Findings[i], report.Findings[j]
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Message < right.Message
	})
	return report, nil
}

type protectionFile struct {
	RequiredStatusChecks struct {
		Strict   bool     `json:"strict"`
		Contexts []string `json:"contexts"`
	} `json:"required_status_checks"`
}

func loadProtection(path string) (protectionFile, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return protectionFile{}, fmt.Errorf("read %s: %w", path, err)
	}
	var protection protectionFile
	if err := json.Unmarshal(contents, &protection); err != nil {
		return protectionFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return protection, nil
}

func validateWorkflow(report *Report, file string, workflow *workflow, contract workflowContract) {
	if workflow.Name != contract.name {
		report.add("protection.context", file, "name", fmt.Sprintf("workflow name is %q, want %q", workflow.Name, contract.name))
	}
	if workflow.PermissionScalar != "" || len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		report.add("workflow.permissions", file, "permissions", "permissions must be exactly contents: read")
	}

	wantedJobs := stringSet(contract.jobs)
	if contract.name == "pr" {
		wantedJobs["macos-correctness"] = true
	}
	for _, expected := range contract.jobs {
		job, exists := workflow.Jobs[expected]
		if !exists {
			report.add("workflow.jobs", file, "jobs", fmt.Sprintf("required job %q is missing", expected))
			continue
		}
		if job.Name != expected {
			report.add("workflow.jobs", file, "jobs."+expected+".name", fmt.Sprintf("displayed job name is %q, want %q", job.Name, expected))
		}
		if job.TimeoutMinutes <= 0 || job.TimeoutMinutes > contract.maxMinutes {
			report.add("job.timeout", file, "jobs."+expected+".timeout-minutes", fmt.Sprintf("timeout is %d minutes, want 1..%d", job.TimeoutMinutes, contract.maxMinutes))
		}
		if job.HasPermissions {
			report.add("workflow.permissions", file, "jobs."+expected+".permissions", "job-level permission overrides are forbidden")
		}
	}
	for jobName := range workflow.Jobs {
		if !wantedJobs[jobName] {
			report.add("workflow.jobs", file, "jobs."+jobName, "job is not in the normative workflow inventory")
		}
	}

	uploads := 0
	for jobName, job := range workflow.Jobs {
		for stepIndex, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
				continue
			}
			uploads++
			got, parseErr := strconv.Atoi(step.With["retention-days"])
			if parseErr != nil || got != contract.retentionDays {
				report.add(
					"artifact.retention",
					file,
					fmt.Sprintf("jobs.%s.steps.%d.with.retention-days", jobName, stepIndex),
					fmt.Sprintf("upload retention is %q, want %d days", step.With["retention-days"], contract.retentionDays),
				)
			}
		}
	}
	if uploads == 0 {
		report.add("artifact.retention", file, "jobs", fmt.Sprintf("workflow must upload diagnostic evidence with %d-day retention", contract.retentionDays))
	}
}

func validateTriggers(report *Report, workflows map[string]*workflow) {
	pr := workflows["pr"]
	if !pr.hasEvent("pull_request") || !equalStringSet(pr.event("push").Branches, []string{"main"}) {
		report.add("workflow.trigger", workflowFile("pr"), "on", "PR workflow must run for every pull request and pushes to main")
	}

	integration := workflows["integration"]
	if !integration.hasEvent("pull_request") ||
		!equalStringSet(integration.event("pull_request").Paths, integrationPaths) ||
		!equalStringSet(integration.event("push").Branches, []string{"main"}) {
		report.add("workflow.trigger", workflowFile("integration"), "on", "integration workflow must path-filter package PRs and run on every push to main")
	}

	nightly := workflows["nightly"]
	if !nightly.hasEvent("workflow_dispatch") || !hasDailyCron(nightly.event("schedule").Crons) {
		report.add("workflow.trigger", workflowFile("nightly"), "on", "nightly workflow must support manual dispatch and a daily cron")
	}

	release := workflows["release"]
	if !release.hasEvent("workflow_dispatch") || !equalStringSet(release.event("push").Tags, []string{"v*"}) {
		report.add("workflow.trigger", workflowFile("release"), "on", "release workflow must support manual dispatch and v* tags")
	}
}

func validateRaceJobs(report *Report, pr *workflow) {
	for _, jobName := range raceJobs {
		job, exists := pr.Jobs[jobName]
		if !exists {
			continue
		}
		found := false
		for _, step := range job.Steps {
			if isRaceTestCommand(step.Run) {
				found = true
				break
			}
		}
		if !found {
			report.add("job.race", workflowFile("pr"), "jobs."+jobName+".steps", "required race job must execute go test with -race")
		}
	}
}

func validateRequiredCommands(report *Report, pr *workflow) {
	requireJobCommands(report, pr, "lint", []string{
		"GO=go ./scripts/check-format.sh",
		"./scripts/check-determinism.sh",
		"staticcheck",
		"golangci-lint",
		"go run -mod=vendor ./tools/claimslint .",
	})
	requireJobCommands(report, pr, "docs-status-lint", []string{
		"go run -mod=vendor ./tools/docsstatus ./docs",
		"go run -mod=vendor ./tools/claimslint .",
	})
	requireJobCommands(report, pr, "deps-audit", []string{
		"go test -mod=vendor ./tools/depsaudit",
		"go run -mod=vendor ./tools/depsaudit -root .",
		"govulncheck",
	})
	requireJobCommands(report, pr, "trace-check", []string{
		"go test -mod=vendor ./tools/tracecheck",
		"go run -mod=vendor ./tools/tracecheck -root .",
	})
}

func requireJobCommands(report *Report, workflow *workflow, jobName string, required []string) {
	job := workflow.Jobs[jobName]
	if job == nil {
		return
	}
	commands := jobCommands(job)
	for _, command := range required {
		if !strings.Contains(commands, command) {
			report.add("job.command", workflowFile("pr"), "jobs."+jobName+".steps", fmt.Sprintf("required command %q is missing", command))
		}
	}
}

func validateVendorMode(report *Report, workflows map[string]*workflow) {
	for workflowName, workflow := range workflows {
		for jobName, job := range workflow.Jobs {
			for stepIndex, step := range job.Steps {
				for _, command := range projectGoCommandsWithoutVendor(step.Run) {
					report.add(
						"job.vendor-mode",
						workflowFile(workflowName),
						fmt.Sprintf("jobs.%s.steps.%d.run", jobName, stepIndex),
						fmt.Sprintf("%s must include -mod=vendor", command),
					)
				}
			}
		}
	}
}

func validateMacOSCorrectness(report *Report, pr *workflow) {
	job := pr.Jobs["macos-correctness"]
	if job == nil {
		report.add("platform.macos", workflowFile("pr"), "jobs.macos-correctness", "unprotected macOS correctness job is required")
		return
	}
	if job.Name != "macos-correctness" || job.RunsOn != "macos-14" {
		report.add("platform.macos", workflowFile("pr"), "jobs.macos-correctness", "macOS correctness job must be named macos-correctness and run on macos-14")
	}
	if job.TimeoutMinutes <= 0 || job.TimeoutMinutes > 15 {
		report.add("job.timeout", workflowFile("pr"), "jobs.macos-correctness.timeout-minutes", "macOS correctness timeout must be within 15 minutes")
	}
	if job.HasPermissions {
		report.add("workflow.permissions", workflowFile("pr"), "jobs.macos-correctness.permissions", "job-level permission overrides are forbidden")
	}
	commands := jobCommands(job)
	for _, required := range []string{
		"GO=go ./scripts/check-format.sh",
		"./scripts/check-determinism.sh",
		"go vet -mod=vendor ./...",
		"go test -mod=vendor -race -shuffle=on ./...",
		"go run -mod=vendor ./tools/archcheck",
		"go run -mod=vendor ./tools/cicontract -root .",
	} {
		if !strings.Contains(commands, required) {
			report.add("platform.macos", workflowFile("pr"), "jobs.macos-correctness.steps", fmt.Sprintf("required macOS command %q is missing", required))
		}
	}
}

func jobCommands(job *workflowJob) string {
	commands := make([]string, 0, len(job.Steps))
	for _, step := range job.Steps {
		if step.Run != "" {
			commands = append(commands, step.Run)
		}
	}
	return strings.Join(commands, "\n")
}

func projectGoCommandsWithoutVendor(command string) []string {
	fields := strings.Fields(command)
	var missing []string
	for index := 0; index+1 < len(fields); index++ {
		if cleanShellToken(fields[index]) != "go" {
			continue
		}
		subcommand := cleanShellToken(fields[index+1])
		switch subcommand {
		case "build", "run", "test", "vet":
		default:
			continue
		}
		vendor := false
		for cursor := index + 2; cursor < len(fields); cursor++ {
			token := cleanShellToken(fields[cursor])
			if token == "&&" || token == "||" || token == ";" || token == "|" {
				break
			}
			if token == "-mod=vendor" {
				vendor = true
				break
			}
		}
		if !vendor {
			missing = append(missing, "go "+subcommand)
		}
	}
	return missing
}

func cleanShellToken(token string) string {
	return strings.Trim(token, "\"'()")
}

func validateContexts(report *Report, protection protectionFile, workflows map[string]*workflow) {
	file := filepath.Join(".github", "branch-protection.json")
	if !protection.RequiredStatusChecks.Strict {
		report.add("protection.context", file, "required_status_checks.strict", "required status checks must be strict")
	}

	expected := make(map[string]ContextMapping)
	for _, contract := range workflowContracts[:2] {
		for _, job := range contract.jobs {
			context := contract.name + " / " + job
			expected[context] = ContextMapping{Context: context, Workflow: contract.name, Job: job}
		}
	}
	protected := make(map[string]bool, len(protection.RequiredStatusChecks.Contexts))
	for _, context := range protection.RequiredStatusChecks.Contexts {
		if protected[context] {
			report.add("protection.context", file, "required_status_checks.contexts", fmt.Sprintf("context %q is duplicated", context))
		}
		protected[context] = true
	}
	for context := range protected {
		if _, exists := expected[context]; !exists {
			report.add("protection.context", file, "required_status_checks.contexts", fmt.Sprintf("unexpected protected context %q", context))
		}
	}
	for context, mapping := range expected {
		if !protected[context] {
			report.add("protection.context", file, "required_status_checks.contexts", fmt.Sprintf("required context %q is missing", context))
			continue
		}
		workflow := workflows[mapping.Workflow]
		job, exists := workflow.Jobs[mapping.Job]
		if !exists || workflow.Name != mapping.Workflow || job.Name != mapping.Job {
			report.add("protection.context", workflowFile(mapping.Workflow), "jobs."+mapping.Job, fmt.Sprintf("protected context %q has no exact workflow/job producer", context))
			continue
		}
		report.Contexts = append(report.Contexts, mapping)
	}
}

func (report *Report) add(code, file, path, message string) {
	report.Findings = append(report.Findings, Finding{Code: code, File: file, Path: path, Message: message})
}

func workflowFile(name string) string {
	return filepath.Join(".github", "workflows", name+".yml")
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func equalStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftSet := stringSet(left)
	if len(leftSet) != len(right) {
		return false
	}
	for _, value := range right {
		if !leftSet[value] {
			return false
		}
	}
	return true
}

func hasDailyCron(crons []string) bool {
	for _, cron := range crons {
		fields := strings.Fields(cron)
		if len(fields) != 5 || fields[2] != "*" || fields[3] != "*" || fields[4] != "*" {
			continue
		}
		minute, minuteErr := strconv.Atoi(fields[0])
		hour, hourErr := strconv.Atoi(fields[1])
		if minuteErr == nil && hourErr == nil && minute >= 0 && minute <= 59 && hour >= 0 && hour <= 23 {
			return true
		}
	}
	return false
}

func isRaceTestCommand(command string) bool {
	fields := strings.Fields(command)
	goTest := false
	race := false
	for index, field := range fields {
		if field == "go" && index+1 < len(fields) && fields[index+1] == "test" {
			goTest = true
		}
		if field == "-race" {
			race = true
		}
	}
	return goTest && race
}

type workflow struct {
	Name             string
	Events           map[string]*workflowEvent
	Permissions      map[string]string
	PermissionScalar string
	Jobs             map[string]*workflowJob
}

type workflowEvent struct {
	Branches []string
	Tags     []string
	Paths    []string
	Crons    []string
}

type workflowJob struct {
	Name           string
	RunsOn         string
	TimeoutMinutes int
	HasPermissions bool
	Steps          []workflowStep
}

type workflowStep struct {
	Run  string
	Uses string
	With map[string]string
}

func (workflow *workflow) hasEvent(name string) bool {
	_, exists := workflow.Events[name]
	return exists
}

func (workflow *workflow) event(name string) *workflowEvent {
	event := workflow.Events[name]
	if event == nil {
		return &workflowEvent{}
	}
	return event
}

type yamlLine struct {
	number int
	indent int
	text   string
}

func parseWorkflow(path string) (*workflow, error) {
	lines, err := readYAMLLines(path)
	if err != nil {
		return nil, err
	}
	result := &workflow{
		Events:      make(map[string]*workflowEvent),
		Permissions: make(map[string]string),
		Jobs:        make(map[string]*workflowJob),
	}

	section := ""
	currentEvent := ""
	currentEventList := ""
	currentJob := ""
	inSteps := false
	currentStep := -1
	inWith := false
	for _, line := range lines {
		pairText := line.text
		sequenceItem := strings.HasPrefix(pairText, "- ")
		if sequenceItem {
			pairText = strings.TrimSpace(strings.TrimPrefix(pairText, "- "))
		}
		key, value, hasValue := "", pairText, pairText != ""
		if !sequenceItem || strings.ContainsRune(pairText, ':') {
			var splitErr error
			key, value, hasValue, splitErr = splitYAMLPair(pairText)
			if splitErr != nil {
				return nil, fmt.Errorf("parse %s:%d: %w", path, line.number, splitErr)
			}
		}

		if line.indent == 0 {
			section = key
			currentEvent = ""
			currentEventList = ""
			currentJob = ""
			inSteps = false
			currentStep = -1
			inWith = false
			switch key {
			case "name":
				result.Name = scalar(value)
			case "permissions":
				if hasValue {
					result.PermissionScalar = scalar(value)
				}
			case "on", "jobs":
			default:
				section = ""
			}
			continue
		}

		switch section {
		case "permissions":
			if line.indent == 2 {
				result.Permissions[key] = scalar(value)
			}
		case "on":
			if line.indent == 2 {
				currentEvent = key
				currentEventList = ""
				if _, exists := result.Events[key]; !exists {
					result.Events[key] = &workflowEvent{}
				}
				continue
			}
			if currentEvent == "" {
				continue
			}
			event := result.Events[currentEvent]
			if line.indent == 4 && strings.HasPrefix(line.text, "- ") {
				itemKey, itemValue, _, itemErr := splitYAMLPair(strings.TrimSpace(strings.TrimPrefix(line.text, "- ")))
				if itemErr != nil {
					return nil, fmt.Errorf("parse %s:%d: %w", path, line.number, itemErr)
				}
				if currentEvent == "schedule" && itemKey == "cron" {
					event.Crons = append(event.Crons, scalar(itemValue))
				}
				continue
			}
			if line.indent == 4 {
				currentEventList = key
				values := inlineList(value)
				appendEventValues(event, key, values)
				continue
			}
			if line.indent == 6 && strings.HasPrefix(line.text, "- ") {
				appendEventValues(event, currentEventList, []string{scalar(strings.TrimSpace(strings.TrimPrefix(line.text, "- ")))})
			}
		case "jobs":
			if line.indent == 2 {
				currentJob = key
				result.Jobs[key] = &workflowJob{}
				inSteps = false
				currentStep = -1
				inWith = false
				continue
			}
			if currentJob == "" {
				continue
			}
			job := result.Jobs[currentJob]
			if line.indent == 4 {
				inWith = false
				switch key {
				case "name":
					job.Name = scalar(value)
				case "runs-on":
					job.RunsOn = scalar(value)
				case "timeout-minutes":
					minutes, conversionErr := strconv.Atoi(scalar(value))
					if conversionErr != nil {
						return nil, fmt.Errorf("parse %s:%d: timeout-minutes must be an integer", path, line.number)
					}
					job.TimeoutMinutes = minutes
				case "permissions":
					job.HasPermissions = true
				case "steps":
					inSteps = true
				}
				continue
			}
			if !inSteps {
				continue
			}
			if line.indent == 6 && strings.HasPrefix(line.text, "- ") {
				job.Steps = append(job.Steps, workflowStep{With: make(map[string]string)})
				currentStep = len(job.Steps) - 1
				inWith = false
				item := strings.TrimSpace(strings.TrimPrefix(line.text, "- "))
				if item != "" {
					itemKey, itemValue, _, itemErr := splitYAMLPair(item)
					if itemErr != nil {
						return nil, fmt.Errorf("parse %s:%d: %w", path, line.number, itemErr)
					}
					assignStepValue(&job.Steps[currentStep], itemKey, scalar(itemValue))
				}
				continue
			}
			if currentStep < 0 {
				continue
			}
			step := &job.Steps[currentStep]
			if line.indent == 8 {
				inWith = key == "with"
				if !inWith {
					assignStepValue(step, key, scalar(value))
				}
				continue
			}
			if line.indent == 10 && inWith {
				step.With[key] = scalar(value)
			}
		}
	}
	return result, nil
}

func readYAMLLines(path string) ([]yamlLine, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()

	var lines []yamlLine
	scanner := bufio.NewScanner(file)
	for number := 1; scanner.Scan(); number++ {
		raw := strings.TrimRight(scanner.Text(), " \r\t")
		if strings.ContainsRune(raw, '\t') {
			return nil, fmt.Errorf("parse %s:%d: tabs are not permitted", path, number)
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if indent%2 != 0 {
			return nil, fmt.Errorf("parse %s:%d: indentation must use two-space levels", path, number)
		}
		lines = append(lines, yamlLine{number: number, indent: indent, text: strings.TrimSpace(raw)})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return lines, nil
}

func splitYAMLPair(text string) (key, value string, hasValue bool, err error) {
	if strings.HasPrefix(text, "- ") {
		return "", "", false, fmt.Errorf("unexpected sequence item %q", text)
	}
	separator := strings.IndexByte(text, ':')
	if separator < 1 {
		return "", "", false, fmt.Errorf("expected key: value, got %q", text)
	}
	key = strings.TrimSpace(text[:separator])
	value = strings.TrimSpace(text[separator+1:])
	return key, value, value != "", nil
}

func scalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	return value
}

func inlineList(value string) []string {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '[' || value[len(value)-1] != ']' {
		return nil
	}
	contents := strings.TrimSpace(value[1 : len(value)-1])
	if contents == "" {
		return []string{}
	}
	parts := strings.Split(contents, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		result = append(result, scalar(part))
	}
	return result
}

func appendEventValues(event *workflowEvent, key string, values []string) {
	switch key {
	case "branches":
		event.Branches = append(event.Branches, values...)
	case "tags":
		event.Tags = append(event.Tags, values...)
	case "paths":
		event.Paths = append(event.Paths, values...)
	}
}

func assignStepValue(step *workflowStep, key, value string) {
	switch key {
	case "run":
		step.Run = value
	case "uses":
		step.Uses = value
	}
}
