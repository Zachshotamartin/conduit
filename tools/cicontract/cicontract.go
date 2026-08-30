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

const (
	checkoutActionUse       = "actions/checkout@11d5960a326750d5838078e36cf38b85af677262"
	setupGoActionUse        = "actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff"
	uploadArtifactActionUse = "actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02"
	govulncheckInstall      = "go install golang.org/x/vuln/cmd/govulncheck@v1.1.4"
	govulncheckRun          = `"$(go env GOPATH)/bin/govulncheck" ./...`
)

var allowedActionUses = map[string]bool{
	checkoutActionUse:       true,
	setupGoActionUse:        true,
	uploadArtifactActionUse: true,
}

var exactRaceCommands = map[string]string{
	"unit-race":  "go test -mod=vendor -race -shuffle=on ./...",
	"proto-race": "go test -mod=vendor -race -shuffle=on ./internal/protocol/...",
	"authz-race": "go test -mod=vendor -race -shuffle=on ./internal/auth/...",
	"index-race": "go test -mod=vendor -race -shuffle=on ./internal/filter/index/...",
}

var exactProtectedJobCommands = map[string]map[string][]string{
	"pr": {
		"lint": {
			"GO=go ./scripts/check-format.sh",
			"./scripts/check-determinism.sh",
			`"$(go env GOPATH)/bin/staticcheck" ./...`,
			`"$(go env GOPATH)/bin/golangci-lint" run`,
			"go run -mod=vendor ./tools/claimslint .",
			"go run -mod=vendor ./tools/cicontract -root .",
		},
		"vet":              {"go vet -mod=vendor ./..."},
		"arch-check":       {"go run -mod=vendor ./tools/archcheck"},
		"docs-status-lint": {"go run -mod=vendor ./tools/docsstatus ./docs", "go run -mod=vendor ./tools/claimslint ."},
		"metrics-contract": {"go test -mod=vendor ./internal/observability/..."},
		"deps-audit": {
			"go test -mod=vendor ./tools/depsaudit",
			"go run -mod=vendor ./tools/depsaudit -root .",
			govulncheckInstall,
			govulncheckRun,
		},
		"trace-check": {
			"go test -mod=vendor ./tools/tracecheck ./internal/observability/... ./internal/transport/...",
			"go run -mod=vendor ./tools/tracecheck -root .",
		},
	},
	"integration": {
		"conformance-node":     {"go test -mod=vendor -race ./test/conformance/... ./internal/protocol/..."},
		"integration-nats":     {"go test -mod=vendor -race ./internal/bus/nats/... ./test/fault/..."},
		"integration-postgres": {"go test -mod=vendor -race ./internal/datasource/postgres/..."},
		"socket-hostile":       {"go test -mod=vendor -race ./test/hostile/... ./internal/transport/..."},
	},
}

var platformCorrectnessCommands = []string{
	"GO=go ./scripts/check-format.sh",
	"./scripts/check-determinism.sh",
	"go vet -mod=vendor ./...",
	"go test -mod=vendor -race -shuffle=on ./...",
	"go run -mod=vendor ./tools/archcheck",
	"go run -mod=vendor ./tools/docsstatus ./docs",
	"go run -mod=vendor ./tools/claimslint .",
	"go run -mod=vendor ./tools/cicontract -root .",
	"go run -mod=vendor ./tools/depsaudit -root .",
	"go run -mod=vendor ./tools/tracecheck -root .",
}

var exactJobRunCommands = map[string]map[string][]string{
	"pr": {
		"lint": {
			"GO=go ./scripts/check-format.sh",
			"./scripts/check-determinism.sh",
			"go install honnef.co/go/tools/cmd/staticcheck@v0.5.1",
			`"$(go env GOPATH)/bin/staticcheck" ./...`,
			"go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2",
			`"$(go env GOPATH)/bin/golangci-lint" run`,
			"go run -mod=vendor ./tools/claimslint .",
			"go run -mod=vendor ./tools/cicontract -root .",
		},
		"vet":              {"go vet -mod=vendor ./..."},
		"arch-check":       {"go run -mod=vendor ./tools/archcheck"},
		"unit-race":        {"go test -mod=vendor -race -shuffle=on ./..."},
		"proto-race":       {"go test -mod=vendor -race -shuffle=on ./internal/protocol/..."},
		"authz-race":       {"go test -mod=vendor -race -shuffle=on ./internal/auth/..."},
		"index-race":       {"go test -mod=vendor -race -shuffle=on ./internal/filter/index/..."},
		"docs-status-lint": {"go run -mod=vendor ./tools/docsstatus ./docs", "go run -mod=vendor ./tools/claimslint ."},
		"metrics-contract": {"go test -mod=vendor ./internal/observability/..."},
		"deps-audit": {
			"go test -mod=vendor ./tools/depsaudit",
			"go run -mod=vendor ./tools/depsaudit -root .",
			govulncheckInstall,
			govulncheckRun,
		},
		"trace-check": {
			"go test -mod=vendor ./tools/tracecheck ./internal/observability/... ./internal/transport/...",
			"go run -mod=vendor ./tools/tracecheck -root .",
		},
		"macos-correctness":       append([]string{"go env GOOS GOARCH"}, platformCorrectnessCommands...),
		"linux-arm64-correctness": append([]string{"go env GOOS GOARCH"}, platformCorrectnessCommands...),
	},
	"integration": {
		"conformance-node":     {"go test -mod=vendor -race ./test/conformance/... ./internal/protocol/..."},
		"integration-nats":     {"go test -mod=vendor -race ./internal/bus/nats/... ./test/fault/..."},
		"integration-postgres": {"go test -mod=vendor -race ./internal/datasource/postgres/..."},
		"socket-hostile":       {"go test -mod=vendor -race ./test/hostile/... ./internal/transport/..."},
	},
	"nightly": {
		"fuzz": {
			"go test -mod=vendor -race ./internal/protocol/... ./internal/graphql/... ./internal/resume/...",
			govulncheckInstall,
			govulncheckRun,
		},
		"soak-accelerated":        {"go test -mod=vendor -race ./internal/queue/... ./internal/fanout/..."},
		"chaos-full":              {"go test -mod=vendor -race ./internal/bus/memory/... ./test/fault/..."},
		"nats-matrix":             {"go test -mod=vendor -race ./internal/bus/nats/..."},
		"bench-regression":        {"go test -mod=vendor -bench=. ./..."},
		"index-property-extended": {"go test -mod=vendor -race ./internal/filter/..."},
	},
	"release": {
		"package":                {"go test -mod=vendor ./cmd/..."},
		"provenance":             {"go test -mod=vendor ./cmd/... ./internal/observability/..."},
		"cross-version-fixtures": {"go test -mod=vendor ./test/fixtures/..."},
		"image-scan":             {"go test -mod=vendor ./cmd/conduit/..."},
		"kind-install":           {"go test -mod=vendor ./test/conformance/... ./cmd/conduit/..."},
	},
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

	validateProtectionPolicy(&report, protection)
	validateTriggers(&report, workflows)
	validateExactRunInventories(&report, workflows)
	validateRaceJobs(&report, workflows["pr"])
	validateRequiredCommands(&report, workflows)
	validateNightlyGovulncheck(&report, workflows["nightly"])
	validateVendorMode(&report, workflows)
	validatePlatformCorrectness(&report, workflows["pr"], platformContract{
		job: "macos-correctness", runner: "macos-14", code: "platform.macos", label: "macOS",
	})
	validatePlatformCorrectness(&report, workflows["pr"], platformContract{
		job: "linux-arm64-correctness", runner: "ubuntu-24.04-arm", code: "platform.linux-arm64", label: "Linux arm64",
	})
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
	fields map[string]json.RawMessage
}

func loadProtection(path string) (protectionFile, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return protectionFile{}, fmt.Errorf("read %s: %w", path, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(contents, &fields); err != nil {
		return protectionFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if fields == nil {
		return protectionFile{}, fmt.Errorf("parse %s: top-level value must be an object", path)
	}
	return protectionFile{fields: fields}, nil
}

func validateWorkflow(report *Report, file string, workflow *workflow, contract workflowContract) {
	if workflow.Name != contract.name {
		report.add("protection.context", file, "name", fmt.Sprintf("workflow name is %q, want %q", workflow.Name, contract.name))
	}
	if workflow.PermissionScalar != "" || len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		report.add("workflow.permissions", file, "permissions", "permissions must be exactly contents: read")
	}
	if workflow.HasEnv {
		report.add("workflow.environment", file, "env", "workflow-level environment overrides are forbidden")
	}
	if workflow.HasDefaults {
		report.add("workflow.environment", file, "defaults", "workflow-level run defaults are forbidden")
	}
	for _, key := range workflow.UnexpectedKeys {
		report.add("workflow.structure", file, key, "top-level workflow key is not in the exact contract")
	}

	wantedJobs := stringSet(contract.jobs)
	if contract.name == "pr" {
		wantedJobs["macos-correctness"] = true
		wantedJobs["linux-arm64-correctness"] = true
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
		if job.RunsOn != "ubuntu-latest" {
			report.add("job.runner", file, "jobs."+expected+".runs-on", fmt.Sprintf("runner is %q, want exactly ubuntu-latest", job.RunsOn))
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
		validateJobBootstrap(report, file, jobName, job)
		if job.HasIf {
			report.add("job.condition", file, "jobs."+jobName+".if", fmt.Sprintf("job-level if condition %q is forbidden because every declared check must report", job.If))
		}
		if job.HasContinueOnError {
			report.add("job.continue-on-error", file, "jobs."+jobName+".continue-on-error", fmt.Sprintf("job-level continue-on-error value %q is forbidden", job.ContinueOnError))
		}
		if job.HasEnv {
			report.add("job.environment", file, "jobs."+jobName+".env", "job-level environment overrides are forbidden")
		}
		if job.HasDefaults {
			report.add("job.environment", file, "jobs."+jobName+".defaults", "job-level run defaults are forbidden")
		}
		for _, key := range job.UnexpectedKeys {
			report.add("job.structure", file, "jobs."+jobName+"."+key, "job key is not in the exact contract")
		}
		if job.Uses != "" {
			report.add("action.pin", file, "jobs."+jobName+".uses", fmt.Sprintf("job-level reusable workflow use %q is not approved", job.Uses))
		}
		for stepIndex, step := range job.Steps {
			stepPath := fmt.Sprintf("jobs.%s.steps.%d", jobName, stepIndex)
			if step.HasShell {
				report.add("step.environment", file, stepPath+".shell", fmt.Sprintf("step-level shell override %q is forbidden", step.Shell))
			}
			if step.HasWorkingDirectory {
				report.add("step.environment", file, stepPath+".working-directory", fmt.Sprintf("step-level working-directory override %q is forbidden", step.WorkingDirectory))
			}
			if step.HasEnv && !allowedStepEnvironment(step) {
				report.add("step.environment", file, stepPath+".env", "step environment must be absent or exactly GOFLAGS=-mod=vendor on an approved analyzer command")
			}
			for _, key := range step.UnexpectedKeys {
				report.add("step.structure", file, stepPath+"."+key, "step key is not in the exact contract")
			}
			if step.Run != "" && step.Uses != "" {
				report.add("step.structure", file, stepPath, "a step cannot contain both run and uses")
			}
			if step.Run != "" && len(step.With) != 0 {
				report.add("step.structure", file, stepPath+".with", "run steps cannot contain action inputs")
			}
			if step.HasIf && !isUploadArtifact(step.Uses) {
				report.add("step.condition", file, stepPath+".if", fmt.Sprintf("step-level if condition %q is allowed only on upload-artifact evidence steps", step.If))
			}
			if step.HasContinueOnError {
				report.add("step.continue-on-error", file, stepPath+".continue-on-error", fmt.Sprintf("step-level continue-on-error value %q is forbidden", step.ContinueOnError))
			}
			if step.Uses != "" && !allowedActionUses[step.Uses] {
				report.add("action.pin", file, stepPath+".uses", fmt.Sprintf("action use %q is not one of the exact approved SHA pins", step.Uses))
			}
			if !isUploadArtifact(step.Uses) {
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

func validateJobBootstrap(report *Report, file, jobName string, job *workflowJob) {
	checkoutCount := 0
	setupGoCount := 0
	for stepIndex, step := range job.Steps {
		stepPath := fmt.Sprintf("jobs.%s.steps.%d", jobName, stepIndex)
		if step.Uses == checkoutActionUse {
			checkoutCount++
			if len(step.With) != 0 {
				report.add("job.bootstrap", file, stepPath+".with", "checkout must not receive configuration")
			}
		}
		if step.Uses != setupGoActionUse {
			continue
		}
		setupGoCount++
		want := map[string]string{
			"go-version":   "1.23.12",
			"check-latest": "false",
			"cache":        "false",
		}
		if !equalStringMap(step.With, want) {
			report.add("job.bootstrap", file, stepPath+".with", "setup-go configuration must be exactly go-version=1.23.12, check-latest=false, cache=false")
		}
	}
	if len(job.Steps) == 0 || job.Steps[0].Uses != checkoutActionUse {
		report.add("job.bootstrap", file, "jobs."+jobName+".steps.0", "checkout must be the first job step")
	}
	if len(job.Steps) < 2 || job.Steps[1].Uses != setupGoActionUse {
		report.add("job.bootstrap", file, "jobs."+jobName+".steps.1", "setup-go must be the second job step")
	}
	if checkoutCount != 1 {
		report.add("job.bootstrap", file, "jobs."+jobName+".steps", fmt.Sprintf("job must contain exactly one checkout step pinned to %s; found %d", checkoutActionUse, checkoutCount))
	}
	if setupGoCount != 1 {
		report.add("job.bootstrap", file, "jobs."+jobName+".steps", fmt.Sprintf("job must contain exactly one setup-go step pinned to %s; found %d", setupGoActionUse, setupGoCount))
	}
}

func allowedStepEnvironment(step workflowStep) bool {
	if !step.HasEnv {
		return true
	}
	if !equalStringMap(step.Env, map[string]string{"GOFLAGS": "-mod=vendor"}) {
		return false
	}
	command := strings.TrimSpace(step.Run)
	return command == `"$(go env GOPATH)/bin/staticcheck" ./...` ||
		command == `"$(go env GOPATH)/bin/golangci-lint" run` ||
		command == govulncheckRun
}

func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range right {
		if left[key] != value {
			return false
		}
	}
	return true
}

func validateTriggers(report *Report, workflows map[string]*workflow) {
	pr := workflows["pr"]
	if !pr.hasEvent("pull_request") ||
		len(pr.event("pull_request").Paths) != 0 ||
		len(pr.event("pull_request").Branches) != 0 ||
		!equalStringSet(pr.event("push").Branches, []string{"main"}) {
		report.add("workflow.trigger", workflowFile("pr"), "on", "PR workflow must run for every pull request and pushes to main")
	}

	integration := workflows["integration"]
	if !integration.hasEvent("pull_request") ||
		len(integration.event("pull_request").Paths) != 0 ||
		len(integration.event("pull_request").Branches) != 0 ||
		!equalStringSet(integration.event("push").Branches, []string{"main"}) {
		report.add("workflow.trigger", workflowFile("integration"), "on", "required integration contexts must run for every pull request and pushes to main; workflow-level path filters are forbidden")
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
		command := exactRaceCommands[jobName]
		if !hasExactRunStep(job, command) {
			report.add("job.race", workflowFile("pr"), "jobs."+jobName+".steps", fmt.Sprintf("required race job must execute exact command %q", command))
		}
	}
}

func validateRequiredCommands(report *Report, workflows map[string]*workflow) {
	for workflowName, jobs := range exactProtectedJobCommands {
		workflow := workflows[workflowName]
		for jobName, required := range jobs {
			requireExactJobCommands(report, workflowName, workflow, jobName, required)
		}
	}
}

func validateExactRunInventories(report *Report, workflows map[string]*workflow) {
	for workflowName, jobs := range exactJobRunCommands {
		workflow := workflows[workflowName]
		for jobName, want := range jobs {
			job := workflow.Jobs[jobName]
			if job == nil {
				continue
			}
			got := make([]string, 0, len(job.Steps))
			for _, step := range job.Steps {
				if step.Run != "" {
					got = append(got, strings.TrimSpace(step.Run))
				}
			}
			if equalStringSlice(got, want) {
				continue
			}
			report.add("job.command-inventory", workflowFile(workflowName), "jobs."+jobName+".steps", fmt.Sprintf("run command sequence is %q, want exact sequence %q", got, want))
		}
	}
}

func equalStringSlice(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range right {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func requireExactJobCommands(report *Report, workflowName string, workflow *workflow, jobName string, required []string) {
	job := workflow.Jobs[jobName]
	if job == nil {
		return
	}
	for _, command := range required {
		if !hasExactRunStep(job, command) {
			report.add("job.command", workflowFile(workflowName), "jobs."+jobName+".steps", fmt.Sprintf("required exact command %q is missing", command))
		}
	}
}

func validateNightlyGovulncheck(report *Report, nightly *workflow) {
	job := nightly.Jobs["fuzz"]
	if job == nil {
		return
	}
	for _, command := range []string{govulncheckInstall, govulncheckRun} {
		if !hasExactRunStep(job, command) {
			report.add("nightly.govulncheck", workflowFile("nightly"), "jobs.fuzz.steps", fmt.Sprintf("nightly vulnerability scan must execute exact command %q", command))
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

type platformContract struct {
	job    string
	runner string
	code   string
	label  string
}

func validatePlatformCorrectness(report *Report, pr *workflow, contract platformContract) {
	job := pr.Jobs[contract.job]
	if job == nil {
		report.add(contract.code, workflowFile("pr"), "jobs."+contract.job, fmt.Sprintf("unprotected %s correctness job is required", contract.label))
		return
	}
	if job.Name != contract.job || job.RunsOn != contract.runner {
		report.add(contract.code, workflowFile("pr"), "jobs."+contract.job, fmt.Sprintf("%s correctness job must be named %s and run on %s", contract.label, contract.job, contract.runner))
	}
	if job.TimeoutMinutes <= 0 || job.TimeoutMinutes > 15 {
		report.add("job.timeout", workflowFile("pr"), "jobs."+contract.job+".timeout-minutes", fmt.Sprintf("%s correctness timeout must be within 15 minutes", contract.label))
	}
	if job.HasPermissions {
		report.add("workflow.permissions", workflowFile("pr"), "jobs."+contract.job+".permissions", "job-level permission overrides are forbidden")
	}
	for _, required := range platformCorrectnessCommands {
		if !hasExactRunStep(job, required) {
			report.add(contract.code, workflowFile("pr"), "jobs."+contract.job+".steps", fmt.Sprintf("required %s command %q is missing", contract.label, required))
		}
	}
}

func hasExactRunStep(job *workflowJob, command string) bool {
	for _, step := range job.Steps {
		if strings.TrimSpace(step.Run) == command {
			return true
		}
	}
	return false
}

func isUploadArtifact(use string) bool {
	action, _, found := strings.Cut(use, "@")
	return found && action == "actions/upload-artifact"
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

func validateProtectionPolicy(report *Report, protection protectionFile) {
	const file = ".github/branch-protection.json"
	validateJSONFields(report, "protection.policy", file, "", protection.fields, []string{
		"required_status_checks",
		"enforce_admins",
		"required_pull_request_reviews",
		"required_linear_history",
		"allow_force_pushes",
		"allow_deletions",
		"restrictions",
	})
	validateJSONBool(report, "protection.policy", file, protection.fields, "enforce_admins", true)
	validateJSONBool(report, "protection.policy", file, protection.fields, "required_linear_history", true)
	validateJSONBool(report, "protection.policy", file, protection.fields, "allow_force_pushes", false)
	validateJSONBool(report, "protection.policy", file, protection.fields, "allow_deletions", false)

	restrictions, exists := protection.fields["restrictions"]
	if exists && strings.TrimSpace(string(restrictions)) != "null" {
		report.add("protection.policy", file, "restrictions", "restrictions must be exactly null")
	}

	reviews, ok := jsonObject(report, "protection.policy", file, "required_pull_request_reviews", protection.fields["required_pull_request_reviews"])
	if !ok {
		return
	}
	validateJSONFields(report, "protection.policy", file, "required_pull_request_reviews", reviews, []string{
		"required_approving_review_count",
		"dismiss_stale_reviews",
	})
	validateJSONInteger(report, "protection.policy", file, reviews, "required_pull_request_reviews.required_approving_review_count", "required_approving_review_count", 1)
	validateJSONBoolAt(report, "protection.policy", file, reviews, "required_pull_request_reviews.dismiss_stale_reviews", "dismiss_stale_reviews", true)
}

func validateJSONFields(report *Report, code, file, prefix string, fields map[string]json.RawMessage, expected []string) {
	wanted := stringSet(expected)
	for _, field := range expected {
		if _, exists := fields[field]; exists {
			continue
		}
		report.add(code, file, joinJSONPath(prefix, field), "required policy field is missing")
	}
	for field := range fields {
		if wanted[field] {
			continue
		}
		report.add(code, file, joinJSONPath(prefix, field), "field is not part of the exact normative policy")
	}
}

func validateJSONBool(report *Report, code, file string, fields map[string]json.RawMessage, key string, want bool) {
	validateJSONBoolAt(report, code, file, fields, key, key, want)
}

func validateJSONBoolAt(report *Report, code, file string, fields map[string]json.RawMessage, path, key string, want bool) {
	raw, exists := fields[key]
	if !exists {
		return
	}
	var got bool
	trimmed := strings.TrimSpace(string(raw))
	if err := json.Unmarshal(raw, &got); err != nil || (trimmed != "true" && trimmed != "false") {
		report.add(code, file, path, fmt.Sprintf("value must be the boolean %t", want))
		return
	}
	if got != want {
		report.add(code, file, path, fmt.Sprintf("value is %t, want %t", got, want))
	}
}

func validateJSONInteger(report *Report, code, file string, fields map[string]json.RawMessage, path, key string, want int) {
	raw, exists := fields[key]
	if !exists {
		return
	}
	var got int
	if err := json.Unmarshal(raw, &got); err != nil {
		report.add(code, file, path, fmt.Sprintf("value must be the integer %d", want))
		return
	}
	if got != want {
		report.add(code, file, path, fmt.Sprintf("value is %d, want %d", got, want))
	}
}

func jsonObject(report *Report, code, file, path string, raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		report.add(code, file, path, "value must be an object")
		return nil, false
	}
	return object, true
}

func joinJSONPath(prefix, field string) string {
	if prefix == "" {
		return field
	}
	return prefix + "." + field
}

func validateContexts(report *Report, protection protectionFile, workflows map[string]*workflow) {
	file := filepath.Join(".github", "branch-protection.json")
	checks, ok := jsonObject(report, "protection.context", file, "required_status_checks", protection.fields["required_status_checks"])
	if !ok {
		return
	}
	validateJSONFields(report, "protection.context", file, "required_status_checks", checks, []string{"strict", "contexts"})
	validateJSONBoolAt(report, "protection.context", file, checks, "required_status_checks.strict", "strict", true)

	var contexts []string
	if raw, exists := checks["contexts"]; exists {
		if err := json.Unmarshal(raw, &contexts); err != nil || contexts == nil {
			report.add("protection.context", file, "required_status_checks.contexts", "contexts must be an array of strings")
			return
		}
	}

	expected := make(map[string]ContextMapping)
	for _, contract := range workflowContracts[:2] {
		for _, job := range contract.jobs {
			context := contract.name + " / " + job
			expected[context] = ContextMapping{Context: context, Workflow: contract.name, Job: job}
		}
	}
	protected := make(map[string]bool, len(contexts))
	for _, context := range contexts {
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

type workflow struct {
	Name             string
	Events           map[string]*workflowEvent
	Permissions      map[string]string
	PermissionScalar string
	Jobs             map[string]*workflowJob
	HasEnv           bool
	HasDefaults      bool
	UnexpectedKeys   []string
}

type workflowEvent struct {
	Branches []string
	Tags     []string
	Paths    []string
	Crons    []string
}

type workflowJob struct {
	Name               string
	RunsOn             string
	TimeoutMinutes     int
	HasPermissions     bool
	Uses               string
	HasIf              bool
	If                 string
	HasContinueOnError bool
	ContinueOnError    string
	HasEnv             bool
	HasDefaults        bool
	Steps              []workflowStep
	UnexpectedKeys     []string
}

type workflowStep struct {
	Run                 string
	Uses                string
	HasIf               bool
	If                  string
	HasContinueOnError  bool
	ContinueOnError     string
	With                map[string]string
	HasEnv              bool
	Env                 map[string]string
	HasShell            bool
	Shell               string
	HasWorkingDirectory bool
	WorkingDirectory    string
	UnexpectedKeys      []string
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
	inEnv := false
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
			inEnv = false
			switch key {
			case "name":
				result.Name = scalar(value)
			case "permissions":
				if hasValue {
					result.PermissionScalar = scalar(value)
				}
			case "env":
				result.HasEnv = true
				section = ""
			case "defaults":
				result.HasDefaults = true
				section = ""
			case "on", "jobs":
			default:
				result.UnexpectedKeys = append(result.UnexpectedKeys, key)
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
				inEnv = false
				continue
			}
			if currentJob == "" {
				continue
			}
			job := result.Jobs[currentJob]
			if line.indent == 4 {
				inWith = false
				inEnv = false
				switch key {
				case "name":
					job.Name = scalar(value)
				case "runs-on":
					job.RunsOn = scalar(value)
				case "uses":
					job.Uses = scalar(value)
				case "if":
					job.HasIf = true
					job.If = scalar(value)
				case "continue-on-error":
					job.HasContinueOnError = true
					job.ContinueOnError = scalar(value)
				case "timeout-minutes":
					minutes, conversionErr := strconv.Atoi(scalar(value))
					if conversionErr != nil {
						return nil, fmt.Errorf("parse %s:%d: timeout-minutes must be an integer", path, line.number)
					}
					job.TimeoutMinutes = minutes
				case "permissions":
					job.HasPermissions = true
				case "env":
					job.HasEnv = true
				case "defaults":
					job.HasDefaults = true
				case "steps":
					inSteps = true
				default:
					job.UnexpectedKeys = append(job.UnexpectedKeys, key)
				}
				continue
			}
			if !inSteps {
				continue
			}
			if line.indent == 6 && strings.HasPrefix(line.text, "- ") {
				job.Steps = append(job.Steps, workflowStep{With: make(map[string]string), Env: make(map[string]string)})
				currentStep = len(job.Steps) - 1
				inWith = false
				inEnv = false
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
				inEnv = key == "env"
				if inEnv {
					step.HasEnv = true
				}
				if !inWith && !inEnv {
					assignStepValue(step, key, scalar(value))
				}
				continue
			}
			if line.indent == 10 && inWith {
				step.With[key] = scalar(value)
			} else if line.indent == 10 && inEnv {
				step.Env[key] = scalar(value)
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
	case "if":
		step.HasIf = true
		step.If = value
	case "continue-on-error":
		step.HasContinueOnError = true
		step.ContinueOnError = value
	case "shell":
		step.HasShell = true
		step.Shell = value
	case "working-directory":
		step.HasWorkingDirectory = true
		step.WorkingDirectory = value
	case "name":
	default:
		step.UnexpectedKeys = append(step.UnexpectedKeys, key)
	}
}
