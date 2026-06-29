package agent

import (
	"strings"
	"testing"
)

func TestExtractSummaryTag(t *testing.T) {
	// Test case 1: Normal summary tag
	content := `<analysis>
Some analysis here
</analysis>

<summary>
1. Primary Request: Test request
2. Key Concepts: Testing
</summary>`

	result := extractSummaryTag(content)
	if !strings.Contains(result, "Primary Request") {
		t.Errorf("Expected summary to contain 'Primary Request', got:\n%s", result)
	}
	if strings.Contains(result, "<summary>") {
		t.Error("Summary should not contain <summary> tag")
	}

	// Test case 2: No summary tag
	content = "Just plain text without tags"
	result = extractSummaryTag(content)
	if result != content {
		t.Errorf("Expected original content when no summary tag, got:\n%s", result)
	}

	// Test case 3: Empty summary
	content = `<summary>
</summary>`
	result = extractSummaryTag(content)
	if result != "" {
		t.Errorf("Expected empty string for empty summary, got:\n%s", result)
	}

	// Test case 4: Multiline content
	content = `<summary>
Line 1
Line 2
Line 3
</summary>`
	result = extractSummaryTag(content)
	if !strings.Contains(result, "Line 1") || !strings.Contains(result, "Line 2") {
		t.Errorf("Expected multiline summary, got:\n%s", result)
	}

	// Test case 5: Summary with attributes (should not match)
	content = `<summary class="test">
Content
</summary>`
	result = extractSummaryTag(content)
	// This should extract the content inside even with attributes
	// because regex is (?s)<summary>(.*?)</summary>
	// But actually our regex requires exact <summary> without attributes
	// Let's check behavior
	if result == content {
		// If summary has attributes, it won't match, return original
		t.Logf("Summary with attributes not extracted (expected behavior), got:\n%s", result)
	}
}

func TestExtractAnalysisTag(t *testing.T) {
	// Test case 1: Normal analysis tag
	content := `<analysis>
This is the analysis section
</analysis>

<summary>
This is the summary
</summary>`

	result := extractAnalysisTag(content)
	if !strings.Contains(result, "analysis section") {
		t.Errorf("Expected analysis to contain 'analysis section', got:\n%s", result)
	}

	// Test case 2: No analysis tag
	content = "Just plain text"
	result = extractAnalysisTag(content)
	if result != "" {
		t.Errorf("Expected empty string when no analysis tag, got:\n%s", result)
	}

	// Test case 3: Empty analysis
	content = `<analysis>
</analysis>`
	result = extractAnalysisTag(content)
	if result != "" {
		t.Errorf("Expected empty string for empty analysis, got:\n%s", result)
	}
}

func TestHasSummaryTag(t *testing.T) {
	tests := []struct {
		content  string
		expected bool
	}{
		{"<summary>Test</summary>", true},
		{"<summary>\nMultiline\nContent\n</summary>", true},
		{"No tags here", false},
		{"<analysis>Only analysis</analysis>", false},
		{"", false},
		{"<summary>", false}, // Missing closing tag
		{"</summary>", false},
	}

	for _, test := range tests {
		result := hasSummaryTag(test.content)
		if result != test.expected {
			t.Errorf("hasSummaryTag(%q) = %v, expected %v", test.content, result, test.expected)
		}
	}
}

func TestStructuredCompactPrompt(t *testing.T) {
	// Verify the prompt contains key elements
	prompt := structuredCompactPrompt

	requiredElements := []string{
		"<summary>",
		"</summary>",
		"CRITICAL: Respond with TEXT ONLY",
		"Do NOT call any tools",
		"Do NOT output <analysis> tags",
		"INHERITANCE",
		"PRESERVATION PRIORITY",
		"Primary Request and Intent",
		"Key Technical Concepts",
		"Files and Code Sections",
		"Errors and Fixes",
		"Problem Solving",
		"Key User Messages and Feedback",
		"Pending Tasks",
		"Current Work",
		"Optional Next Step",
	}

	forbiddenPatterns := []string{
		"wrap your analysis in <analysis>",
		"<analysis>\n[Your thought process",
	}

	for _, element := range requiredElements {
		if !strings.Contains(prompt, element) {
			t.Errorf("Prompt missing required element: %s", element)
		}
	}

	for _, pattern := range forbiddenPatterns {
		if strings.Contains(prompt, pattern) {
			t.Errorf("Prompt should not ask model to output an analysis section: %s", pattern)
		}
	}
}

func TestExtractSummaryTag_Trimming(t *testing.T) {
	// Test that whitespace is properly trimmed
	content := `<summary>
   
   Content with surrounding whitespace
   
</summary>`

	result := extractSummaryTag(content)
	if strings.HasPrefix(result, "\n") || strings.HasPrefix(result, " ") {
		t.Error("Summary should be trimmed of leading whitespace")
	}
	if strings.HasSuffix(result, "\n") || strings.HasSuffix(result, " ") {
		t.Error("Summary should be trimmed of trailing whitespace")
	}
}

func TestExtractSummaryTag_MultipleSummaries(t *testing.T) {
	// If there are multiple summary tags, should extract the first one
	content := `<summary>
First summary
</summary>

<summary>
Second summary
</summary>`

	result := extractSummaryTag(content)
	if !strings.Contains(result, "First summary") {
		t.Errorf("Expected first summary, got:\n%s", result)
	}
	if strings.Contains(result, "Second summary") {
		t.Error("Should only extract first summary tag")
	}
}
