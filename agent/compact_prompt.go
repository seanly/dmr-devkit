package agent

import (
	"regexp"
	"strings"
)

// structuredCompactPrompt asks the model to produce a structured summary
// wrapped in <summary> tags. We intentionally do NOT ask for an <analysis>
// section so the model cannot leak its thought process into the stored summary.
const structuredCompactPrompt = `Your task is to create a detailed, self-contained summary of the conversation so far. The summary will become the "shared understanding" used to continue the conversation after a context handoff.

CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

INHERITANCE: If the conversation below contains "[Previous Context Summary]", preserve its still-relevant content in the new summary. Update statuses: mark completed items done, drop stale constraints, add newly introduced pending tasks. Do NOT simply copy the old summary; merge it with the new conversation. Keep the original goal unless the user explicitly changed it.

PRESERVATION PRIORITY (most to least important):
1. User's explicit goals and high-level intent
2. Active constraints, preferences, and user feedback (especially corrections)
3. Pending tasks and open questions
4. Files, code sections, and artifacts currently being worked on
5. Errors encountered and how they were fixed
6. Technical decisions and reasoning
7. Older details that are no longer relevant may be dropped

Your summary should include the following sections:

1. **Primary Request and Intent**: What the user wants to achieve. Be specific and capture any explicit scope or success criteria.
2. **Key Technical Concepts**: Important technologies, frameworks, or domain concepts discussed.
3. **Files and Code Sections**: Specific files read, modified, or created. Include short code snippets only for the most important or recent changes, and note why each file matters.
4. **Errors and Fixes**: Errors encountered and how they were resolved. Include exact error messages or user feedback when relevant.
5. **Problem Solving**: Key decisions made and any ongoing troubleshooting.
6. **Key User Messages and Feedback**: Capture user messages that changed intent, corrected you, or added constraints. Do NOT list every greeting or acknowledgment; focus on messages that affect future actions.
7. **Pending Tasks**: Tasks explicitly requested but not yet completed.
8. **Current Work**: What was being worked on immediately before this summary request. Include file names and the last action taken.
9. **Optional Next Step**: A concise next step ONLY if the user's most recent messages clearly imply one. If the task is finished or unclear, write "None" or omit this section. Do not invent work.

Output format:

<summary>
1. Primary Request and Intent:
   [Detailed description]

2. Key Technical Concepts:
   - [Concept 1]
   - [Concept 2]

3. Files and Code Sections:
   - [File Name 1]
      - [Why it matters]
      - [Important snippet if any]

4. Errors and Fixes:
    - [Error]: [How it was fixed]

5. Problem Solving:
   [Description]

6. Key User Messages and Feedback:
    - [Message or feedback summary]

7. Pending Tasks:
   - [Task 1]

8. Current Work:
   [Precise description]

9. Optional Next Step:
   [Next step or "None"]
</summary>

Requirements:
- The summary must be detailed enough for the conversation to continue seamlessly.
- Preserve critical technical information (file paths, commands, configurations, exact error messages).
- Use the same language as the conversation (Chinese for Chinese, English for English).
- Length should be roughly 600-1200 words. Be concise but complete.
- Do NOT output <analysis> tags, markdown code fences, or meta-commentary about how you produced the summary.`

// continueAfterCompactPrompt is used after preemptive/proactive compact to help LLM continue smoothly.
const continueAfterCompactPrompt = `I have compacted the conversation history to manage context size. The summary above and the [TaskState v1] block capture:

- **Your original request and intent**
- **Key work completed**: files examined, modified, or created
- **Active constraints and preferences**
- **Pending tasks** that still need attention
- **Current status**: what was being worked on immediately before this compact

Please continue from where we left off. Use the summary and task state as your working memory. If you need specific details from earlier in the conversation, use tapeSearch or ask the user rather than guessing.`

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
