package skill

import (
	"context"
	"encoding/json"
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
		lines = append(lines, fmt.Sprintf("- %s [%s] %s", s.Name, group, s.Location))
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

// --- skillWorkflow ---

func (m *Manager) skillWorkflowTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "skillWorkflow",
			Description: "Execute a workflow skill by name. Runs the workflow's steps as subagents in dependency order.",
			Group:       m.toolGroup,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Workflow skill name",
					},
					"vars": map[string]any{
						"type":        "object",
						"description": "Variables to inject into workflow step prompts (e.g. image, tag, env)",
					},
				},
				"required": []string{"name"},
			},
		},
		Handler:     m.handleSkillWorkflow,
		NeedContext: true,
	}
}

func (m *Manager) handleSkillWorkflow(ctx *tool.ToolContext, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return map[string]any{"success": false, "error": "name is required"}, nil
	}

	m.ensureSkillsFresh()
	var loc string
	var sk *Skill
	for _, s := range m.skills {
		if strings.ToLower(s.Name) == name {
			loc = s.Location
			sk = s
			break
		}
	}
	if loc == "" {
		return map[string]any{"success": false, "error": "workflow skill not found"}, nil
	}

	// Re-read from disk to get latest content and Type.
	sk, err := parseSkillFile(loc)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	if sk.Type != "workflow" {
		return map[string]any{"success": false, "error": fmt.Sprintf("skill %q is not a workflow (type=%s)", name, sk.Type)}, nil
	}

	wf, err := ParseWorkflow(sk.Content)
	if err != nil {
		return map[string]any{"success": false, "error": fmt.Sprintf("failed to parse workflow: %v", err)}, nil
	}

	steps, err := wf.TopologicalOrder()
	if err != nil {
		return map[string]any{"success": false, "error": fmt.Sprintf("workflow error: %v", err)}, nil
	}

	raw, ok := ctx.State[tool.StateKeyRuntimeAgent]
	if !ok || raw == nil {
		return map[string]any{"success": false, "error": "no runtime agent available"}, nil
	}
	ag, ok := raw.(agent.RuntimeAgent)
	if !ok || ag == nil {
		return map[string]any{"success": false, "error": "invalid runtime agent"}, nil
	}

	vars := make(map[string]string)
	if rawVars, ok := args["vars"].(map[string]any); ok {
		for k, v := range rawVars {
			vars[k] = fmt.Sprintf("%v", v)
		}
	}

	stepResults := make(map[string]string)
	for _, step := range steps {
		prompt, err := renderPrompt(step.Prompt, vars, stepResults)
		if err != nil {
			return map[string]any{
				"success": false,
				"error":   fmt.Sprintf("step %q render failed: %v", step.Name, err),
				"results": stepResults,
			}, nil
		}

		var contextJSON string
		if len(stepResults) > 0 {
			b, _ := json.Marshal(map[string]any{
				"previous_steps": stepResults,
			})
			contextJSON = string(b)
		}

		subCtx := context.Background()
		if ctx != nil && ctx.Ctx != nil {
			subCtx = ctx.Ctx
		}
		output, err := ag.RunSubagent(subCtx, ctx.Tape, prompt, step.Model, "temp", contextJSON, 0)
		if err != nil {
			return map[string]any{
				"success": false,
				"error":   fmt.Sprintf("step %q failed: %v", step.Name, err),
				"results": stepResults,
			}, nil
		}
		stepResults[step.Name] = output
	}

	out := map[string]any{
		"success": true,
		"results": stepResults,
	}
	if len(sk.Secrets) > 0 {
		secretsOut := make([]map[string]string, 0, len(sk.Secrets))
		for _, sec := range sk.Secrets {
			secretsOut = append(secretsOut, map[string]string{
				"name":        sec.Name,
				"description": sec.Description,
				"where":       sec.Where,
			})
		}
		out["secrets"] = secretsOut
	}
	return out, nil
}
