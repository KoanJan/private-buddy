package experience

import (
	"strings"
	"testing"
)

// TestExtractSkillFrontmatter_Normal verifies that a standard SKILL.md with
// name and description fields is correctly parsed.
func TestExtractSkillFrontmatter_Normal(t *testing.T) {
	raw := `---
name: search-tool
description: Search posts using the API.
---
## When to use this skill
Use when the user wants to find tweets.
`

	fm, err := ExtractSkillFrontmatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	if fm.Name != "search-tool" {
		t.Errorf("expected Name='search-tool', got '%s'", fm.Name)
	}
	if fm.Description != "Search posts using the API." {
		t.Errorf("expected Description='Search posts using the API.', got '%s'", fm.Description)
	}
}

// TestExtractSkillFrontmatter_ExtraFields verifies that unrecognized YAML
// fields in the frontmatter block are silently ignored.
func TestExtractSkillFrontmatter_ExtraFields(t *testing.T) {
	raw := `---
name: my-skill
description: A useful skill.
version: "1.0"
author: unknown
---
body line 1
body line 2
`

	fm, err := ExtractSkillFrontmatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}

	if fm.Name != "my-skill" {
		t.Errorf("expected Name='my-skill', got '%s'", fm.Name)
	}
	if fm.Description != "A useful skill." {
		t.Errorf("expected Description='A useful skill.', got '%s'", fm.Description)
	}
}

// TestExtractSkillFrontmatter_MissingName verifies that a frontmatter block
// without a name field returns an empty Name.
func TestExtractSkillFrontmatter_MissingName(t *testing.T) {
	raw := `---
description: Only description here.
---
body content
`

	fm, err := ExtractSkillFrontmatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	if fm.Name != "" {
		t.Errorf("expected empty Name, got '%s'", fm.Name)
	}
	if fm.Description != "Only description here." {
		t.Errorf("expected Description='Only description here.', got '%s'", fm.Description)
	}
}

// TestExtractSkillFrontmatter_MissingDescription verifies that a frontmatter
// block without a description field returns an empty Description.
func TestExtractSkillFrontmatter_MissingDescription(t *testing.T) {
	raw := `---
name: name-only-skill
---
body content
`

	fm, err := ExtractSkillFrontmatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	if fm.Name != "name-only-skill" {
		t.Errorf("expected Name='name-only-skill', got '%s'", fm.Name)
	}
	if fm.Description != "" {
		t.Errorf("expected empty Description, got '%s'", fm.Description)
	}
}

// TestExtractSkillFrontmatter_NoFrontmatter verifies that a markdown file
// without any YAML frontmatter returns an error.
func TestExtractSkillFrontmatter_NoFrontmatter(t *testing.T) {
	raw := `# Just a markdown file
No frontmatter here.

## Section
Some content.
`

	fm, err := ExtractSkillFrontmatter(raw)
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
	if fm != nil {
		t.Error("expected nil frontmatter when no frontmatter present")
	}
}

// TestExtractSkillFrontmatter_FrontmatterNotAtStart verifies that ---
// delimiters not appearing at line 1 are NOT treated as frontmatter.
func TestExtractSkillFrontmatter_FrontmatterNotAtStart(t *testing.T) {
	raw := `# Title

---
name: not-frontmatter
description: This is NOT frontmatter.
---
body
`

	fm, err := ExtractSkillFrontmatter(raw)
	if err == nil {
		t.Fatal("expected error when frontmatter not at line 1")
	}
	if fm != nil {
		t.Error("expected nil frontmatter when frontmatter not at line 1")
	}
}

// TestExtractSkillFrontmatter_CRLF verifies that Windows-style CRLF line
// endings are normalized correctly during frontmatter extraction.
func TestExtractSkillFrontmatter_CRLF(t *testing.T) {
	raw := "---\r\nname: crlf-skill\r\ndescription: Windows style line endings.\r\n---\r\nbody line\r\n"

	fm, err := ExtractSkillFrontmatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	if fm.Name != "crlf-skill" {
		t.Errorf("expected Name='crlf-skill', got '%s'", fm.Name)
	}
	if fm.Description != "Windows style line endings." {
		t.Errorf("expected Description='Windows style line endings.', got '%s'", fm.Description)
	}
}

// TestExtractSkillFrontmatter_Empty verifies that an empty string input
// returns an error.
func TestExtractSkillFrontmatter_Empty(t *testing.T) {
	raw := ""

	fm, err := ExtractSkillFrontmatter(raw)
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if fm != nil {
		t.Error("expected nil frontmatter for empty input")
	}
}

// TestExtractSkillFrontmatter_OnlyFrontmatter verifies that a SKILL.md
// containing only frontmatter (no body) is correctly parsed.
func TestExtractSkillFrontmatter_OnlyFrontmatter(t *testing.T) {
	raw := `---
name: header-only
description: No body.
---
`

	fm, err := ExtractSkillFrontmatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	if fm.Name != "header-only" {
		t.Errorf("expected Name='header-only', got '%s'", fm.Name)
	}
	if fm.Description != "No body." {
		t.Errorf("expected Description='No body.', got '%s'", fm.Description)
	}
}

// TestExtractSkillFrontmatter_MultilineDescription verifies that YAML block
// scalar (|) multiline descriptions are correctly parsed.
func TestExtractSkillFrontmatter_MultilineDescription(t *testing.T) {
	raw := `---
name: multiline
description: |
  This is a multiline
  description field.
---
body
`

	fm, err := ExtractSkillFrontmatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	if fm.Name != "multiline" {
		t.Errorf("expected Name='multiline', got '%s'", fm.Name)
	}
	desc := fm.Description
	if !strings.HasPrefix(desc, "This is a multiline") {
		t.Errorf("expected Description to start with 'This is a multiline', got %q", desc)
	}
	if !strings.Contains(desc, "description field") {
		t.Errorf("expected Description to contain 'description field', got %q", desc)
	}
}

// TestExtractSkillFrontmatter_YAMLError verifies that invalid YAML in the
// frontmatter block returns an error.
func TestExtractSkillFrontmatter_YAMLError(t *testing.T) {
	raw := `---
name: valid
description: valid
invalid: [
---
body
`

	fm, err := ExtractSkillFrontmatter(raw)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if fm != nil {
		t.Errorf("expected nil frontmatter for invalid YAML, got %+v", fm)
	}
}

// TestFormatRawWithLineNumbers verifies that raw SKILL.md content is correctly
// formatted with 1-based line number prefixes.
func TestFormatRawWithLineNumbers(t *testing.T) {
	raw := "---\nname: format-test\ndescription: Testing format output.\n---\nline A\nline B\nline C\n"

	formatted := FormatRawWithLineNumbers(raw)
	formatted = strings.TrimRight(formatted, "\n")
	lines := strings.Split(formatted, "\n")

	if len(lines) != 8 {
		t.Fatalf("expected 8 formatted lines, got %d: %q", len(lines), lines)
	}

	expectedLines := []string{
		"1| ---",
		"2| name: format-test",
		"3| description: Testing format output.",
		"4| ---",
		"5| line A",
		"6| line B",
		"7| line C",
		"8| ",
	}
	for i, expected := range expectedLines {
		if lines[i] != expected {
			t.Errorf("line %d: expected '%s', got '%s'", i, expected, lines[i])
		}
	}
}

// TestFormatRawWithLineNumbers_Empty verifies that an empty string is correctly
// formatted as a single empty line with a line number prefix.
func TestFormatRawWithLineNumbers_Empty(t *testing.T) {
	raw := ""

	formatted := FormatRawWithLineNumbers(raw)
	if formatted != "1| \n" {
		t.Errorf("expected '1| \\n', got '%s'", formatted)
	}
}
