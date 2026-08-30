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
)

var (
	claimMarker  = regexp.MustCompile(`<!--\s*claim:(R[0-9]+)\s*-->`)
	claimID      = regexp.MustCompile(`^C[0-9]+$`)
	evidenceLink = regexp.MustCompile(`^\[[^]]+\]\([^)]+\)$`)
)

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
	}

	marketingPath := filepath.Join(root, "docs", "MARKETING_PLAN.md")
	marketing, err := os.ReadFile(marketingPath)
	if err != nil {
		return nil, err
	}
	violations = append(violations, lintClaimsRegister(marketingPath, string(marketing), statuses)...)

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Path == violations[j].Path {
			return violations[i].Line < violations[j].Line
		}
		return violations[i].Path < violations[j].Path
	})
	return violations, nil
}

func claimAssetPaths(root string) ([]string, error) {
	paths := make(map[string]bool)
	for _, name := range []string{"README.md", "CHANGELOG.md", "RELEASE_NOTES.md"} {
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			paths[path] = true
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	for _, directory := range []string{"docs", "marketing"} {
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
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
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
	for offset := 0; offset < len(content); {
		location := claimMarker.FindStringSubmatchIndex(content[offset:])
		if location == nil {
			break
		}
		start := offset + location[0]
		gate := content[offset+location[2] : offset+location[3]]
		status, exists := statuses[gate]
		if !exists {
			violations = append(violations, violation{
				Path:    path,
				Line:    1 + strings.Count(content[:start], "\n"),
				Message: fmt.Sprintf("claim marker names unknown gate %s", gate),
			})
		} else if status != "accepted" {
			violations = append(violations, violation{
				Path:    path,
				Line:    1 + strings.Count(content[:start], "\n"),
				Message: fmt.Sprintf("claim marker for %s is forbidden while gate status is %q", gate, status),
			})
		}
		offset += location[1]
	}
	return violations
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
		if len(columns) != 6 {
			violations = append(violations, violation{Path: path, Line: index + 1, Message: fmt.Sprintf("claim %s must have exactly six columns including Evidence", currentClaimID)})
			continue
		}
		gate := columns[3]
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
		fmt.Fprintln(os.Stderr, "usage: claimslint [repository-root]")
		os.Exit(2)
	}
	violations, err := lintClaims(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "claims lint: %v\n", err)
		os.Exit(2)
	}
	for _, item := range violations {
		fmt.Fprintln(os.Stderr, item.String())
	}
	if len(violations) != 0 {
		os.Exit(1)
	}
}
