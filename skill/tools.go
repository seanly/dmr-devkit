package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/tool"
)

// --- skillCreate ---

func (m *Manager) skillCreateTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "skillCreate",
			Description: "Create a new skill. The skill will be saved to the learner directory with group forced to 'extended'.",
			Group:       m.toolGroup,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Skill name (lowercase, hyphens)",
					},
					"group": map[string]any{
						"type":        "string",
						"enum":        []string{"extended", "core"},
						"default":     "extended",
						"description": "Skill group (default extended)",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Full SKILL.md content with YAML frontmatter",
					},
					"category": map[string]any{
						"type":        "string",
						"description": "Category subdirectory (e.g. 'devops', 'coding'). Default: ''",
					},
				},
				"required": []string{"name", "content"},
			},
		},
		Handler: m.handleSkillCreate,
	}
}

func (m *Manager) handleSkillCreate(_ *tool.ToolContext, args map[string]any) (any, error) {
	if !m.config.AllowCreate {
		return map[string]any{"success": false, "error": "skill creation is disabled"}, nil
	}
	name, _ := args["name"].(string)
	group, _ := args["group"].(string)
	content, _ := args["content"].(string)
	category, _ := args["category"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return map[string]any{"success": false, "error": "name is required"}, nil
	}
	if group == "" {
		group = "extended"
	}

	if m.config.SecurityScan {
		if err := scanSkillContent(content); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
	}
	if err := validateSkillContent(content, m.config.MaxSkillSize); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}

	content = normalizeSkillGroup(content, group)

	dir := filepath.Join(m.config.AutoCreatePath, safeName(name))
	if category != "" {
		dir = filepath.Join(m.config.AutoCreatePath, safeName(category), safeName(name))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := writeFileAtomic(path, []byte(content), 0o600); err != nil {
		os.RemoveAll(dir)
		return map[string]any{"success": false, "error": err.Error()}, nil
	}

	m.cleanupAutoSkills()
	m.refreshSkills()
	return map[string]any{"success": true, "location": path}, nil
}

// --- skillPromote / skillDemote / skillList / skillEdit / skillDelete ---

func (m *Manager) skillPromoteTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "skillPromote",
			Description: "Promote a skill from extended to core (injects into system prompt).",
			Group:       m.toolGroup,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Skill name"},
				},
				"required": []string{"name"},
			},
		},
		Handler: m.handleSkillPromote,
	}
}

func (m *Manager) skillDemoteTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "skillDemote",
			Description: "Demote a skill from core to extended (removes from system prompt).",
			Group:       m.toolGroup,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Skill name"},
				},
				"required": []string{"name"},
			},
		},
		Handler: m.handleSkillDemote,
	}
}

func (m *Manager) skillListTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "skillList",
			Description: "List all available skills with their group and location.",
			Group:       m.toolGroup,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		Handler: m.handleSkillList,
	}
}

func (m *Manager) skillEditTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "skillEdit",
			Description: "Edit an existing skill. Supports full replacement (content) or patch (old_string + new_string).",
			Group:       m.toolGroup,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":       map[string]any{"type": "string", "description": "Skill name"},
					"content":    map[string]any{"type": "string", "description": "New full SKILL.md content (for full replacement)"},
					"old_string": map[string]any{"type": "string", "description": "Text to find (for patch mode)"},
					"new_string": map[string]any{"type": "string", "description": "Replacement text (for patch mode)"},
				},
				"required": []string{"name"},
			},
		},
		Handler: m.handleSkillEdit,
	}
}

func (m *Manager) skillDeleteTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "skillDelete",
			Description: "Delete an existing skill by removing its directory and SKILL.md.",
			Group:       m.toolGroup,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Skill name"},
				},
				"required": []string{"name"},
			},
		},
		Handler: m.handleSkillDelete,
	}
}

func (m *Manager) handleSkillPromote(_ *tool.ToolContext, args map[string]any) (any, error) {
	return m.setSkillGroup(args, "core")
}

func (m *Manager) handleSkillDemote(_ *tool.ToolContext, args map[string]any) (any, error) {
	return m.setSkillGroup(args, "extended")
}

func (m *Manager) setSkillGroup(args map[string]any, group string) (any, error) {
	name, _ := args["name"].(string)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return map[string]any{"success": false, "error": "name is required"}, nil
	}
	m.ensureSkillsFresh()
	var loc string
	for _, s := range m.skills {
		if strings.ToLower(s.Name) == name {
			loc = s.Location
			break
		}
	}
	if loc == "" {
		return map[string]any{"success": false, "error": "skill not found"}, nil
	}
	data, err := os.ReadFile(loc)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	content := string(data)
	content = normalizeSkillGroup(content, group)
	if err := writeFileAtomic(loc, []byte(content), 0o600); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	m.refreshSkills()
	return map[string]any{"success": true, "group": group}, nil
}

func (m *Manager) handleSkillList(_ *tool.ToolContext, _ map[string]any) (any, error) {
	m.ensureSkillsFresh()
	if len(m.skills) == 0 {
		return "(no skills found)", nil
	}
	var lines []string
	for _, s := range m.skills {
		group := "extended"
		if skillIsCore(s) {
			group = "core"
		}
		skType := s.Type
		if skType == "" {
			skType = "prompt"
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] (%s) %s", s.Name, group, skType, s.Location))
	}
	return strings.Join(lines, "\n"), nil
}

func (m *Manager) handleSkillEdit(_ *tool.ToolContext, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return map[string]any{"success": false, "error": "name is required"}, nil
	}
	m.ensureSkillsFresh()
	var loc string
	for _, s := range m.skills {
		if strings.ToLower(s.Name) == name {
			loc = s.Location
			break
		}
	}
	if loc == "" {
		return map[string]any{"success": false, "error": "skill not found"}, nil
	}

	content, hasContent := args["content"].(string)
	oldStr, hasOld := args["old_string"].(string)
	newStr, hasNew := args["new_string"].(string)

	var finalContent string
	if hasOld && hasNew {
		data, err := os.ReadFile(loc)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
		current := string(data)
		count := strings.Count(current, oldStr)
		if count == 0 {
			return map[string]any{"success": false, "error": "old_string not found in SKILL.md"}, nil
		}
		if count > 1 {
			return map[string]any{"success": false, "error": fmt.Sprintf("old_string matches %d times, must be unique", count)}, nil
		}
		finalContent = strings.Replace(current, oldStr, newStr, 1)
	} else if hasContent {
		finalContent = content
	} else {
		return map[string]any{"success": false, "error": "provide either content or old_string+new_string"}, nil
	}

	if m.config.SecurityScan {
		if err := scanSkillContent(finalContent); err != nil {
			return map[string]any{"success": false, "error": err.Error()}, nil
		}
	}
	if err := validateSkillContent(finalContent, m.config.MaxSkillSize); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}

	if err := writeFileAtomic(loc, []byte(finalContent), 0o600); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	m.refreshSkills()
	return map[string]any{"success": true, "location": loc}, nil
}

func (m *Manager) handleSkillDelete(_ *tool.ToolContext, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return map[string]any{"success": false, "error": "name is required"}, nil
	}
	m.ensureSkillsFresh()
	var loc string
	for _, s := range m.skills {
		if strings.ToLower(s.Name) == name {
			loc = s.Location
			break
		}
	}
	if loc == "" {
		return map[string]any{"success": false, "error": "skill not found"}, nil
	}
	dir := filepath.Dir(loc)
	if err := os.RemoveAll(dir); err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	m.refreshSkills()
	return map[string]any{"success": true, "deleted": dir}, nil
}

// --- delegate ---

func (m *Manager) delegateTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "delegate",
			Description: "Delegate a task to a specialist skill agent. Available specialists: " + strings.Join(m.agentSkillNames(), ", ") + ". The skill name is validated at runtime, so newly created skills are usable immediately.",
			Group:       m.toolGroup,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill": map[string]any{
						"type":        "string",
						"description": "Agent skill name to delegate to",
					},
					"task": map[string]any{
						"type":        "string",
						"description": "Task description for the specialist",
					},
				},
				"required": []string{"skill", "task"},
			},
		},
		Handler:     m.handleDelegate,
		NeedContext: true,
	}
}

func (m *Manager) agentSkillNames() []string {
	m.ensureSkillsFresh()
	var names []string
	for _, s := range m.skills {
		if s.Type == "agent" {
			names = append(names, s.Name)
		}
	}
	return names
}

func (m *Manager) handleDelegate(ctx *tool.ToolContext, args map[string]any) (any, error) {
	skillID := strings.ToLower(strings.TrimSpace(args["skill"].(string)))
	task := strings.TrimSpace(args["task"].(string))

	if skillID == "" {
		return map[string]any{"success": false, "error": "skill is required"}, nil
	}
	if task == "" {
		return map[string]any{"success": false, "error": "task is required"}, nil
	}

	// Enforce subagent delegation allowlist when running inside a subagent context.
	if allowlistRaw, ok := ctx.State["subagent_allowlist"]; ok {
		allowlist, ok := allowlistRaw.([]string)
		if !ok {
			return map[string]any{"success": false, "error": "invalid subagent allowlist state"}, nil
		}
		allowed := false
		for _, allowedSkill := range allowlist {
			if strings.ToLower(strings.TrimSpace(allowedSkill)) == skillID {
				allowed = true
				break
			}
		}
		if !allowed {
			return map[string]any{"success": false, "error": fmt.Sprintf("delegation to %q not allowed by this agent's subagents configuration", skillID)}, nil
		}
	}

	return m.runSkillDelegation(ctx, skillID, task)
}

// runSkillDelegation is the shared implementation for delegate and synthesized delegate_* tools.
func (m *Manager) runSkillDelegation(ctx *tool.ToolContext, skillID, task string) (any, error) {
	m.ensureSkillsFresh()
	var sk *Skill
	for _, s := range m.skills {
		if strings.ToLower(s.Name) == skillID {
			sk = s
			break
		}
	}
	if sk == nil {
		return map[string]any{"success": false, "error": "skill not found"}, nil
	}
	if sk.Type != "agent" {
		return map[string]any{"success": false, "error": fmt.Sprintf("skill %q is not an agent skill", skillID)}, nil
	}

	raw, ok := ctx.State[tool.StateKeyRuntimeAgent]
	if !ok || raw == nil {
		return map[string]any{"success": false, "error": "no runtime agent available"}, nil
	}
	ag, ok := raw.(agent.RuntimeAgent)
	if !ok || ag == nil {
		return map[string]any{"success": false, "error": "invalid runtime agent"}, nil
	}

	// Build a rich context string that preserves the parent task context and
	// clearly marks the skill instructions. This is injected as a system message
	// on the child tape so the sub-agent knows its role and constraints.
	contextJSON := buildSkillDelegationContext(ctx, sk, task)

	maxSteps := sk.MaxIterations
	if maxSteps == 0 {
		maxSteps = 8
	}

	subCtx := context.Background()
	if ctx != nil && ctx.Ctx != nil {
		subCtx = ctx.Ctx
	}

	modelName := sk.Model
	if modelName == "inherit" {
		modelName = ""
	}

	subResult, err := ag.RunSubagentWithTools(subCtx, ctx.Tape, task, modelName, "temp", contextJSON, maxSteps, sk.ToolAllowlist, sk.Subagents)
	output := ""
	if subResult != nil {
		output = subResult.Text
	}

	if sk.MaxResultChars > 0 {
		runes := []rune(output)
		if len(runes) > sk.MaxResultChars {
			output = string(runes[:sk.MaxResultChars]) + "\n[...truncated]"
		}
	}

	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	out := map[string]any{"success": true, "output": output}
	if subResult != nil && subResult.Packet != nil {
		out["packet"] = subResult.Packet
	}
	return out, nil
}

// buildSkillDelegationContext produces the context payload passed to the sub-agent.
// It includes the parent task context and the skill instructions so the specialist
// has enough background to produce useful output without losing the parent agent's
// intent.
func buildSkillDelegationContext(ctx *tool.ToolContext, sk *Skill, task string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are the **%s** specialist sub-agent, invoked via `delegate(skill=%q, task=...)`.\n\n", sk.Name, sk.Name)

	fmt.Fprintf(&b, "**Parent task:** %s\n", task)
	if ctx != nil {
		if ctx.Tape != "" {
			fmt.Fprintf(&b, "**Parent tape:** %s\n", ctx.Tape)
		}
		if ws := ctx.GetCwd(); ws != "" {
			fmt.Fprintf(&b, "**Workspace:** %s\n", ws)
		}
	}
	if sk.Description != "" {
		fmt.Fprintf(&b, "**Your role:** %s\n", sk.Description)
	}
	if sk.WhenToUse != "" {
		fmt.Fprintf(&b, "**When to use you:** %s\n", sk.WhenToUse)
	}
	if len(sk.ToolAllowlist) > 0 {
		fmt.Fprintf(&b, "**Allowed tools:** %s\n", strings.Join(sk.ToolAllowlist, ", "))
	}

	b.WriteString("\n## Skill Instructions\n")
	b.WriteString("Follow these instructions precisely. Do not deviate unless the parent task explicitly requires it.\n\n")
	b.WriteString(sk.Content)
	b.WriteString("\n\n## Response Guidelines\n")
	b.WriteString("- Focus only on the delegated task.\n")
	b.WriteString("- Return concise, actionable output that the parent agent can use.\n")
	b.WriteString("- If you cannot complete the task, explain what blocked you and what the parent agent should do next.\n")

	return b.String()
}
