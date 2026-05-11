package agent

import (
	"regexp"
	"strings"
)

// structuredCompactPrompt is a Claude Code style prompt that forces the model
// to analyze first, then provide a structured summary.
const structuredCompactPrompt = `Your task is to create a detailed summary of the conversation so far.

CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

Before providing your final summary, wrap your analysis in <analysis> tags to organize your thoughts and ensure you've covered all necessary points. In your analysis process:

1. Chronologically analyze each message and section of the conversation. For each section thoroughly identify:
   - The user's explicit requests and intents
   - Your approach to addressing the user's requests
   - Key decisions, technical concepts and code patterns
   - Specific details like:
     - file names
     - full code snippets
     - function signatures
     - file edits
   - Errors that you ran into and how you fixed them
   - Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
2. Double-check for technical accuracy and completeness, addressing each required element thoroughly.

Your summary should include the following sections:

1. **Primary Request and Intent**: Capture all of the user's explicit requests and intents in detail
2. **Key Technical Concepts**: List all important technical concepts, technologies, and frameworks discussed.
3. **Files and Code Sections**: Enumerate specific files and code sections examined, modified, or created. Pay special attention to the most recent messages and include full code snippets where applicable and include a summary of why this file read or edit is important.
4. **Errors and Fixes**: List all errors that you ran into, and how you fixed them. Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
5. **Problem Solving**: Document problems solved and any ongoing troubleshooting efforts.
6. **All User Messages**: List ALL user messages that are not tool results. These are critical for understanding the users' feedback and changing intent.
7. **Pending Tasks**: Outline any pending tasks that you have explicitly been asked to work on.
8. **Current Work**: Describe in detail precisely what was being worked on immediately before this summary request, paying special attention to the most recent messages from both user and assistant. Include file names and code snippets where applicable.
9. **Optional Next Step**: List the next step that you will take that is related to the most recent work you were doing. IMPORTANT: ensure that this step is DIRECTLY in line with the user's most recent explicit requests, and the task you were working on immediately before this summary request. If your last task was concluded, then only list next steps if they are explicitly in line with the users request. Do not start on tangential requests or really old requests that were already completed without confirming with the user first.
                       If there is a next step, include direct quotes from the most recent conversation showing exactly what task you were working on and where you left off. This should be verbatim to ensure there's no drift in task interpretation.

Output format:

<analysis>
[Your thought process, ensuring all points are covered thoroughly and accurately]
</analysis>

<summary>
1. Primary Request and Intent:
   [Detailed description]

2. Key Technical Concepts:
   - [Concept 1]
   - [Concept 2]

3. Files and Code Sections:
   - [File Name 1]
      - [Summary of why this file is important]
      - [Important Code Snippet]

4. Errors and Fixes:
    - [Detailed description of error 1]:
      - [How you fixed the error]

5. Problem Solving:
   [Description]

6. All User Messages:
    - [Detailed non tool use user message]

7. Pending Tasks:
   - [Task 1]

8. Current Work:
   [Precise description of current work]

9. Optional Next Step:
   [Optional Next step to take]
</summary>

Requirements:
- The summary must be detailed enough for the conversation to continue seamlessly
- Preserve all critical technical information (file paths, commands, configurations, etc.)
- Use the same language as the conversation (Chinese for Chinese conversations, English for English conversations)
- Length should be 800-1500 words`

// continueAfterCompactPrompt is used after preemptive/proactive compact to help LLM continue smoothly.
const continueAfterCompactPrompt = `I have compacted the conversation history to manage context size. The summary above captures:

- **Your original request and intent**: What you wanted to accomplish
- **Key work completed**: Files examined, modified, or created
- **Technical decisions**: Approaches chosen and trade-offs considered
- **Current status**: What was being worked on immediately before this compact
- **Pending tasks**: Any remaining items you've asked me to work on

Please continue from where we left off. Reference the summary as needed for context. If you need specific details from earlier in the conversation, let me know and I can retrieve them.`

var (
	reSummaryTag   = regexp.MustCompile(`(?s)<summary>(.*?)</summary>`)
	reAnalysisTag  = regexp.MustCompile(`(?s)<analysis>(.*?)</analysis>`)
	reSummaryCheck = regexp.MustCompile(`(?s)<summary>.*?</summary>`)
)

// extractSummaryTag extracts the content between <summary>...</summary> tags.
// If no summary tag is found, returns the original content.
func extractSummaryTag(content string) string {
	matches := reSummaryTag.FindStringSubmatch(content)

	if len(matches) > 1 {
		// Return the content inside the tags, trimmed
		return strings.TrimSpace(matches[1])
	}

	// No summary tag found, return original content
	return strings.TrimSpace(content)
}

// extractAnalysisTag extracts the content between <analysis>...</analysis> tags.
// This is useful for debugging or logging the model's thought process.
func extractAnalysisTag(content string) string {
	matches := reAnalysisTag.FindStringSubmatch(content)

	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	return ""
}

// hasSummaryTag checks if the content contains a <summary> tag.
func hasSummaryTag(content string) bool {
	return reSummaryCheck.MatchString(content)
}
