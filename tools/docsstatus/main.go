package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	forbiddenDocumentationPhrase = regexp.MustCompile(`(?i)\b(?:TODO|coming\s+soon)\b`)
	statusLabel                  = regexp.MustCompile(`(?i)^(?:[[:space:]]*-[[:space:]]*)?(?:\*\*)?(?:Document status|Status(?: of every deliverable in this document)?|Review status):(?:\*\*)?[[:space:]]*(.*)$`)
	lifecycleWord                = regexp.MustCompile(`(?i)\b(?:accepted|in progress|planned|deferred)\b`)
)

var lifecycleValues = []string{"in progress", "accepted", "planned", "deferred"}

type violation struct {
	Path    string
	Line    int
	Message string
}

func (v violation) String() string {
	return fmt.Sprintf("%s:%d: %s", v.Path, v.Line, v.Message)
}

func lintDocs(root string) ([]violation, error) {
	var violations []violation
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		fileViolations, err := lintDocument(path, strings.EqualFold(entry.Name(), "OPEN_QUESTIONS.md"))
		if err != nil {
			return err
		}
		violations = append(violations, fileViolations...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Path == violations[j].Path {
			return violations[i].Line < violations[j].Line
		}
		return violations[i].Path < violations[j].Path
	})
	return violations, nil
}

func lintDocument(path string, allowDeferredLanguage bool) ([]violation, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var violations []violation
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	inFence := false
	sawStatusLabel := false
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		}
		if lineNumber <= 16 && !inFence {
			if match := statusLabel.FindStringSubmatch(line); match != nil {
				sawStatusLabel = true
				if reason := invalidLifecycleStatus(match[1]); reason != "" {
					violations = append(violations, violation{
						Path: path, Line: lineNumber,
						Message: "invalid lifecycle status declaration: " + reason,
					})
				}
			}
		}
		if allowDeferredLanguage {
			continue
		}
		if match := forbiddenDocumentationPhrase.FindString(line); match != "" {
			violations = append(violations, violation{
				Path:    path,
				Line:    lineNumber,
				Message: fmt.Sprintf("forbidden phrase %q outside OPEN_QUESTIONS.md", match),
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !sawStatusLabel {
		violations = append(violations, violation{
			Path:    path,
			Line:    1,
			Message: "document has no lifecycle status declaration in its header",
		})
	}
	return violations, nil
}

func invalidLifecycleStatus(raw string) string {
	value := strings.TrimSpace(raw)
	remainder := ""
	selected := ""
	if strings.HasPrefix(value, "`") {
		closing := strings.Index(value[1:], "`")
		if closing < 0 {
			return "unterminated inline-code value"
		}
		selected = value[1 : closing+1]
		remainder = value[closing+2:]
	} else {
		lower := strings.ToLower(value)
		for _, candidate := range lifecycleValues {
			if !strings.HasPrefix(lower, candidate) {
				continue
			}
			if len(value) != len(candidate) && !isStatusBoundary(value[len(candidate)]) {
				continue
			}
			selected = value[:len(candidate)]
			remainder = value[len(candidate):]
			break
		}
	}

	if !isLifecycleValue(selected) {
		return fmt.Sprintf("value must begin with exactly one of accepted, in progress, planned, or deferred; got %q", raw)
	}
	if lifecycleWord.MatchString(remainder) {
		return fmt.Sprintf("declaration contains more than one lifecycle value: %q", raw)
	}
	return ""
}

func isLifecycleValue(value string) bool {
	for _, candidate := range lifecycleValues {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}

func isStatusBoundary(value byte) bool {
	switch value {
	case ' ', '\t', '.', ',', ';', ':', ')', ']':
		return true
	default:
		return false
	}
}

func main() {
	root := "docs"
	if len(os.Args) == 2 {
		root = os.Args[1]
	} else if len(os.Args) > 2 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: docsstatus [docs-directory]")
		os.Exit(2)
	}
	violations, err := lintDocs(root)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "docs-status lint: %v\n", err)
		os.Exit(2)
	}
	for _, item := range violations {
		_, _ = fmt.Fprintln(os.Stderr, item.String())
	}
	if len(violations) != 0 {
		os.Exit(1)
	}
}
