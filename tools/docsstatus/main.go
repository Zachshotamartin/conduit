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
	statusDeclaration            = regexp.MustCompile(`(?i)\bstatus\b[^\n]*(?:accepted|in progress|planned|deferred|normative)`)
)

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
		if strings.EqualFold(entry.Name(), "TEMPLATE.md") {
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
	defer file.Close()

	var violations []violation
	var header strings.Builder
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	inFence := false
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if lineNumber <= 16 {
			header.WriteString(line)
			header.WriteByte('\n')
		}
		if inFence || allowDeferredLanguage {
			continue
		}
		visible := removeQuotedMetaLanguage(line)
		if match := forbiddenDocumentationPhrase.FindString(visible); match != "" {
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
	if !statusDeclaration.MatchString(header.String()) {
		violations = append(violations, violation{
			Path:    path,
			Line:    1,
			Message: "document has no lifecycle status declaration in its header",
		})
	}
	return violations, nil
}

func removeQuotedMetaLanguage(line string) string {
	var out strings.Builder
	var delimiter rune
	escaped := false
	for _, current := range line {
		if escaped {
			escaped = false
			if delimiter == 0 {
				out.WriteRune(current)
			}
			continue
		}
		if current == '\\' {
			escaped = true
			continue
		}
		if delimiter != 0 {
			if current == delimiter {
				delimiter = 0
			}
			continue
		}
		if current == '`' || current == '"' {
			delimiter = current
			continue
		}
		out.WriteRune(current)
	}
	return out.String()
}

func main() {
	root := "docs"
	if len(os.Args) == 2 {
		root = os.Args[1]
	} else if len(os.Args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: docsstatus [docs-directory]")
		os.Exit(2)
	}
	violations, err := lintDocs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "docs-status lint: %v\n", err)
		os.Exit(2)
	}
	for _, item := range violations {
		fmt.Fprintln(os.Stderr, item.String())
	}
	if len(violations) != 0 {
		os.Exit(1)
	}
}
