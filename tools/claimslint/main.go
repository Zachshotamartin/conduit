package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var (
	claimMarker           = regexp.MustCompile(`<!--\s*claim:(R[0-9]+)\s*-->`)
	htmlComment           = regexp.MustCompile(`<!--.*?-->`)
	htmlTag               = regexp.MustCompile(`<[^>]*>`)
	claimID               = regexp.MustCompile(`^C[0-9]+$`)
	evidenceLink          = regexp.MustCompile(`^\[[^]]+\]\([^)]+\)$`)
	claimReference        = regexp.MustCompile(`(?i)(?:\b(?:ADR|R|C|L|W|FR|NFR|UNIT)[-_ ]?[0-9]+(?:\.[0-9]+)*(?:[…–—-][0-9]+(?:\.[0-9]+)*)?\b|§\s*[0-9]+(?:\.[0-9]+)*)`)
	numericToken          = regexp.MustCompile(`(?i)\b[0-9]+(?:[,.][0-9]+)*(?:[kmbx]\b|×|\b)`)
	riskyMetric           = regexp.MustCompile(`(?i)(?:\b(?:benchmarks?|capacity|concurrent|connections?|deliver(?:y|ies)|envelopes?|events?|fleet|latency|memory|messages?|nodes?|operations?|publishes?|qps|rate|requests?|rps|scale|scalable|subscriptions?|throughput|users?)\b|\b(?:p50|p90|p95|p99)\b|\b(?:allocs?/op|ops?/s|req(?:uests)?/s)\b|\b(?:bytes?|[kmgt]i?b)\b|\bper[ -]connection\b)`)
	nonClaimQualifier     = regexp.MustCompile(`(?i)\b(?:committed\s+)?target\b|\bplanned\b|\bunimplemented\b|\bnot (?:yet )?(?:built|demonstrated|implemented)\b|\bno number is claimed\b`)
	realtimeQualification = regexp.MustCompile(`(?i)(?:\b(?:measured|observed|published)\b[^\r\n]{0,40}\blatency\b|\blatency\b[^\r\n]{0,40}(?:\bp(?:50|90|95|99)\b|\b[0-9]+(?:\.[0-9]+)?\s*(?:ns|us|µs|ms|s|secs?|seconds?)\b))`)
)

type requiredClaim struct {
	ID   string
	Gate string
}

var requiredClaims = []requiredClaim{
	{ID: "C1", Gate: "R1"},
	{ID: "C2", Gate: "R2"},
	{ID: "C3", Gate: "R3"},
	{ID: "C4", Gate: "R4"},
	{ID: "C5", Gate: "R5"},
	{ID: "C6", Gate: "R6"},
	{ID: "C7", Gate: "R7"},
	{ID: "C8", Gate: "R8"},
	{ID: "C9", Gate: "R9"},
	{ID: "C10", Gate: "R9"},
	{ID: "C11", Gate: "R10"},
}

var forbiddenPromotions = []struct {
	name       string
	expression *regexp.Regexp
}{
	{name: "guaranteed delivery", expression: regexp.MustCompile(`(?i)\bguaranteed(?:[[:space:]-]+)delivery\b`)},
	{name: "exactly-once", expression: regexp.MustCompile(`(?i)\bexactly(?:[[:space:]-]+)once\b`)},
	{name: "infinitely scalable", expression: regexp.MustCompile(`(?i)\binfinitely(?:[[:space:]-]+)scalable\b`)},
	{name: "zero downtime", expression: regexp.MustCompile(`(?i)\bzero(?:[[:space:]-]+)downtime\b`)},
	{name: "unqualified real-time", expression: regexp.MustCompile(`(?i)\breal(?:[[:space:]-]+)time\b`)},
}

var textAssetExtensions = map[string]bool{
	"": true, ".adoc": true, ".asciidoc": true, ".css": true,
	".csv": true, ".htm": true, ".html": true, ".js": true,
	".json": true, ".jsonc": true, ".jsx": true, ".less": true,
	".markdown": true, ".md": true, ".mdx": true, ".rst": true,
	".sass": true, ".scss": true, ".svelte": true, ".svg": true,
	".textile": true, ".toml": true, ".tpl": true, ".tmpl": true,
	".ts": true, ".tsv": true, ".tsx": true, ".txt": true,
	".vue": true, ".xml": true, ".yaml": true, ".yml": true,
}

type violation struct {
	Path    string
	Line    int
	Message string
}

func (v violation) String() string {
	return fmt.Sprintf("%s:%d: %s", v.Path, v.Line, v.Message)
}

func lintClaims(root string) ([]violation, error) {
	statuses, statusPath, err := loadGateStatuses(root)
	if err != nil {
		return nil, err
	}
	var violations []violation
	for gate, status := range statuses {
		if !validGateStatus(status) {
			violations = append(violations, violation{
				Path:    statusPath,
				Line:    1,
				Message: fmt.Sprintf("gate %s has invalid status %q", gate, status),
			})
		}
	}

	claimPaths, err := claimAssetPaths(root)
	if err != nil {
		return nil, err
	}
	for _, path := range claimPaths {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		violations = append(violations, lintMarkers(path, string(content), statuses)...)
		if isPromotionalAsset(root, path) {
			violations = append(violations, lintPromotionalClaims(path, string(content), statuses)...)
		}
	}

	marketingPath := filepath.Join(root, "docs", "MARKETING_PLAN.md")
	marketing, err := os.ReadFile(marketingPath)
	if err != nil {
		return nil, err
	}
	violations = append(violations, lintClaimsRegister(marketingPath, string(marketing), statuses)...)

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Path == violations[j].Path {
			if violations[i].Line == violations[j].Line {
				return violations[i].Message < violations[j].Message
			}
			return violations[i].Line < violations[j].Line
		}
		return violations[i].Path < violations[j].Path
	})
	return violations, nil
}

func claimAssetPaths(root string) ([]string, error) {
	paths := make(map[string]bool)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !isRootPublicAsset(entry.Name()) || !isTextAsset(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		paths[filepath.Join(root, entry.Name())] = true
	}

	for _, directory := range []string{"docs", "marketing", "release", "releases"} {
		base := filepath.Join(root, directory)
		if _, err := os.Stat(base); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !isTextAsset(path) {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			paths[path] = true
			return nil
		}); err != nil {
			return nil, err
		}
	}

	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	return ordered, nil
}

func isRootPublicAsset(name string) bool {
	stem := strings.TrimSuffix(strings.ToLower(name), strings.ToLower(filepath.Ext(name)))
	stem = strings.ReplaceAll(stem, "-", "_")
	return stem == "readme" || stem == "changelog" || stem == "releasenotes" || stem == "release_notes" ||
		strings.HasPrefix(stem, "readme_") || strings.HasPrefix(stem, "readme.") ||
		strings.HasPrefix(stem, "changelog_") || strings.HasPrefix(stem, "changelog.") ||
		strings.HasPrefix(stem, "releasenotes_") || strings.HasPrefix(stem, "release_notes_") || strings.HasPrefix(stem, "release_notes.")
}

func isTextAsset(path string) bool {
	return textAssetExtensions[strings.ToLower(filepath.Ext(path))]
}

func isPromotionalAsset(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) == 1 {
		return isRootPublicAsset(parts[0])
	}
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(part) {
		case "marketing", "release", "releases", "release-notes", "release_notes":
			return true
		}
	}
	base := strings.ToLower(parts[len(parts)-1])
	stem := strings.TrimSuffix(base, strings.ToLower(filepath.Ext(base)))
	stem = strings.ReplaceAll(stem, "-", "_")
	return stem == "changelog" || stem == "release_notes" || strings.HasPrefix(stem, "changelog_") || strings.HasPrefix(stem, "release_notes_")
}

func loadGateStatuses(root string) (map[string]string, string, error) {
	paths := []string{
		filepath.Join(root, "gate-status.json"),
		filepath.Join(root, "docs", "gate-status.json"),
	}
	var content []byte
	selected := ""
	for _, path := range paths {
		read, err := os.ReadFile(path)
		if err == nil {
			content = read
			selected = path
			break
		}
		if !os.IsNotExist(err) {
			return nil, path, err
		}
	}
	if selected == "" {
		return nil, paths[0], fmt.Errorf("gate status file not found at %s or %s", paths[0], paths[1])
	}
	var statuses map[string]string
	if err := json.Unmarshal(content, &statuses); err != nil {
		return nil, selected, fmt.Errorf("parse %s: %w", selected, err)
	}
	return statuses, selected, nil
}

func validGateStatus(status string) bool {
	switch status {
	case "accepted", "in progress", "planned", "deferred":
		return true
	default:
		return false
	}
}

func lintMarkers(path, content string, statuses map[string]string) []violation {
	var violations []violation
	for index, line := range strings.Split(content, "\n") {
		matches := claimMarker.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			continue
		}
		if !hasVisibleClaimText(line) {
			violations = append(violations, violation{
				Path: path, Line: index + 1,
				Message: "claim marker must be inline on the same line as the claim it authorizes",
			})
		}
		for _, match := range matches {
			gate := match[1]
			status, exists := statuses[gate]
			if !exists {
				violations = append(violations, violation{
					Path: path, Line: index + 1,
					Message: fmt.Sprintf("claim marker names unknown gate %s", gate),
				})
			} else if status != "accepted" {
				violations = append(violations, violation{
					Path: path, Line: index + 1,
					Message: fmt.Sprintf("claim marker for %s is forbidden while gate status is %q", gate, status),
				})
			}
		}
	}
	return violations
}

func hasVisibleClaimText(line string) bool {
	visible := claimMarker.ReplaceAllString(line, "")
	visible = htmlComment.ReplaceAllString(visible, "")
	visible = htmlTag.ReplaceAllString(visible, "")
	visible = strings.TrimFunc(visible, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	return visible != ""
}

func lintPromotionalClaims(path, content string, statuses map[string]string) []violation {
	var violations []violation
	lines := strings.Split(content, "\n")

	for _, forbidden := range forbiddenPromotions {
		for _, location := range forbidden.expression.FindAllStringIndex(content, -1) {
			lineNumber := 1 + strings.Count(content[:location[0]], "\n")
			if forbidden.name == "unqualified real-time" && realtimeQualification.MatchString(lines[lineNumber-1]) {
				continue
			}
			violations = append(violations, violation{
				Path: path, Line: lineNumber,
				Message: fmt.Sprintf("forbidden promotional claim %q", forbidden.name),
			})
		}
	}

	for index, line := range lines {
		claimText := claimMarker.ReplaceAllString(line, "")
		claimText = claimReference.ReplaceAllString(claimText, "")
		if !numericToken.MatchString(claimText) || !riskyMetric.MatchString(claimText) || nonClaimQualifier.MatchString(claimText) {
			continue
		}
		if lineHasAcceptedMarker(line, statuses) {
			continue
		}
		violations = append(violations, violation{
			Path: path, Line: index + 1,
			Message: "numeric performance or scale claim requires an accepted gate marker on the same line",
		})
	}

	return violations
}

func lineHasAcceptedMarker(line string, statuses map[string]string) bool {
	for _, match := range claimMarker.FindAllStringSubmatch(line, -1) {
		if statuses[match[1]] == "accepted" {
			return true
		}
	}
	return false
}

func lintClaimsRegister(path, content string, statuses map[string]string) []violation {
	var violations []violation
	lines := strings.Split(content, "\n")
	headerLine := 0
	for index, line := range lines {
		columns := markdownCells(line)
		if len(columns) != 0 && columns[0] == "#" {
			headerLine = index + 1
			want := []string{"#", "Claim (exact public sentence)", "Ladder level", "Gate", "Status", "Evidence"}
			if !equalStrings(columns, want) {
				return []violation{{
					Path: path, Line: headerLine,
					Message: "claims register header must be: # | Claim (exact public sentence) | Ladder level | Gate | Status | Evidence",
				}}
			}
			break
		}
	}
	if headerLine == 0 {
		return []violation{{Path: path, Line: 1, Message: "claims register is missing the required Evidence column header"}}
	}

	requiredGate := make(map[string]string, len(requiredClaims))
	for _, claim := range requiredClaims {
		requiredGate[claim.ID] = claim.Gate
	}
	seen := make(map[string]bool)
	for index, line := range lines {
		columns := markdownCells(line)
		if len(columns) == 0 {
			continue
		}
		currentClaimID := columns[0]
		if !claimID.MatchString(currentClaimID) {
			continue
		}
		if seen[currentClaimID] {
			violations = append(violations, violation{Path: path, Line: index + 1, Message: fmt.Sprintf("claim %s is duplicated", currentClaimID)})
			continue
		}
		seen[currentClaimID] = true
		expectedGate, required := requiredGate[currentClaimID]
		if !required {
			violations = append(violations, violation{Path: path, Line: index + 1, Message: fmt.Sprintf("claim %s is not in the required C1-C11 inventory", currentClaimID)})
			continue
		}
		if len(columns) != 6 {
			violations = append(violations, violation{Path: path, Line: index + 1, Message: fmt.Sprintf("claim %s must have exactly six columns including Evidence", currentClaimID)})
			continue
		}
		gate := columns[3]
		if gate != expectedGate {
			violations = append(violations, violation{
				Path: path, Line: index + 1,
				Message: fmt.Sprintf("claim %s must map to %s, got %s", currentClaimID, expectedGate, gate),
			})
		}
		claimStatus := columns[4]
		evidence := columns[5]
		gateStatus, exists := statuses[gate]
		if !exists {
			violations = append(violations, violation{
				Path:    path,
				Line:    index + 1,
				Message: fmt.Sprintf("claim %s names unknown gate %s", currentClaimID, gate),
			})
			continue
		}
		want := "unearned"
		if gateStatus == "accepted" {
			want = "earned"
		}
		if claimStatus != want {
			violations = append(violations, violation{
				Path:    path,
				Line:    index + 1,
				Message: fmt.Sprintf("claim %s status is %q, want %q while %s is %q", currentClaimID, claimStatus, want, gate, gateStatus),
			})
		}
		if claimStatus == "unearned" && evidence != "—" {
			violations = append(violations, violation{
				Path: path, Line: index + 1,
				Message: fmt.Sprintf("claim %s is unearned and must use — in Evidence", currentClaimID),
			})
		}
		if claimStatus == "earned" && !evidenceLink.MatchString(evidence) {
			violations = append(violations, violation{
				Path: path, Line: index + 1,
				Message: fmt.Sprintf("claim %s is earned and must carry a Markdown evidence link", currentClaimID),
			})
		}
	}
	for _, claim := range requiredClaims {
		if !seen[claim.ID] {
			violations = append(violations, violation{
				Path: path, Line: headerLine,
				Message: fmt.Sprintf("required claim %s mapped to %s is missing", claim.ID, claim.Gate),
			})
		}
	}
	return violations
}

func markdownCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return nil
	}
	parts := strings.Split(strings.Trim(trimmed, "|"), "|")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func main() {
	root := "."
	if len(os.Args) == 2 {
		root = os.Args[1]
	} else if len(os.Args) > 2 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: claimslint [repository-root]")
		os.Exit(2)
	}
	violations, err := lintClaims(root)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "claims lint: %v\n", err)
		os.Exit(2)
	}
	for _, item := range violations {
		_, _ = fmt.Fprintln(os.Stderr, item.String())
	}
	if len(violations) != 0 {
		os.Exit(1)
	}
}
