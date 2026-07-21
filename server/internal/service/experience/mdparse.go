package experience

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillFrontmatter holds the metadata parsed from a SKILL.md YAML frontmatter block.
type SkillFrontmatter struct {
	Name        string // name field value → maps to exp.Title
	Description string // description field value → maps to exp.Description
}

// ExtractSkillFrontmatter extracts the YAML frontmatter (name, description) from
// a SKILL.md file.
//
// The expected format is:
//
//	---
//	name: skill-name
//	description: skill description
//	---
//	body content...
func ExtractSkillFrontmatter(rawContent string) (*SkillFrontmatter, error) {
	var fm SkillFrontmatter

	// Normalize line endings.
	rawContent = strings.ReplaceAll(rawContent, "\r\n", "\n")
	lines := strings.Split(rawContent, "\n")

	// Locate frontmatter boundaries.
	fmStart := -1
	fmEnd := -1
	for i, line := range lines {
		if line == "---" {
			if fmStart == -1 {
				fmStart = i
			} else if fmEnd == -1 {
				fmEnd = i
				break
			}
		}
	}

	if fmStart == 0 && fmEnd > fmStart {
		yamlBlock := strings.Join(lines[fmStart+1:fmEnd], "\n")
		if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
			return nil, fmt.Errorf("parse YAML frontmatter: %w", err)
		}
		return &fm, nil
	}

	return nil, errors.New("skill frontmatter not found")
}

// FormatRawWithLineNumbers returns the entire raw SKILL.md content formatted
// with 1-based line number prefixes. Includes the YAML frontmatter so the LLM
// can see the original name and description fields.
func FormatRawWithLineNumbers(rawContent string) string {
	normalized := strings.ReplaceAll(rawContent, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	var sb strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&sb, "%d| %s\n", i+1, line)
	}
	return sb.String()
}
