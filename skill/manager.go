package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/tool"
)

// Manager provides skill discovery, loading, and agent hook integration.
type Manager struct {
	config Config

	skills         []*Skill
	resolvedRoots  []string
	lastScanMtime  time.Time
	ensureSkillsMu sync.Mutex

	// toolGroup determines whether skill tools are core or extended.
	toolGroup tool.ToolGroup
}

// NewManager creates a new skill manager from config.
func NewManager(cfg Config) *Manager {
	roots := append([]string{}, cfg.Paths...)
	if cfg.AutoCreatePath != "" {
		_ = os.MkdirAll(cfg.AutoCreatePath, 0o755)
		roots = append(roots, cfg.AutoCreatePath)
	}

	return &Manager{
		config:        cfg,
		resolvedRoots: dedupeRoots(roots),
		skills:        discoverSkillsFromRoots(dedupeRoots(roots)),
		lastScanMtime: maxFileMtimeUnderRoots(dedupeRoots(roots)),
		toolGroup:     tool.ToolGroupCore,
	}
}

// NewManagerWithToolGroup creates a manager with a specific tool group.
func NewManagerWithToolGroup(cfg Config, tg tool.ToolGroup) *Manager {
	m := NewManager(cfg)
	m.toolGroup = tg
	return m
}

// SetToolGroup changes the tool group after construction.
func (m *Manager) SetToolGroup(tg tool.ToolGroup) {
	m.toolGroup = tg
}

// ToolGroup returns the current tool group.
func (m *Manager) ToolGroup() tool.ToolGroup {
	return m.toolGroup
}

// Skills returns the currently discovered skills.
func (m *Manager) Skills() []*Skill {
	m.ensureSkillsFresh()
	return append([]*Skill{}, m.skills...)
}

// RegisterBuiltin registers an in-code skill (e.g. from embed.FS).
// Built-ins are prepended so they appear first and can be overridden by disk skills.
func (m *Manager) RegisterBuiltin(sk *Skill) {
	m.ensureSkillsMu.Lock()
	defer m.ensureSkillsMu.Unlock()

	// If a disk skill with the same name exists, the built-in is shadowed.
	m.skills = append([]*Skill{sk}, m.skills...)
}

// --- agent.Hooks implementation ---

var _ agent.Hooks = (*Manager)(nil)

// ComposeSystemPrompt injects the skill search hint into the system prompt.
func (m *Manager) ComposeSystemPrompt(_ context.Context, base string) string {
	fragment, _ := m.buildSystemPrompt()
	if fragment == "" {
		return base
	}
	if base == "" {
		return fragment
	}
	return base + "\n" + fragment
}

// CollectAllTools returns skill tools.
func (m *Manager) CollectAllTools(_ context.Context, includeCore, includeExtended bool) []*tool.Tool {
	var out []*tool.Tool
	if includeCore && m.toolGroup == tool.ToolGroupCore {
		out = append(out, m.allTools()...)
	}
	if includeExtended && m.toolGroup == tool.ToolGroupExtended {
		out = append(out, m.allTools()...)
	}
	return out
}

// AfterAgentRun implements agent.Hooks.
func (m *Manager) AfterAgentRun(context.Context, agent.AfterAgentRunArgs) error { return nil }

// InterceptInput implements agent.Hooks.
func (m *Manager) InterceptInput(context.Context, agent.InterceptInputArgs) (*agent.InterceptResult, error) {
	return nil, nil
}

// OnDiscoveredToolsCleared implements agent.Hooks.
func (m *Manager) OnDiscoveredToolsCleared(context.Context, string) error { return nil }

// OnContextReset implements agent.Hooks.
func (m *Manager) OnContextReset(context.Context, string, string) error { return nil }

// BeforeToolCall implements agent.Hooks.
func (m *Manager) BeforeToolCall(context.Context, *tool.Tool, map[string]any, *tool.ToolContext) error {
	return nil
}

// BatchBeforeToolCall implements agent.Hooks.
func (m *Manager) BatchBeforeToolCall(context.Context, []tool.BatchCheckItem) map[int]error {
	return nil
}

// SanitizeToolResult implements agent.Hooks.
func (m *Manager) SanitizeToolResult(_ context.Context, _ string, result any, _ *tool.ToolContext) (any, error) {
	return result, nil
}

// SanitizeToolLog implements agent.Hooks.
func (m *Manager) SanitizeToolLog(_ context.Context, _ string, result any, _ *tool.ToolContext) (any, error) {
	return result, nil
}

// SanitizeToolAudit implements agent.Hooks.
func (m *Manager) SanitizeToolAudit(_ context.Context, _ string, result any, _ *tool.ToolContext) (any, error) {
	return result, nil
}

// AfterToolRound implements agent.Hooks.
func (m *Manager) AfterToolRound(context.Context, agent.AfterToolRoundArgs) error { return nil }

// --- tools ---

func (m *Manager) allTools() []*tool.Tool {
	tools := []*tool.Tool{
		m.skillSearchTool(),
		m.skillTool(),
		m.skillCreateTool(),
		m.skillPromoteTool(),
		m.skillDemoteTool(),
		m.skillListTool(),
		m.skillEditTool(),
		m.skillDeleteTool(),
		m.delegateTool(),
	}
	return tools
}

func (m *Manager) skillSearchTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "skillSearch",
			Description: "Search available skills by keywords or task description. Returns matching skill names and descriptions. Call skill(name=\"...\") to load the full content of a skill.",
			Group:       tool.ToolGroupCore,
			SearchHint:  "skill, capability, skill.md, find skill, search skill, 技能, 搜索, 查找",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Keywords describing the task or capability you need, e.g. 'build docker', 'write tests', 'cron job'"},
				},
				"required": []string{"query"},
			},
		},
		Handler: m.skillSearchHandler,
	}
}

func (m *Manager) skillSearchHandler(_ *tool.ToolContext, args map[string]any) (any, error) {
	m.ensureSkillsFresh()
	query, _ := args["query"].(string)
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return "Please provide a search query.", nil
	}

	// Tokenize query into individual keywords for full-text matching.
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return "Please provide a search query.", nil
	}

	type scoredSkill struct {
		skill *Skill
		score int
	}
	var scored []scoredSkill
	for _, s := range m.skills {
		if !skillIsCore(s) {
			continue
		}
		nameLower := strings.ToLower(s.Name)
		descLower := strings.ToLower(s.Description)

		score := 0
		// Exact name match gets highest score.
		if nameLower == query {
			score = 100
		} else if len(tokens) == 1 && strings.Contains(nameLower, tokens[0]) {
			// Single-token partial name match.
			score = 50
		} else {
			// Full-text: count how many tokens match name or description.
			nameHits, descHits := 0, 0
			for _, tok := range tokens {
				if strings.Contains(nameLower, tok) {
					nameHits++
				} else if strings.Contains(descLower, tok) {
					descHits++
				}
			}
			// All tokens matched in name.
			if nameHits == len(tokens) {
				score = 60
			} else if nameHits > 0 {
				// Partial name hits.
				score = 30 + nameHits*10
			} else if descHits > 0 {
				// Description hits only.
				score = 10 + descHits*5
			}
		}
		if score > 0 {
			scored = append(scored, scoredSkill{skill: s, score: score})
		}
	}

	if len(scored) == 0 {
		return "No skills found matching your query. Try different keywords, or use skillList() to see all available skills.", nil
	}

	// sort by score descending, then by name ascending
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].skill.Name < scored[j].skill.Name
	})

	var results []string
	for _, sc := range scored {
		tag := "[prompt]"
		if sc.skill.Type == "agent" {
			tag = "[agent]"
		}
		results = append(results, fmt.Sprintf("- %s %s: %s", tag, sc.skill.Name, sc.skill.Description))
	}

	return "Matching skills:\n" + strings.Join(results, "\n") + "\n\nFor prompt skills, call skill(name=\"<skill_name>\"). For agent skills, call delegate(skill=\"<skill_name>\", task=\"...\").", nil
}

func (m *Manager) skillTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "skill",
			Description: "Load a skill by name. Returns the skill content.",
			Group:       tool.ToolGroupCore,
			SearchHint:  "skill, capability, skill.md, load skill, specialized, 技能, 加载, 能力",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Skill name"},
				},
				"required": []string{"name"},
			},
		},
		Handler: m.skillHandler,
	}
}

func (m *Manager) skillHandler(_ *tool.ToolContext, args map[string]any) (any, error) {
	m.ensureSkillsFresh()
	name, _ := args["name"].(string)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "No skill name provided. Use skillSearch(query=\"...\") to find skills.", nil
	}

	var sk *Skill
	for _, s := range m.skills {
		if strings.ToLower(s.Name) == name {
			sk = s
			break
		}
	}
	if sk == nil {
		var suggestions []string
		for _, s := range m.skills {
			if strings.Contains(strings.ToLower(s.Name), name) || strings.Contains(strings.ToLower(s.Description), name) {
				suggestions = append(suggestions, fmt.Sprintf("- %s: %s", s.Name, s.Description))
			}
		}
		if len(suggestions) > 0 {
			return "No exact skill match found. Did you mean:\n" + strings.Join(suggestions, "\n") + "\n\nUse skillSearch(query=\"...\") for a broader search.", nil
		}
		return "No skill found with that name. Use skillSearch(query=\"...\") to search for skills.", nil
	}
	if !skillIsCore(sk) {
		return fmt.Sprintf("Skill %q is currently extended. Call skillPromote(name=\"%s\") first to enable it.", sk.Name, sk.Name), nil
	}

	s := sk
	if skillDiskDir(sk) != "" {
		parsed, err := parseSkillFile(sk.Location)
		if err != nil {
			return nil, fmt.Errorf("read skill: %w", err)
		}
		s = parsed
	}

	return formatSkillLoaded(s), nil
}

func formatSkillLoaded(s *Skill) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Skill Loaded: %s\n", s.Name)
	if s.Description != "" {
		fmt.Fprintf(&b, "**Description:** %s\n", s.Description)
	}
	fmt.Fprintf(&b, "**Type:** %s\n", s.Type)
	if s.Type == "agent" && s.WhenToUse != "" {
		fmt.Fprintf(&b, "**When to use:** %s\n", s.WhenToUse)
	}
	if dir := skillDiskDir(s); dir != "" {
		fmt.Fprintf(&b, "Base directory for this skill: %s\n", dir)
		fmt.Fprintf(&b, "%s\n\n", skillSupportingFilesHint)
	} else {
		b.WriteByte('\n')
	}
	b.WriteString("## Instructions\n")
	b.WriteString("Please follow the instructions below when executing this task. Do not deviate from them unless the user explicitly asks otherwise.\n\n")
	b.WriteString(s.Content)
	return b.String()
}

// --- system prompt ---

const skillSearchInstructions = `## Skills

Project-specific skills are available. To find and load a skill:

1. Call skillSearch(query="description of what you need") to discover relevant skills.
2. Call skill(name="exact_skill_name") to load its full instructions.
3. Follow the loaded instructions to complete the task.`

func (m *Manager) buildSystemPrompt() (string, error) {
	m.ensureSkillsFresh()
	if len(m.skills) == 0 {
		return "", nil
	}
	return skillSearchInstructions, nil
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// --- helpers for tools ---

func (m *Manager) findSkillLocation(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, s := range m.skills {
		if strings.ToLower(s.Name) == name {
			return s.Location
		}
	}
	return ""
}

// resolveSkillRoots resolves skill root paths from config.
func resolveSkillRoots(configPaths []string) []string {
	home, _ := os.UserHomeDir()

	var paths []string
	if len(configPaths) > 0 {
		for _, p := range configPaths {
			expanded := os.ExpandEnv(p)
			if strings.HasPrefix(expanded, "~/") {
				expanded = filepath.Join(home, expanded[2:])
			}
			if !filepath.IsAbs(expanded) {
				expanded = filepath.Join(home, ".dmr", expanded)
			}
			paths = append(paths, expanded)
		}
	} else {
		paths = []string{
			filepath.Join(home, ".dmr", "skills", "local"),
		}
	}
	return paths
}
